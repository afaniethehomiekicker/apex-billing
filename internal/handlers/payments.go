package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Payments.
//
//	GET  /api/payments                  ?status= &client_id= &method= &month=YYYY-MM &q= &page=
//	GET  /api/payments/open-invoices    ?client_id=  issued invoices still open (for applying a payment)
//	GET  /api/payments/:id              with allocations, history
//	GET  /api/payments/:id/slip         the slip file
//	POST /api/payments                  record (record_payments): multipart "data" (JSON) + "slip" (file)
//	POST /api/payments/:id/verify       (verify_payments)
//	POST /api/payments/:id/reject       { reason } (verify_payments)
//	POST /api/payments/:id/reverse      { reason } (verify_payments) a verified payment, e.g. bounced cheque
//	POST /api/payments/:id/allocate     { allocations } apply a verified payment's unapplied credit
//
// Only verified payments count: they set each invoice's amount paid. A
// payment is checked by someone other than the person who recorded it
// (an Admin may do both). Nothing is deleted.

// ---- slips ------------------------------------------------------------------

const maxSlipBytes = 8 << 20

var slipTypes = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "application/pdf": ".pdf",
}

// uploadDir: UPLOAD_DIR, default "uploads" (next to the program). Back it
// up with the database.
func uploadDir() string {
	d := strings.TrimSpace(os.Getenv("UPLOAD_DIR"))
	if d == "" {
		d = "uploads"
	}
	return d
}

// saveSlip checks the file is a picture or PDF and stores it under
// uploads/slips/YYYY/MM/ with a random name. Returns the path relative to
// the upload dir.
func saveSlip(fh *multipart.FileHeader) (rel, ctype string, size int64, msg string, err error) {
	if fh.Size > maxSlipBytes {
		return "", "", 0, "The slip is too big (max 8 MB)", nil
	}
	f, err := fh.Open()
	if err != nil {
		return "", "", 0, "", err
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	ctype = http.DetectContentType(head[:n])
	if i := strings.Index(ctype, ";"); i > 0 {
		ctype = ctype[:i]
	}
	ext, ok := slipTypes[ctype]
	if !ok {
		return "", "", 0, "The slip must be a photo (JPG, PNG, WEBP) or a PDF", nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", "", 0, "", err
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", "", 0, "", err
	}
	now := time.Now()
	rel = filepath.ToSlash(filepath.Join("slips", now.Format("2006"), now.Format("01"), hex.EncodeToString(buf)+ext))
	full := filepath.Join(uploadDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", "", 0, "", err
	}
	out, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", "", 0, "", err
	}
	size, err = io.Copy(out, io.LimitReader(f, maxSlipBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil || size > maxSlipBytes {
		os.Remove(full)
		if err == nil {
			return "", "", 0, "The slip is too big (max 8 MB)", nil
		}
		return "", "", 0, "", err
	}
	return rel, ctype, size, "", nil
}

func GetPaymentSlip(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var p models.Payment
	if err := database.DB.First(&p, id).Error; err != nil || p.SlipPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "No slip for this payment"})
		return
	}
	full := filepath.Join(uploadDir(), filepath.FromSlash(p.SlipPath))
	if _, err := os.Stat(full); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "The slip file is missing from the server's uploads folder"})
		return
	}
	c.Header("Content-Type", p.SlipType)
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, strings.ReplaceAll(p.PaymentNumber+filepath.Ext(p.SlipPath), `"`, "")))
	c.Header("Cache-Control", "private, no-store")
	// Opened directly, the file can't run anything.
	c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; plugin-types application/pdf; sandbox")
	c.File(full)
}

// ---- amounts ----------------------------------------------------------------

// refreshInvoicePaid sets each invoice's amount paid from verified payments.
func refreshInvoicePaid(tx *gorm.DB, invoiceIDs []uint) error {
	if len(invoiceIDs) == 0 {
		return nil
	}
	return tx.Exec(`UPDATE invoices SET amount_paid = COALESCE((
			SELECT SUM(a.amount) FROM payment_allocations a JOIN payments p ON p.id = a.payment_id
			WHERE a.invoice_id = invoices.id AND p.status = 'verified'), 0)
		WHERE id IN ?`, invoiceIDs).Error
}

