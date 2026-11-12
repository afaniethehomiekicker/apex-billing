package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Invoices.
//
//	GET    /api/invoices              ?status=draft|issued|cancelled|all &client_id= &company_id= &month=YYYY-MM (invoice date)
//	                                  &billing_month=YYYY-MM &q= &page= &page_size=
//	GET    /api/invoices/:id          with lines, client, company, history
//	POST   /api/invoices              a manual draft (manage_billing)
//	PUT    /api/invoices/:id          edit a draft: dates, notes, lines (each with its tax_ids)
//	DELETE /api/invoices/:id          delete a draft (issued invoices are cancelled instead)
//	POST   /api/invoices/:id/add-due  add the client's due service charges to a draft
//	POST   /api/invoices/generate     bulk monthly run: { month, invoice_date, company_id (0 = all), preview }
//	POST   /api/invoices/issue        issue drafts: { ids: [...] }
//	POST   /api/invoices/:id/cancel   cancel an issued invoice: { reason }
//
// Each invoice is from one of our companies (a client with services from
// two companies gets two invoices). Drafts have no number and can change
// freely. Issuing gives the next number (that company's prefix + counter,
// never reused), copies the client's and the company's details (and logo)
// and marks the services billed up to the periods on it.

var errNotFound = errors.New("not found")

// ---- list / view ------------------------------------------------------------

func parseMonth(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func GetInvoices(c *gin.Context) {
	q := database.DB.Model(&models.Invoice{}).Joins("JOIN clients ON clients.id = invoices.client_id")
	switch st := c.DefaultQuery("status", ""); st {
	case "":
		q = q.Where("invoices.status <> ?", models.InvoiceCancelled)
	case "all":
	case models.InvoiceDraft, models.InvoiceIssued, models.InvoiceCancelled:
		q = q.Where("invoices.status = ?", st)
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
		q = q.Where("invoices.client_id = ?", id)
	}
	if id, ok := companyParam(c); !ok {
		return
	} else if id != 0 {
		q = q.Where("invoices.company_id = ?", id)
	}
	if m, err := parseMonth(c.Query("month")); err != nil {
		badRequest(c, "Month must be YYYY-MM")
		return
	} else if m != nil {
		q = q.Where("invoices.invoice_date >= ? AND invoices.invoice_date < ?", m.Format("2006-01-02"), m.AddDate(0, 1, 0).Format("2006-01-02"))
	}
	if m, err := parseMonth(c.Query("billing_month")); err != nil {
		badRequest(c, "Billing month must be YYYY-MM")
		return
	} else if m != nil {
		q = q.Where("invoices.billing_month = ?", m.Format("2006-01-02"))
	}
	switch c.Query("pay") {
	case "":
	case "open": // issued, not fully paid
		q = q.Where("invoices.status = 'issued' AND invoices.total > invoices.amount_paid")
	case "overdue":
		q = q.Where("invoices.status = 'issued' AND invoices.total > invoices.amount_paid AND invoices.due_date < ?", today())
	case "paid":
		q = q.Where("invoices.status = 'issued' AND invoices.total <= invoices.amount_paid")
	default:
		badRequest(c, "Unknown payment filter")
		return
	}
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		like := likeTerm(s)
		q = q.Where("(LOWER(invoices.invoice_number) LIKE ? OR LOWER(clients.name) LIKE ? OR LOWER(clients.client_number) LIKE ?)", like, like, like)
	}

	var sum struct {
		Count int64   `json:"count"`
		Total float64 `json:"total"`
	}
	if err := q.Session(&gorm.Session{}).Select("COUNT(*) AS count, COALESCE(SUM(invoices.total), 0) AS total").Scan(&sum).Error; err != nil {
		serverError(c, "Could not load invoices", err)
		return
	}
	sum.Total = roundMoney(sum.Total)
	page, size := pageParams(c)
	var list []models.Invoice
	if err := q.Session(&gorm.Session{}).Preload("Client").Preload("Company").
		Order("CASE WHEN invoices.status = 'draft' THEN 0 ELSE 1 END, invoices.invoice_date DESC, invoices.invoice_number DESC, LOWER(clients.name)").
		Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		serverError(c, "Could not load invoices", err)
		return
	}
	if list == nil {
		list = []models.Invoice{}
	}

	// Drafts waiting to be issued, by run, for the "issue all" bar.
	type draftGroup struct {
		BillingMonth *time.Time `json:"billing_month"`
		Count        int64      `json:"count"`
		Total        float64    `json:"total"`
	}
	var drafts []draftGroup
	database.DB.Model(&models.Invoice{}).Where("status = ?", models.InvoiceDraft).
		Select("billing_month, COUNT(*) AS count, COALESCE(SUM(total), 0) AS total").
		Group("billing_month").Order("billing_month DESC NULLS LAST").Scan(&drafts)
	for i := range drafts {
		drafts[i].Total = roundMoney(drafts[i].Total)
	}
	if drafts == nil {
		drafts = []draftGroup{}
	}

	c.JSON(http.StatusOK, gin.H{
		"invoices": list, "total": sum.Count, "amount": sum.Total, "page": page, "page_size": size,
		"drafts": drafts, "can_manage": auth.Can(auth.User(c).Role, "manage_billing"),
	})
}