// invoiceOpen: how much of each invoice is not yet covered by verified or
// pending payments (so two pending payments can't both pay the same bill).
func invoiceOpen(tx *gorm.DB, ids []uint) (map[uint]float64, error) {
	type row struct {
		ID   uint
		Open float64
	}
	var rows []row
	err := tx.Raw(`SELECT i.id, i.total - COALESCE((
			SELECT SUM(a.amount) FROM payment_allocations a JOIN payments p ON p.id = a.payment_id
			WHERE a.invoice_id = i.id AND p.status IN ('pending', 'verified')), 0) AS open
		FROM invoices i WHERE i.id IN ?`, ids).Scan(&rows).Error
	out := map[uint]float64{}
	for _, r := range rows {
		out[r.ID] = roundMoney(r.Open)
	}
	return out, err
}

func allocated(p *models.Payment) float64 {
	t := 0.0
	for _, a := range p.Allocations {
		t += a.Amount
	}
	return roundMoney(t)
}

func credited(p *models.Payment) float64 { return roundMoney(p.Amount + p.TaxDeducted) }

type allocInput struct {
	InvoiceID uint    `json:"invoice_id"`
	Amount    float64 `json:"amount"`
}

// checkAllocations validates applying amounts to a client's invoices, up
// to "available". Returns the rows to add, or a message.
func checkAllocations(tx *gorm.DB, clientID uint, in []allocInput, available float64) ([]models.PaymentAllocation, string, error) {
	var out []models.PaymentAllocation
	if len(in) == 0 {
		return nil, "", nil
	}
	if len(in) > 500 {
		return nil, "Too many invoices at once", nil
	}
	seen := map[uint]bool{}
	var ids []uint
	total := 0.0
	for _, a := range in {
		if a.InvoiceID == 0 || badNumber(a.Amount) || a.Amount < 0 {
			return nil, "Check the amounts applied to invoices", nil
		}
		if roundMoney(a.Amount) == 0 {
			continue
		}
		if seen[a.InvoiceID] {
			return nil, "An invoice is listed twice", nil
		}
		seen[a.InvoiceID] = true
		ids = append(ids, a.InvoiceID)
		total += roundMoney(a.Amount)
		out = append(out, models.PaymentAllocation{InvoiceID: a.InvoiceID, Amount: roundMoney(a.Amount)})
	}
	if len(out) == 0 {
		return nil, "", nil
	}
	if roundMoney(total) > roundMoney(available) {
		return nil, fmt.Sprintf("You applied %s to invoices but only %s is available", money(total), money(available)), nil
	}
	var invs []models.Invoice
	if err := tx.Where("id IN ?", ids).Find(&invs).Error; err != nil {
		return nil, "", err
	}
	byID := map[uint]models.Invoice{}
	for _, v := range invs {
		byID[v.ID] = v
	}
	open, err := invoiceOpen(tx, ids)
	if err != nil {
		return nil, "", err
	}
	for _, a := range out {
		v, ok := byID[a.InvoiceID]
		if !ok || v.ClientID != clientID {
			return nil, "An invoice doesn't belong to this client", nil
		}
		if v.Status != models.InvoiceIssued {
			return nil, "Payments can only be applied to issued invoices (" + v.InvoiceNumber + " is not)", nil
		}
		if a.Amount > open[v.ID] {
			return nil, fmt.Sprintf("%s has only %s left to pay (counting payments waiting to be verified)", v.InvoiceNumber, money(open[v.ID])), nil
		}
	}
	return out, "", nil
}

// ---- record -----------------------------------------------------------------

type paymentInput struct {
	ClientID         uint         `json:"client_id"`
	PaymentDate      string       `json:"payment_date"`
	Method           string       `json:"method"`
	Reference        string       `json:"reference"`
	BankName         string       `json:"bank_name"`
	Amount           float64      `json:"amount"`
	TaxDeducted      float64      `json:"tax_deducted"`
	Notes            string       `json:"notes"`
	Allocations      []allocInput `json:"allocations"`
	ConfirmDuplicate bool         `json:"confirm_duplicate"`
}