func loadInvoice(tx *gorm.DB, id uint) (*models.Invoice, error) {
	var inv models.Invoice
	err := tx.Preload("Client").Preload("Company").Preload("Lines", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC, id ASC") }).
		First(&inv, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errNotFound
	}
	return &inv, err
}

func GetInvoice(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	inv, err := loadInvoice(database.DB, id)
	if err == errNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invoice not found"})
		return
	} else if err != nil {
		serverError(c, "Could not load the invoice", err)
		return
	}
	// A draft always shows the client's current details.
	if inv.Status == models.InvoiceDraft && inv.Client != nil {
		copyClientDetails(inv, inv.Client)
	}
	company, _, _ := invoiceIssuer(inv)
	var history []models.AuditLog
	database.DB.Where("entity_type = ? AND entity_id = ?", "invoice", inv.ID).Order("id DESC").Limit(50).Find(&history)
	// Payments applied to it (all statuses, so rejected / reversed show too).
	type payRow struct {
		PaymentID     uint      `json:"payment_id"`
		PaymentNumber string    `json:"payment_number"`
		PaymentDate   time.Time `json:"payment_date"`
		Method        string    `json:"method"`
		Status        string    `json:"status"`
		Amount        float64   `json:"amount"`
	}
	payments := []payRow{}
	database.DB.Raw(`SELECT p.id AS payment_id, p.payment_number, p.payment_date, p.method, p.status, a.amount
		FROM payment_allocations a JOIN payments p ON p.id = a.payment_id WHERE a.invoice_id = ? ORDER BY p.payment_date, p.id`, inv.ID).Scan(&payments)
	// Unit and category of each service line's product, for the printed
	// invoice (it groups lines by category). Typed-in lines have none.
	type lineInfo struct {
		LineID   uint   `json:"line_id"`
		Unit     string `json:"unit"`
		Category string `json:"category"`
	}
	lineInfos := []lineInfo{}
	database.DB.Raw(`SELECT l.id AS line_id, COALESCE(p.unit, '') AS unit, COALESCE(p.category, '') AS category
		FROM invoice_lines l JOIN subscriptions s ON s.id = l.subscription_id JOIN products p ON p.id = s.product_id
		WHERE l.invoice_id = ?`, inv.ID).Scan(&lineInfos)
	c.JSON(http.StatusOK, gin.H{"invoice": inv, "company": company, "history": history, "today": today(), "payments": payments,
		"line_info": lineInfos, "can_manage": auth.Can(auth.User(c).Role, "manage_billing")})
}

// ---- drafts -----------------------------------------------------------------

type lineInput struct {
	ID          uint   `json:"id"` // an existing line of this draft; 0 = new typed-in line
	Description string `json:"description"`
	Reference   string `json:"reference"`
	// Heading and unit printed on the invoice. Left out (null): unchanged.
	Category *string `json:"category"`
	Unit     *string `json:"unit"`
	Quantity float64 `json:"quantity"`
	Rate     float64 `json:"rate"`
	// The line's taxes. Left out (null): an existing line keeps its taxes,
	// a new line has none.
	TaxIDs []uint `json:"tax_ids"`
}

type invoiceInput struct {
	ClientID    uint        `json:"client_id"`  // new drafts only
	CompanyID   uint        `json:"company_id"` // the billing company (0: new = default, edit = unchanged)
	InvoiceDate string      `json:"invoice_date"`
	DueDate     string      `json:"due_date"`
	TaxPercent  *float64    `json:"tax_percent"` // the old single tax % (only older drafts have one)
	Notes       string      `json:"notes"`
	Lines       []lineInput `json:"lines"`
}

const maxLines = 300

// applyLines rebuilds the draft's lines from the input. Lines that came
// from a service keep their service, period and days (only the wording,
// quantity and rate can change); new lines are typed-in charges (a
// negative rate makes a discount).
func applyLines(inv *models.Invoice, in []lineInput, taxes map[uint]models.Tax) string {
	if msg := checkLinesCompany(inv, in, taxes); msg != "" {
		return msg
	}
	if len(in) > maxLines {
		return fmt.Sprintf("An invoice can have at most %d lines", maxLines)
	}
	existing := map[uint]models.InvoiceLine{}
	for _, l := range inv.Lines {
		existing[l.ID] = l
	}
	var out []models.InvoiceLine
	for i, li := range in {
		n := strconv.Itoa(i + 1)
		var l models.InvoiceLine
		if li.ID != 0 {
			old, ok := existing[li.ID]
			if !ok {
				return "Line " + n + " is not on this invoice"
			}
			l = old
		}
		var ok bool
		if l.Description, ok = trimTo(li.Description, 300); !ok {
			return "Line " + n + ": description is too long"
		}
		if l.Description == "" {
			return "Line " + n + ": enter a description"
		}
		if l.Reference, ok = trimTo(li.Reference, 100); !ok {
			return "Line " + n + ": reference is too long"
		}
		if li.Category != nil {
			if l.Category, ok = trimTo(*li.Category, 60); !ok {
				return "Line " + n + ": heading is too long (max 60)"
			}
		}
		if li.Unit != nil {
			if l.Unit, ok = trimTo(*li.Unit, 40); !ok {
				return "Line " + n + ": unit is too long (max 40)"
			}
		}
		if badNumber(li.Quantity) || li.Quantity <= 0 || li.Quantity > 1000000 {
			return "Line " + n + ": quantity must be more than 0"
		}
		if badNumber(li.Rate) || math_abs(li.Rate) > 999999999999 {
			return "Line " + n + ": the rate is not valid"
		}
		if l.SubscriptionID != nil && li.Rate < 0 {
			return "Line " + n + ": a service's rate can't be negative"
		}
		l.Quantity, l.Rate = roundMoney(li.Quantity), roundMoney(li.Rate)
		l.Amount = lineAmount(l.Quantity, l.Rate, l.ProrateDays, l.ProrateOf)
		if li.TaxIDs != nil {
			ids, msg := checkTaxIDs(li.TaxIDs, taxIDsOf(l.Taxes), taxes, inv.CompanyID)
			if msg != "" {
				return "Line " + n + ": " + msg
			}
			l.Taxes = snapshotTaxes(ids, taxes)
		}
		l.SortOrder = i
		out = append(out, l)
	}
	inv.Lines = out
	return ""
}

// checkLinesCompany: an existing line that keeps its taxes must have this
// company's taxes (they would be wrong after the company changed).
func checkLinesCompany(inv *models.Invoice, in []lineInput, taxes map[uint]models.Tax) string {
	existing := map[uint]models.InvoiceLine{}
	for _, l := range inv.Lines {
		existing[l.ID] = l
	}
	for i, li := range in {
		if li.ID == 0 || li.TaxIDs != nil {
			continue
		}
		for _, id := range taxIDsOf(existing[li.ID].Taxes) {
			if t, ok := taxes[id]; ok && t.CompanyID != inv.CompanyID {
				return "Line " + strconv.Itoa(i+1) + ": " + t.Name + " is another company's tax; choose this company's taxes"
			}
		}
	}
	return ""
}