func RecordPayment(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSlipBytes+(1<<20))
	var in paymentInput
	if err := json.Unmarshal([]byte(c.PostForm("data")), &in); err != nil {
		badRequest(c, "Invalid request (or the slip is larger than 8 MB)")
		return
	}
	var cl models.Client
	if in.ClientID == 0 || database.DB.First(&cl, in.ClientID).Error != nil {
		badRequest(c, "Choose the client who paid")
		return
	}
	p := models.Payment{ClientID: cl.ID, Status: models.PaymentPending}
	d, err := parseDay(in.PaymentDate)
	if err != nil || d == nil {
		badRequest(c, "Give the payment date")
		return
	}
	if d.After(todayDate()) {
		badRequest(c, "The payment date can't be in the future")
		return
	}
	p.PaymentDate = *d
	if _, ok := models.PaymentMethods[in.Method]; !ok {
		badRequest(c, "Choose how they paid")
		return
	}
	p.Method = in.Method
	var ok bool
	if p.Reference, ok = trimTo(in.Reference, 100); !ok {
		badRequest(c, "Reference is too long")
		return
	}
	if p.BankName, ok = trimTo(in.BankName, 100); !ok {
		badRequest(c, "Bank name is too long")
		return
	}
	if p.Notes, ok = trimTo(in.Notes, 2000); !ok {
		badRequest(c, "Notes are too long")
		return
	}
	if badNumber(in.Amount) || in.Amount < 0 || in.Amount > 999999999999 || badNumber(in.TaxDeducted) || in.TaxDeducted < 0 || in.TaxDeducted > 999999999999 {
		badRequest(c, "Check the amount")
		return
	}
	p.Amount, p.TaxDeducted = roundMoney(in.Amount), roundMoney(in.TaxDeducted)
	if credited(&p) <= 0 {
		badRequest(c, "Enter the amount received")
		return
	}
	if p.Method == "cheque" && p.Reference == "" {
		badRequest(c, "Give the cheque number")
		return
	}

	fh, ferr := c.FormFile("slip")
	if ferr != nil && p.Method != "cash" {
		badRequest(c, "Attach the slip (photo or PDF of the deposit slip, cheque or transfer receipt)")
		return
	}

	if !in.ConfirmDuplicate && p.Reference != "" {
		var other models.Payment
		if database.DB.Where("client_id = ? AND LOWER(TRIM(reference)) = LOWER(?) AND status <> ?", cl.ID, p.Reference, models.PaymentRejected).
			First(&other).Error == nil {
			c.JSON(http.StatusConflict, gin.H{"duplicate": true, "error": fmt.Sprintf("%s already has payment %s with reference %s (%s, %s)",
				cl.Name, other.PaymentNumber, p.Reference, other.PaymentDate.Format("02 Jan 2006"), money(other.Amount))})
			return
		}
	}

	var fullPath string
	if fh != nil {
		rel, ctype, size, msg, err := saveSlip(fh)
		if err != nil {
			serverError(c, "Could not save the slip", err)
			return
		}
		if msg != "" {
			badRequest(c, msg)
			return
		}
		p.SlipPath, p.SlipType, p.SlipSize = rel, ctype, size
		p.SlipName, _ = trimTo(filepath.Base(fh.Filename), 200)
		fullPath = filepath.Join(uploadDir(), filepath.FromSlash(rel))
	}

	u := auth.User(c)
	p.RecordedByID, p.RecordedBy = u.ID, u.Name
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockBilling(tx); err != nil {
			return err
		}
		allocs, msg, err := checkAllocations(tx, cl.ID, in.Allocations, credited(&p))
		if err != nil {
			return err
		}
		if msg != "" {
			badRequest(c, msg)
			return errAnswered
		}
		if err := tx.Omit("Client", "Allocations").Create(&p).Error; err != nil {
			return err
		}
		for i := range allocs {
			allocs[i].PaymentID, allocs[i].CreatedByID = p.ID, u.ID
		}
		if len(allocs) > 0 {
			if err := tx.Create(&allocs).Error; err != nil {
				return err
			}
		}
		p.Allocations = allocs
		auditTx(tx, c, "recorded", "payment", p.ID, fmt.Sprintf("%s from %s %s: %s %s by %s%s, applied to %d invoice(s); waiting for verification",
			p.PaymentNumber, cl.ClientNumber, cl.Name, money(p.Amount), withTax(p.TaxDeducted), models.PaymentMethods[p.Method], refText(p.Reference), len(allocs)))
		c.JSON(http.StatusCreated, gin.H{"payment": p})
		return nil
	})
	if err != nil {
		if fullPath != "" {
			os.Remove(fullPath)
		}
		if err != errAnswered {
			serverError(c, "Could not record the payment", err)
		}
	}
}

func withTax(t float64) string {
	if t == 0 {
		return ""
	}
	return "+ tax deducted " + money(t)
}