func math_abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// applyInvoiceHeader: dates, tax %, notes.
func applyInvoiceHeader(inv *models.Invoice, in invoiceInput, termsDays int) string {
	d, err := parseDay(in.InvoiceDate)
	if err != nil || d == nil {
		return "Give the invoice date"
	}
	due, err := parseDay(in.DueDate)
	if err != nil {
		return "The due date must be a date"
	}
	if due == nil {
		t := d.AddDate(0, 0, termsDays)
		due = &t
	}
	if due.Before(*d) {
		return "The due date can't be before the invoice date"
	}
	// The old single tax %: only an older draft that has one can change it
	// (or remove it); new invoices use each line's taxes.
	if in.TaxPercent != nil && inv.TaxPercent > 0 {
		if badNumber(*in.TaxPercent) || *in.TaxPercent < 0 || *in.TaxPercent > 100 {
			return "Tax % must be between 0 and 100"
		}
		inv.TaxPercent = roundMoney(*in.TaxPercent)
	}
	var ok bool
	if inv.Notes, ok = trimTo(in.Notes, 5000); !ok {
		return "Notes are too long"
	}
	inv.InvoiceDate, inv.DueDate = *d, *due
	return ""
}

// companySettings: the general settings, with the default company's
// details (for pages that print one company).
func companySettings() models.CompanySettings { return loadSettings() }

func CreateInvoice(c *gin.Context) {
	var in invoiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	var cl models.Client
	if in.ClientID == 0 || database.DB.First(&cl, in.ClientID).Error != nil {
		badRequest(c, "Choose a client")
		return
	}
	if cl.Status == "archived" {
		badRequest(c, cl.Name+" is archived. Restore the client first.")
		return
	}
	if strings.TrimSpace(in.InvoiceDate) == "" {
		in.InvoiceDate = today()
	}
	co, msg := pickCompany(database.DB, in.CompanyID, true)
	if msg != "" {
		badRequest(c, msg)
		return
	}
	uid := auth.User(c).ID
	inv := models.Invoice{ClientID: cl.ID, CompanyID: co.ID, Status: models.InvoiceDraft,
		Currency: companySettings().Currency, CreatedByID: uid, UpdatedByID: uid}
	in.TaxPercent = nil
	if msg := applyInvoiceHeader(&inv, in, cl.TermsDays); msg != "" {
		badRequest(c, msg)
		return
	}
	taxes, err := allTaxes(database.DB)
	if err != nil {
		serverError(c, "Could not load taxes", err)
		return
	}
	if msg := applyLines(&inv, in.Lines, taxes); msg != "" {
		badRequest(c, msg)
		return
	}
	copyClientDetails(&inv, &cl)
	recalcInvoice(&inv)
	if err := database.DB.Create(&inv).Error; err != nil {
		serverError(c, "Could not create the invoice", err)
		return
	}
	audit(c, "created", "invoice", inv.ID, "Draft for "+cl.ClientNumber+" "+cl.Name+" from "+co.Name)
	c.JSON(http.StatusCreated, gin.H{"invoice": inv})
}

// loadDraft loads an invoice for changing; answers and returns nil unless
// it is a draft.
func loadDraft(c *gin.Context, tx *gorm.DB) *models.Invoice {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	inv, err := loadInvoice(tx, id)
	if err == errNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invoice not found"})
		return nil
	} else if err != nil {
		serverError(c, "Could not load the invoice", err)
		return nil
	}
	if inv.Status != models.InvoiceDraft {
		badRequest(c, "Only a draft can be changed. An issued invoice can only be cancelled.")
		return nil
	}
	return inv
}

// saveDraftLines replaces the draft's lines and totals.
func saveDraftLines(tx *gorm.DB, inv *models.Invoice) error {
	if err := tx.Where("invoice_id = ?", inv.ID).Delete(&models.InvoiceLine{}).Error; err != nil {
		return err
	}
	for i := range inv.Lines {
		inv.Lines[i].ID = 0
		inv.Lines[i].InvoiceID = inv.ID
	}
	if len(inv.Lines) > 0 {
		if err := tx.Create(&inv.Lines).Error; err != nil {
			return err
		}
	}
	return tx.Omit("Client", "Company", "Lines").Save(inv).Error
}

func UpdateInvoice(c *gin.Context) {
	var in invoiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockBilling(tx); err != nil {
			return err
		}
		inv := loadDraft(c, tx)
		if inv == nil {
			return errAnswered
		}
		before := fmt.Sprintf("%d lines, total %s", len(inv.Lines), money(inv.Total))
		companyNote := ""
		if in.CompanyID != 0 && in.CompanyID != inv.CompanyID {
			co, msg := pickCompany(tx, in.CompanyID, true)
			if msg != "" {
				badRequest(c, msg)
				return errAnswered
			}
			for _, l := range inv.Lines {
				if l.SubscriptionID != nil {
					badRequest(c, "This draft has service charges, which are billed by their own company. Remove them first to change the company.")
					return errAnswered
				}
			}
			companyNote = "; company " + companyNameOf(inv.Company) + " → " + co.Name
			inv.CompanyID, inv.Company = co.ID, co
		}
		if msg := applyInvoiceHeader(inv, in, inv.Client.TermsDays); msg != "" {
			badRequest(c, msg)
			return errAnswered
		}
		taxes, err := allTaxes(tx)
		if err != nil {
			return err
		}
		if msg := applyLines(inv, in.Lines, taxes); msg != "" {
			badRequest(c, msg)
			return errAnswered
		}
		recalcInvoice(inv)
		inv.UpdatedByID = auth.User(c).ID
		if err := saveDraftLines(tx, inv); err != nil {
			return err
		}
		auditTx(tx, c, "updated", "invoice", inv.ID, "Draft for "+inv.Client.Name+": "+before+" → "+
			fmt.Sprintf("%d lines, total %s", len(inv.Lines), money(inv.Total))+companyNote)
		inv.Client = nil
		c.JSON(http.StatusOK, gin.H{"invoice": inv})
		return nil
	})
	if err != nil && err != errAnswered {
		serverError(c, "Could not save the invoice", err)
	}
}

func DeleteInvoice(c *gin.Context) {
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockBilling(tx); err != nil {
			return err
		}
		inv := loadDraft(c, tx)
		if inv == nil {
			return errAnswered
		}
		if err := tx.Where("invoice_id = ?", inv.ID).Delete(&models.InvoiceLine{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&models.Invoice{}, inv.ID).Error; err != nil {
			return err
		}
		auditTx(tx, c, "deleted", "invoice", inv.ID, fmt.Sprintf("Draft for %s (%d lines, total %s); its service charges can be billed again",
			inv.Client.Name, len(inv.Lines), money(inv.Total)))
		c.JSON(http.StatusOK, gin.H{"deleted": true})
		return nil
	})
	if err != nil && err != errAnswered {
		serverError(c, "Could not delete the draft", err)
	}
}

// AddDueCharges adds the client's service charges due up to the end of the
// draft's invoice month (that aren't on any invoice yet).
func AddDueCharges(c *gin.Context) {
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockBilling(tx); err != nil {
			return err
		}
		inv := loadDraft(c, tx)
		if inv == nil {
			return errAnswered
		}
		var subs []models.Subscription
		if err := tx.Where("client_id = ? AND company_id = ? AND status = 'active'", inv.ClientID, inv.CompanyID).Find(&subs).Error; err != nil {
			return err
		}
		due, err := dueForServices(tx, subs, inv.InvoiceDate)
		if err != nil {
			return err
		}
		added := due[billKey{inv.ClientID, inv.CompanyID}]
		for i := range added {
			added[i].SortOrder = len(inv.Lines) + i
		}
		inv.Lines = append(inv.Lines, added...)
		recalcInvoice(inv)
		inv.UpdatedByID = auth.User(c).ID
		if err := saveDraftLines(tx, inv); err != nil {
			return err
		}
		if len(added) > 0 {
			auditTx(tx, c, "updated", "invoice", inv.ID, fmt.Sprintf("Added %d service charge(s) due to %s; total %s",
				len(added), monthEnd(inv.InvoiceDate).Format("2006-01-02"), money(inv.Total)))
		}
		c.JSON(http.StatusOK, gin.H{"added": len(added)})
		return nil
	})
	if err != nil && err != errAnswered {
		serverError(c, "Could not add the charges", err)
	}
}