func refText(r string) string {
	if r == "" {
		return ""
	}
	return " (" + r + ")"
}

// ---- verify / reject / reverse ------------------------------------------------

func loadPayment(c *gin.Context, tx *gorm.DB) *models.Payment {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var p models.Payment
	if err := tx.Preload("Client").Preload("Allocations").First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Payment not found"})
		return nil
	}
	return &p
}

func allocInvoiceIDs(p *models.Payment) []uint {
	var ids []uint
	for _, a := range p.Allocations {
		ids = append(ids, a.InvoiceID)
	}
	return ids
}

// paymentAction runs one status change in a transaction.
func paymentAction(c *gin.Context, fn func(tx *gorm.DB, p *models.Payment) (string, error)) {
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockBilling(tx); err != nil {
			return err
		}
		p := loadPayment(c, tx)
		if p == nil {
			return errAnswered
		}
		msg, err := fn(tx, p)
		if err != nil {
			return err
		}
		if msg != "" {
			badRequest(c, msg)
			return errAnswered
		}
		if err := tx.Omit("Client", "Allocations").Save(p).Error; err != nil {
			return err
		}
		if err := refreshInvoicePaid(tx, allocInvoiceIDs(p)); err != nil {
			return err
		}
		p.Client = nil
		c.JSON(http.StatusOK, gin.H{"payment": p})
		return nil
	})
	if err != nil && err != errAnswered {
		serverError(c, "Could not update the payment", err)
	}
}

func reasonFrom(c *gin.Context) (string, bool) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return "", false
	}
	r, fits := trimTo(in.Reason, 1000)
	if r == "" || !fits {
		badRequest(c, "Give the reason (max 1000 characters)")
		return "", false
	}
	return r, true
}

func VerifyPayment(c *gin.Context) {
	u := auth.User(c)
	paymentAction(c, func(tx *gorm.DB, p *models.Payment) (string, error) {
		if p.Status != models.PaymentPending {
			return "Only a payment waiting for verification can be verified", nil
		}
		if p.RecordedByID == u.ID && u.Role != models.RoleAdmin {
			return "You recorded this payment, so someone else has to verify it", nil
		}
		now := time.Now()
		p.Status, p.CheckedByID, p.CheckedBy, p.CheckedAt = models.PaymentVerified, u.ID, u.Name, &now
		auditTx(tx, c, "verified", "payment", p.ID, fmt.Sprintf("%s from %s: %s", p.PaymentNumber, p.Client.Name, money(credited(p))))
		return "", nil
	})
}

func RejectPayment(c *gin.Context) {
	reason, ok := reasonFrom(c)
	if !ok {
		return
	}
	u := auth.User(c)
	paymentAction(c, func(tx *gorm.DB, p *models.Payment) (string, error) {
		if p.Status != models.PaymentPending {
			return "Only a payment waiting for verification can be rejected (a verified one can be reversed)", nil
		}
		now := time.Now()
		p.Status, p.CheckedByID, p.CheckedBy, p.CheckedAt, p.RejectReason = models.PaymentRejected, u.ID, u.Name, &now, reason
		auditTx(tx, c, "rejected", "payment", p.ID, fmt.Sprintf("%s from %s (%s): %s", p.PaymentNumber, p.Client.Name, money(credited(p)), reason))
		return "", nil
	})
}

func ReversePayment(c *gin.Context) {
	reason, ok := reasonFrom(c)
	if !ok {
		return
	}
	u := auth.User(c)
	paymentAction(c, func(tx *gorm.DB, p *models.Payment) (string, error) {
		if p.Status != models.PaymentVerified {
			return "Only a verified payment can be reversed", nil
		}
		now := time.Now()
		p.Status, p.ReversedByID, p.ReversedBy, p.ReversedAt, p.ReverseReason = models.PaymentReversed, u.ID, u.Name, &now, reason
		auditTx(tx, c, "reversed", "payment", p.ID, fmt.Sprintf("%s from %s (%s): %s", p.PaymentNumber, p.Client.Name, money(credited(p)), reason))
		return "", nil
	})
}