// ---- bulk monthly run -------------------------------------------------------

type generateInput struct {
	Month       string `json:"month"`        // YYYY-MM
	InvoiceDate string `json:"invoice_date"` // default: 1st of the month
	CompanyID   uint   `json:"company_id"`   // only this company's services (0 = all)
	Preview     bool   `json:"preview"`
}

type runClient struct {
	ClientID     uint                 `json:"client_id"`
	ClientNumber string               `json:"client_number"`
	Name         string               `json:"name"`
	CompanyID    uint                 `json:"company_id"`
	CompanyName  string               `json:"company_name"`
	Lines        []models.InvoiceLine `json:"lines"`
	Subtotal     float64              `json:"subtotal"`
	TaxLines     []models.TaxApplied  `json:"tax_lines"`
	Tax          float64              `json:"tax"`      // added
	Deducted     float64              `json:"deducted"` // deducted
	Total        float64              `json:"total"`
	Earlier      int                  `json:"earlier"`       // lines for periods before the month
	AddsToDraft  string               `json:"adds_to_draft"` // existing draft it is added to
}

// GenerateInvoices: one draft per active client and billing company with
// charges due up to the end of the month. Running it again only adds what's new (to the same
// drafts). With preview, nothing is saved.
func GenerateInvoices(c *gin.Context) {
	var in generateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	month, err := parseMonth(in.Month)
	if err != nil || month == nil {
		badRequest(c, "Choose the month to bill")
		return
	}
	invDate := *month
	if d, err := parseDay(in.InvoiceDate); err != nil {
		badRequest(c, "The invoice date must be a date")
		return
	} else if d != nil {
		invDate = *d
	}
	if lim := todayDate().AddDate(1, 0, 0); month.After(lim) {
		badRequest(c, "That month is too far ahead")
		return
	}
	currency := companySettings().Currency

	err = database.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockBilling(tx); err != nil {
			return err
		}
		var subs []models.Subscription
		sq := tx.Joins("JOIN clients ON clients.id = subscriptions.client_id").
			Where("subscriptions.status = 'active' AND clients.status = 'active' AND subscriptions.start_date <= ?", monthEnd(*month).Format("2006-01-02"))
		if in.CompanyID != 0 {
			sq = sq.Where("subscriptions.company_id = ?", in.CompanyID)
		}
		if err := sq.Find(&subs).Error; err != nil {
			return err
		}
		coNames := companyNames(tx)
		due, err := dueForServices(tx, subs, *month)
		if err != nil {
			return err
		}
		var clients []models.Client
		ids := make([]uint, 0, len(due))
		byClient := map[uint][]uint{} // client → its billing companies (by name)
		for k := range due {
			if _, ok := byClient[k.ClientID]; !ok {
				ids = append(ids, k.ClientID)
			}
			byClient[k.ClientID] = append(byClient[k.ClientID], k.CompanyID)
		}
		for _, cos := range byClient {
			sort.Slice(cos, func(i, j int) bool {
				if coNames[cos[i]] != coNames[cos[j]] {
					return coNames[cos[i]] < coNames[cos[j]]
				}
				return cos[i] < cos[j]
			})
		}
		if len(ids) > 0 {
			if err := tx.Where("id IN ?", ids).Order("LOWER(name), id").Find(&clients).Error; err != nil {
				return err
			}
		}
		// Existing drafts of this run.
		var existing []models.Invoice
		tx.Preload("Lines").Where("status = 'draft' AND billing_month = ?", month.Format("2006-01-02")).Find(&existing)
		draftOf := map[billKey]*models.Invoice{}
		for i := range existing {
			draftOf[billKey{existing[i].ClientID, existing[i].CompanyID}] = &existing[i]
		}

		var out []runClient
		var created, updated int
		var grand float64
		uid := auth.User(c).ID
		for i := range clients {
			cl := &clients[i]
			for _, coID := range byClient[cl.ID] {
				key := billKey{cl.ID, coID}
				lines := due[key]
				rc := runClient{ClientID: cl.ID, ClientNumber: cl.ClientNumber, Name: cl.Name, CompanyID: coID, CompanyName: coNames[coID]}
				for _, l := range lines {
					if l.PeriodStart.Before(*month) {
						rc.Earlier++
					}
				}
				inv := draftOf[key]
				if inv != nil {
					rc.AddsToDraft = fmt.Sprintf("draft #%d", inv.ID)
				}
				// What this run adds, with its taxes.
				tmp := models.Invoice{Lines: lines}
				recalcInvoice(&tmp)
				lines = tmp.Lines
				rc.Lines, rc.Subtotal, rc.TaxLines = lines, tmp.Subtotal, tmp.TaxLines
				rc.Tax, rc.Deducted, rc.Total = tmp.TaxAmount, tmp.DeductedAmount, tmp.Total
				grand += rc.Total
				out = append(out, rc)
				if in.Preview {
					continue
				}
				if inv == nil {
					inv = &models.Invoice{ClientID: cl.ID, CompanyID: coID, Status: models.InvoiceDraft, BillingMonth: month,
						InvoiceDate: invDate, DueDate: invDate.AddDate(0, 0, cl.TermsDays),
						Currency: currency, CreatedByID: uid, UpdatedByID: uid}
					copyClientDetails(inv, cl)
					if err := tx.Omit("Client", "Company", "Lines").Create(inv).Error; err != nil {
						return err
					}
					auditTx(tx, c, "created", "invoice", inv.ID, "Draft made by the monthly run for "+month.Format("January 2006")+" from "+coNames[coID])
					created++
				} else {
					updated++
				}
				for j := range lines {
					lines[j].SortOrder = len(inv.Lines) + j
				}
				inv.Lines = append(inv.Lines, lines...)
				recalcInvoice(inv)
				inv.UpdatedByID = uid
				if err := saveDraftLines(tx, inv); err != nil {
					return err
				}
			}
		}
		if out == nil {
			out = []runClient{}
		}
		if !in.Preview && len(out) > 0 {
			auditTx(tx, c, "generated", "invoice", 0, fmt.Sprintf("Monthly invoices for %s: %d new draft(s), %d draft(s) added to, total %s",
				month.Format("January 2006"), created, updated, money(roundMoney(grand))))
		}
		c.JSON(http.StatusOK, gin.H{"month": month.Format("2006-01"), "invoice_date": invDate.Format("2006-01-02"),
			"clients": out, "total": roundMoney(grand), "created": created, "updated": updated, "preview": in.Preview})
		return nil
	})
	if err != nil {
		serverError(c, "Could not make the invoices", err)
	}
}

// ---- issue / cancel ---------------------------------------------------------

var prefixClean = regexp.MustCompile(`[^A-Z0-9-]`)
var prefixDashes = regexp.MustCompile(`-{2,}`)

type issueInput struct {
	IDs []uint `json:"ids"`
}

// IssueInvoices issues the given drafts, in client-name order, each with
// the next number. A draft that can't be issued is skipped with a reason.
func IssueInvoices(c *gin.Context) {
	var in issueInput
	if err := c.ShouldBindJSON(&in); err != nil || len(in.IDs) == 0 {
		badRequest(c, "Choose the drafts to issue")
		return
	}
	if len(in.IDs) > 2000 {
		badRequest(c, "Too many at once (max 2000)")
		return
	}
	type skipped struct {
		ID     uint   `json:"id"`
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	var issued []string
	var skips []skipped
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockBilling(tx); err != nil {
			return err
		}
		var drafts []models.Invoice
		if err := tx.Preload("Client").Preload("Company").Preload("Lines").Joins("JOIN clients ON clients.id = invoices.client_id").
			Where("invoices.id IN ? AND invoices.status = 'draft'", in.IDs).
			Order("invoices.invoice_date, LOWER(clients.name), invoices.id").Find(&drafts).Error; err != nil {
			return err
		}
		now := time.Now()
		uid := auth.User(c).ID
		for i := range drafts {
			inv := &drafts[i]
			switch {
			case len(inv.Lines) == 0:
				skips = append(skips, skipped{inv.ID, inv.Client.Name, "no lines"})
				continue
			case inv.Total < 0:
				skips = append(skips, skipped{inv.ID, inv.Client.Name, "the total is below zero"})
				continue
			case inv.Client.Status == "archived":
				skips = append(skips, skipped{inv.ID, inv.Client.Name, "the client is archived"})
				continue
			case inv.Company == nil:
				skips = append(skips, skipped{inv.ID, inv.Client.Name, "no billing company"})
				continue
			case !inv.Company.Active:
				skips = append(skips, skipped{inv.ID, inv.Client.Name, inv.Company.Name + " is inactive"})
				continue
			}
			num, err := models.NextID(tx, invoicePrefixOf(inv.Company))
			if err != nil {
				return err
			}
			copyClientDetails(inv, inv.Client)
			// The company as printed, with the logo it has now.
			inv.Issuer = issuerOf(inv.Company)
			inv.IssuerLogoPath, inv.IssuerLogoType = inv.Company.LogoPath, inv.Company.LogoType
			recalcInvoice(inv)
			inv.InvoiceNumber, inv.Status = num, models.InvoiceIssued
			inv.IssuedAt, inv.IssuedByID, inv.UpdatedByID = &now, uid, uid
			if err := tx.Omit("Client", "Company", "Lines").Save(inv).Error; err != nil {
				return err
			}
			if err := refreshBilledThrough(tx, lineServiceIDs(inv.Lines)); err != nil {
				return err
			}
			auditTx(tx, c, "issued", "invoice", inv.ID, fmt.Sprintf("%s from %s to %s %s, total %s %s",
				num, inv.Company.Name, inv.Client.ClientNumber, inv.Client.Name, inv.Currency, money(inv.Total)))
			issued = append(issued, num)
		}
		return nil
	})
	if err != nil {
		serverError(c, "Could not issue the invoices", err)
		return
	}
	if issued == nil {
		issued = []string{}
	}
	if skips == nil {
		skips = []skipped{}
	}
	c.JSON(http.StatusOK, gin.H{"issued": issued, "skipped": skips})
}