// AllocatePayment applies a verified payment's unapplied credit to invoices.
func AllocatePayment(c *gin.Context) {
	var in struct {
		Allocations []allocInput `json:"allocations"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	u := auth.User(c)
	paymentAction(c, func(tx *gorm.DB, p *models.Payment) (string, error) {
		if p.Status != models.PaymentVerified {
			return "Only a verified payment's credit can be applied here", nil
		}
		free := roundMoney(credited(p) - allocated(p))
		allocs, msg, err := checkAllocations(tx, p.ClientID, in.Allocations, free)
		if err != nil || msg != "" {
			return msg, err
		}
		if len(allocs) == 0 {
			return "Enter the amount to apply to at least one invoice", nil
		}
		// One allocation per invoice: add to an existing one.
		existing := map[uint]*models.PaymentAllocation{}
		for i := range p.Allocations {
			existing[p.Allocations[i].InvoiceID] = &p.Allocations[i]
		}
		for _, a := range allocs {
			if e, ok := existing[a.InvoiceID]; ok {
				if err := tx.Model(&models.PaymentAllocation{}).Where("id = ?", e.ID).
					Update("amount", roundMoney(e.Amount+a.Amount)).Error; err != nil {
					return "", err
				}
				e.Amount = roundMoney(e.Amount + a.Amount)
				continue
			}
			a.PaymentID, a.CreatedByID = p.ID, u.ID
			if err := tx.Create(&a).Error; err != nil {
				return "", err
			}
			p.Allocations = append(p.Allocations, a)
		}
		auditTx(tx, c, "applied", "payment", p.ID, fmt.Sprintf("%s credit applied to %d invoice(s); %s left unapplied",
			p.PaymentNumber, len(allocs), money(roundMoney(credited(p)-allocated(p)))))
		return "", nil
	})
}

// ---- list / view --------------------------------------------------------------

func GetPayments(c *gin.Context) {
	q := database.DB.Model(&models.Payment{}).Joins("JOIN clients ON clients.id = payments.client_id")
	switch st := c.DefaultQuery("status", ""); st {
	case "", "all":
	case models.PaymentPending, models.PaymentVerified, models.PaymentRejected, models.PaymentReversed:
		q = q.Where("payments.status = ?", st)
	default:
		badRequest(c, "Unknown status")
		return
	}
	if v := c.Query("client_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			badRequest(c, "Invalid client")
			return
		}
		q = q.Where("payments.client_id = ?", id)
	}
	if m := c.Query("method"); m != "" {
		if _, ok := models.PaymentMethods[m]; !ok {
			badRequest(c, "Unknown method")
			return
		}
		q = q.Where("payments.method = ?", m)
	}
	if m, err := parseMonth(c.Query("month")); err != nil {
		badRequest(c, "Month must be YYYY-MM")
		return
	} else if m != nil {
		q = q.Where("payments.payment_date >= ? AND payments.payment_date < ?", m.Format("2006-01-02"), m.AddDate(0, 1, 0).Format("2006-01-02"))
	}
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		like := likeTerm(s)
		q = q.Where(`(LOWER(payments.payment_number) LIKE ? OR LOWER(payments.reference) LIKE ? OR LOWER(clients.name) LIKE ?
			OR LOWER(clients.client_number) LIKE ?)`, like, like, like, like)
	}
	var sum struct {
		Count  int64   `json:"count"`
		Amount float64 `json:"amount"`
		Tax    float64 `json:"tax"`
	}
	if err := q.Session(&gorm.Session{}).Select("COUNT(*) AS count, COALESCE(SUM(payments.amount), 0) AS amount, COALESCE(SUM(payments.tax_deducted), 0) AS tax").
		Scan(&sum).Error; err != nil {
		serverError(c, "Could not load payments", err)
		return
	}
	page, size := pageParams(c)
	var list []models.Payment
	if err := q.Session(&gorm.Session{}).Preload("Client").Preload("Allocations").
		Order("CASE WHEN payments.status = 'pending' THEN 0 ELSE 1 END, payments.payment_date DESC, payments.id DESC").
		Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		serverError(c, "Could not load payments", err)
		return
	}
	if list == nil {
		list = []models.Payment{}
	}
	var pending struct {
		Count  int64   `json:"count"`
		Amount float64 `json:"amount"`
	}
	database.DB.Model(&models.Payment{}).Where("status = ?", models.PaymentPending).
		Select("COUNT(*) AS count, COALESCE(SUM(amount + tax_deducted), 0) AS amount").Scan(&pending)
	role := auth.User(c).Role
	c.JSON(http.StatusOK, gin.H{
		"payments": list, "total": sum.Count, "amount": roundMoney(sum.Amount), "tax": roundMoney(sum.Tax),
		"page": page, "page_size": size, "pending": pending, "methods": models.PaymentMethods,
		"can_record": auth.Can(role, "record_payments"), "can_verify": auth.Can(role, "verify_payments"),
	})
}

func GetPayment(c *gin.Context) {
	p := loadPayment(c, database.DB.Preload("Allocations.Invoice"))
	if p == nil {
		return
	}
	var history []models.AuditLog
	database.DB.Where("entity_type = ? AND entity_id = ?", "payment", p.ID).Order("id DESC").Limit(50).Find(&history)
	u := auth.User(c)
	c.JSON(http.StatusOK, gin.H{
		"payment": p, "history": history, "methods": models.PaymentMethods,
		"credited": credited(p), "allocated": allocated(p), "unallocated": roundMoney(credited(p) - allocated(p)),
		"can_verify":   auth.Can(u.Role, "verify_payments"),
		"own_payment":  p.RecordedByID == u.ID && u.Role != models.RoleAdmin,
		"can_allocate": auth.Can(u.Role, "verify_payments") || auth.Can(u.Role, "manage_billing"),
	})
}

// OpenInvoices: a client's issued invoices with something left to pay.
func OpenInvoices(c *gin.Context) {
	cid, err := strconv.ParseUint(c.Query("client_id"), 10, 64)
	if err != nil || cid == 0 {
		badRequest(c, "Choose a client")
		return
	}
	var invs []models.Invoice
	if err := database.DB.Where("client_id = ? AND status = ?", cid, models.InvoiceIssued).
		Order("invoice_date, id").Find(&invs).Error; err != nil {
		serverError(c, "Could not load invoices", err)
		return
	}
	ids := make([]uint, 0, len(invs))
	for _, v := range invs {
		ids = append(ids, v.ID)
	}
	open := map[uint]float64{}
	if len(ids) > 0 {
		if open, err = invoiceOpen(database.DB, ids); err != nil {
			serverError(c, "Could not load invoices", err)
			return
		}
	}
	type row struct {
		ID            uint      `json:"id"`
		InvoiceNumber string    `json:"invoice_number"`
		InvoiceDate   time.Time `json:"invoice_date"`
		DueDate       time.Time `json:"due_date"`
		Total         float64   `json:"total"`
		AmountPaid    float64   `json:"amount_paid"`
		Pending       float64   `json:"pending"` // in payments waiting for verification
		Open          float64   `json:"open"`
	}
	out := []row{}
	for _, v := range invs {
		if open[v.ID] <= 0 {
			continue
		}
		out = append(out, row{v.ID, v.InvoiceNumber, v.InvoiceDate, v.DueDate, v.Total, v.AmountPaid,
			roundMoney(v.Total - v.AmountPaid - open[v.ID]), open[v.ID]})
	}
	c.JSON(http.StatusOK, gin.H{"invoices": out})
}

// ---- balances -----------------------------------------------------------------

type clientBalance struct {
	Opening   float64 `json:"opening"`
	Invoiced  float64 `json:"invoiced"`  // issued invoices
	Received  float64 `json:"received"`  // verified payments (amount + tax deducted)
	Balance   float64 `json:"balance"`   // opening + invoiced − received; negative = in credit
	Pending   float64 `json:"pending"`   // payments waiting for verification
	Overdue   float64 `json:"overdue"`   // unpaid on invoices past their due date
	Unapplied float64 `json:"unapplied"` // verified credit not applied to invoices
}

func balanceOf(cl *models.Client) clientBalance {
	b := clientBalance{Opening: cl.OpeningBalance}
	database.DB.Model(&models.Invoice{}).Where("client_id = ? AND status = ?", cl.ID, models.InvoiceIssued).
		Select("COALESCE(SUM(total), 0)").Scan(&b.Invoiced)
	database.DB.Model(&models.Invoice{}).Where("client_id = ? AND status = ? AND due_date < ?", cl.ID, models.InvoiceIssued, today()).
		Select("COALESCE(SUM(total - amount_paid), 0)").Scan(&b.Overdue)
	database.DB.Model(&models.Payment{}).Where("client_id = ? AND status = ?", cl.ID, models.PaymentVerified).
		Select("COALESCE(SUM(amount + tax_deducted), 0)").Scan(&b.Received)
	database.DB.Model(&models.Payment{}).Where("client_id = ? AND status = ?", cl.ID, models.PaymentPending).
		Select("COALESCE(SUM(amount + tax_deducted), 0)").Scan(&b.Pending)
	database.DB.Raw(`SELECT COALESCE(SUM(p.amount + p.tax_deducted - COALESCE((SELECT SUM(a.amount) FROM payment_allocations a WHERE a.payment_id = p.id), 0)), 0)
		FROM payments p WHERE p.client_id = ? AND p.status = 'verified'`, cl.ID).Scan(&b.Unapplied)
	b.Invoiced, b.Received, b.Pending, b.Overdue, b.Unapplied = roundMoney(b.Invoiced), roundMoney(b.Received), roundMoney(b.Pending), roundMoney(b.Overdue), roundMoney(b.Unapplied)
	b.Balance = roundMoney(b.Opening + b.Invoiced - b.Received)
	return b
}

// GetDashboard: the headline figures.
func GetDashboard(c *gin.Context) {
	td := today()
	ms := monthStart(todayDate())
	var d struct {
		Receivable     float64 `json:"receivable"`      // everyone's balances (opening + invoiced − received)
		Outstanding    float64 `json:"outstanding"`     // unpaid on issued invoices
		Overdue        float64 `json:"overdue"`         // … of which past due
		OverdueCount   int64   `json:"overdue_count"`   // invoices past due, not fully paid
		CollectedMonth float64 `json:"collected_month"` // verified payments dated this month (incl. tax deducted)
		PendingCount   int64   `json:"pending_count"`
		PendingAmount  float64 `json:"pending_amount"`
		InvoicedMonth  float64 `json:"invoiced_month"`
		ContractsDue   int     `json:"contracts_due"`     // notice date reached: renew or not
		ContractsEnded int     `json:"contracts_expired"` // end date passed, not renewed or terminated
	}
	db := database.DB
	var opening, invoiced, received float64
	db.Model(&models.Client{}).Select("COALESCE(SUM(opening_balance), 0)").Scan(&opening)
	db.Model(&models.Invoice{}).Where("status = ?", models.InvoiceIssued).Select("COALESCE(SUM(total), 0)").Scan(&invoiced)
	db.Model(&models.Payment{}).Where("status = ?", models.PaymentVerified).Select("COALESCE(SUM(amount + tax_deducted), 0)").Scan(&received)
	d.Receivable = roundMoney(opening + invoiced - received)
	db.Model(&models.Invoice{}).Where("status = ?", models.InvoiceIssued).Select("COALESCE(SUM(total - amount_paid), 0)").Scan(&d.Outstanding)
	db.Model(&models.Invoice{}).Where("status = ? AND due_date < ? AND total > amount_paid", models.InvoiceIssued, td).
		Select("COALESCE(SUM(total - amount_paid), 0)").Scan(&d.Overdue)
	db.Model(&models.Invoice{}).Where("status = ? AND due_date < ? AND total > amount_paid", models.InvoiceIssued, td).Count(&d.OverdueCount)
	db.Model(&models.Payment{}).Where("status = ? AND payment_date >= ?", models.PaymentVerified, ms.Format("2006-01-02")).
		Select("COALESCE(SUM(amount + tax_deducted), 0)").Scan(&d.CollectedMonth)
	db.Model(&models.Invoice{}).Where("status = ? AND invoice_date >= ? AND invoice_date < ?", models.InvoiceIssued,
		ms.Format("2006-01-02"), ms.AddDate(0, 1, 0).Format("2006-01-02")).
		Select("COALESCE(SUM(total), 0)").Scan(&d.InvoicedMonth)
	db.Model(&models.Payment{}).Where("status = ?", models.PaymentPending).Count(&d.PendingCount)
	db.Model(&models.Payment{}).Where("status = ?", models.PaymentPending).Select("COALESCE(SUM(amount + tax_deducted), 0)").Scan(&d.PendingAmount)
	d.Outstanding, d.Overdue, d.CollectedMonth = roundMoney(d.Outstanding), roundMoney(d.Overdue), roundMoney(d.CollectedMonth)
	d.PendingAmount, d.InvoicedMonth = roundMoney(d.PendingAmount), roundMoney(d.InvoicedMonth)
	d.ContractsDue, d.ContractsEnded = contractsDue()
	c.JSON(http.StatusOK, gin.H{"dashboard": d, "today": td})
}