func CancelInvoice(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	reason, fits := trimTo(in.Reason, 1000)
	if reason == "" || !fits {
		badRequest(c, "Give the reason for cancelling (max 1000 characters)")
		return
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockBilling(tx); err != nil {
			return err
		}
		inv, err := loadInvoice(tx, id)
		if err == errNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Invoice not found"})
			return errAnswered
		} else if err != nil {
			return err
		}
		if inv.Status != models.InvoiceIssued {
			badRequest(c, "Only an issued invoice can be cancelled (a draft can be deleted)")
			return errAnswered
		}
		var paid []string
		tx.Raw(`SELECT p.payment_number FROM payment_allocations a JOIN payments p ON p.id = a.payment_id
			WHERE a.invoice_id = ? AND p.status IN ('pending', 'verified') ORDER BY p.id`, inv.ID).Scan(&paid)
		if len(paid) > 0 {
			badRequest(c, "Payments are applied to this invoice ("+strings.Join(paid, ", ")+"). Reject or reverse them first.")
			return errAnswered
		}
		now := time.Now()
		inv.Status, inv.CancelledAt, inv.CancelledByID, inv.CancelReason = models.InvoiceCancelled, &now, auth.User(c).ID, reason
		inv.UpdatedByID = auth.User(c).ID
		if err := tx.Omit("Client", "Company", "Lines").Save(inv).Error; err != nil {
			return err
		}
		if err := refreshBilledThrough(tx, lineServiceIDs(inv.Lines)); err != nil {
			return err
		}
		auditTx(tx, c, "cancelled", "invoice", inv.ID, inv.InvoiceNumber+" ("+inv.BillName+", "+money(inv.Total)+"): "+reason)
		inv.Client, inv.Company, inv.Lines = nil, nil, nil
		c.JSON(http.StatusOK, gin.H{"invoice": inv})
		return nil
	})
	if err != nil && err != errAnswered {
		serverError(c, "Could not cancel the invoice", err)
	}
}
