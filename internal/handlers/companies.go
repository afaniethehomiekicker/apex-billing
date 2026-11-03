package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Our companies (the ones that bill clients).
//
//	GET  /api/companies                ?status=active (default) | all   (view_billing)
//	GET  /api/companies/:id            with its history                (view_billing)
//	GET  /api/companies/:id/logo       the logo picture                (view_billing)
//	POST /api/companies                (manage_settings)
//	PUT  /api/companies/:id            (manage_settings; everything sent each time)
//	POST /api/companies/:id/logo       upload / replace: multipart "file" (manage_settings)
//	POST /api/companies/:id/logo/remove                                 (manage_settings)
//
// Each company has its own details, logo, bank details, invoice prefix and
// counter, and taxes. Each service says which company bills it; invoices
// are made per client and company. One company is the default. Never
// deleted: made inactive.

const maxLogoBytes = 2 << 20

var logoTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

// Prefixes used for other permanent numbers can't be an invoice prefix.
var reservedPrefixes = map[string]bool{"BU": true, "CUS": true, "SUB": true, "PAY": true, "CON": true}

func companyByID(tx *gorm.DB, id uint) (*models.Company, error) {
	var co models.Company
	err := tx.First(&co, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errNotFound
	}
	return &co, err
}

// defaultCompany: the company marked default (or the first one).
func defaultCompany(tx *gorm.DB) (*models.Company, error) {
	var co models.Company
	err := tx.Order("is_default DESC, id ASC").First(&co).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errNotFound
	}
	return &co, err
}

// pickCompany: the chosen company (0 = the default). A new choice must be
// active. Answers with the reason when it can't be used.
func pickCompany(tx *gorm.DB, id uint, mustBeActive bool) (*models.Company, string) {
	var co *models.Company
	var err error
	if id == 0 {
		co, err = defaultCompany(tx)
	} else {
		co, err = companyByID(tx, id)
	}
	if err != nil {
		return nil, "Choose the company that bills this"
	}
	if mustBeActive && !co.Active {
		return nil, co.Name + " is inactive. Choose another company."
	}
	return co, ""
}

func companyNames(tx *gorm.DB) map[uint]string {
	var list []models.Company
	tx.Select("id, name").Find(&list)
	out := make(map[uint]string, len(list))
	for _, c := range list {
		out[c.ID] = c.Name
	}
	return out
}

func issuerOf(co *models.Company) models.Issuer {
	return models.Issuer{Name: co.Name, Address: co.Address, Phone: co.Phone, Email: co.Email, Website: co.Website,
		NTN: co.NTN, STRN: co.STRN, BankDetails: co.BankDetails, InvoiceFooter: co.InvoiceFooter}
}

// cleanPrefix: upper case, letters and digits only, at most 6.
func cleanPrefix(p string) string {
	p = prefixClean.ReplaceAllString(strings.ToUpper(p), "")
	if len(p) > 6 {
		p = p[:6]
	}
	return p
}

// invoicePrefixOf: the prefix for the company's next invoice number.
func invoicePrefixOf(co *models.Company) string {
	if co == nil {
		return "INV"
	}
	if p := cleanPrefix(co.InvoicePrefix); p != "" {
		return p
	}
	return "INV"
}

// ---- startup ----------------------------------------------------------------

// EnsureCompanies (every start, after the tables): the first time, makes
// the first company from the old single company settings and puts every
// existing service, invoice and tax under it. Makes sure exactly one
// company is the default and prefixes are unique.
func EnsureCompanies() {
	db := database.DB
	var n int64
	db.Model(&models.Company{}).Count(&n)
	if n == 0 {
		s := loadSettingsRow()
		name := strings.TrimSpace(s.CompanyName)
		if name == "" {
			name = strings.TrimSpace(os.Getenv("COMPANY_NAME"))
		}
		if name == "" {
			name = "My company"
		}
		prefix := cleanPrefix(s.InvoicePrefix)
		if prefix == "" {
			prefix = "INV"
		}
		co := models.Company{Name: name, Address: s.Address, Phone: s.Phone, Email: s.Email, Website: s.Website,
			NTN: s.NTN, STRN: s.STRN, BankDetails: s.BankDetails, InvoiceFooter: s.InvoiceFooter,
			InvoicePrefix: prefix, IsDefault: true, Active: true}
		if err := db.Create(&co).Error; err != nil {
			log.Printf("companies: making the first company: %v", err)
			return
		}
		log.Printf("Companies: %q made from the company settings; existing services, invoices and taxes are under it", name)
	}
	def, err := defaultCompany(db)
	if err != nil {
		log.Printf("companies: %v", err)
		return
	}
	// Exactly one default.
	db.Model(&models.Company{}).Where("id <> ? AND is_default", def.ID).Update("is_default", false)
	if !def.IsDefault {
		db.Model(&models.Company{}).Where("id = ?", def.ID).Updates(map[string]interface{}{"is_default": true, "active": true})
	}
	for _, t := range []string{"subscriptions", "invoices", "taxes"} {
		if err := db.Exec("UPDATE "+t+" SET company_id = ? WHERE company_id = 0 OR company_id IS NULL", def.ID).Error; err != nil {
			log.Printf("companies: %s: %v", t, err)
		}
	}
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_companies_invoice_prefix ON companies (UPPER(invoice_prefix))`)
}

// ---- list / view ------------------------------------------------------------

// companyView adds what pages need to show.
type companyView struct {
	models.Company
	HasLogo  bool  `json:"has_logo"`
	Services int64 `json:"services"` // current services it bills
}

func companyViews(list []models.Company) []companyView {
	counts := map[uint]int64{}
	type row struct {
		CompanyID uint
		N         int64
	}
	var rows []row
	database.DB.Model(&models.Subscription{}).Select("company_id, COUNT(*) AS n").Where(currentServiceSQL, today()).
		Group("company_id").Scan(&rows)
	for _, r := range rows {
		counts[r.CompanyID] = r.N
	}
	out := make([]companyView, 0, len(list))
	for _, c := range list {
		out = append(out, companyView{Company: c, HasLogo: c.LogoPath != "", Services: counts[c.ID]})
	}
	return out
}

func GetCompanies(c *gin.Context) {
	q := database.DB.Model(&models.Company{})
	switch c.DefaultQuery("status", "active") {
	case "active":
		q = q.Where("active = ?", true)
	case "all":
	default:
		badRequest(c, "Unknown status")
		return
	}
	var list []models.Company
	if err := q.Order("is_default DESC, LOWER(name), id").Limit(500).Find(&list).Error; err != nil {
		serverError(c, "Could not load the companies", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"companies": companyViews(list), "can_manage": auth.Can(auth.User(c).Role, "manage_settings")})
}

func GetCompany(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	co, err := companyByID(database.DB, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Company not found"})
		return
	}
	var history []models.AuditLog
	database.DB.Where("entity_type = ? AND entity_id = ?", "company", co.ID).Order("id DESC").Limit(50).Find(&history)
	c.JSON(http.StatusOK, gin.H{"company": companyViews([]models.Company{*co})[0], "history": history,
		"can_manage": auth.Can(auth.User(c).Role, "manage_settings")})
}

// ---- add / change -----------------------------------------------------------

type companyInput struct {
	Name          string `json:"name"`
	Address       string `json:"address"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	Website       string `json:"website"`
	NTN           string `json:"ntn"`
	STRN          string `json:"strn"`
	BankDetails   string `json:"bank_details"`
	InvoiceFooter string `json:"invoice_footer"`
	InvoicePrefix string `json:"invoice_prefix"`
	IsDefault     bool   `json:"is_default"`
	Active        *bool  `json:"active"`
}

func applyCompanyInput(in companyInput, co *models.Company) string {
	fields := []struct {
		dst   *string
		src   string
		max   int
		label string
	}{
		{&co.Name, in.Name, 200, "Company name"}, {&co.Address, in.Address, 1000, "Address"}, {&co.Phone, in.Phone, 60, "Phone"},
		{&co.Email, in.Email, 200, "Email"}, {&co.Website, in.Website, 200, "Website"}, {&co.NTN, in.NTN, 40, "NTN"},
		{&co.STRN, in.STRN, 40, "STRN"}, {&co.BankDetails, in.BankDetails, 2000, "Bank details"},
		{&co.InvoiceFooter, in.InvoiceFooter, 2000, "Invoice footer"},
	}
	for _, f := range fields {
		v, ok := trimTo(f.src, f.max)
		if !ok {
			return f.label + " is too long"
		}
		*f.dst = v
	}
	if co.Name == "" {
		return "Enter the company name (it is printed on its invoices)"
	}
	p := strings.ToUpper(strings.TrimSpace(in.InvoicePrefix))
	if p == "" {
		return "Enter the invoice number prefix (e.g. INV)"
	}
	if len(p) > 6 || cleanPrefix(p) != p {
		return "The invoice prefix may only use letters and numbers (at most 6)"
	}
	if reservedPrefixes[p] {
		return p + " is used for other numbers (clients, services, payments…); choose another prefix"
	}
	co.InvoicePrefix = p
	if in.Active != nil {
		co.Active = *in.Active
	}
	return ""
}

// companyClash: another company with the same name or invoice prefix.
func companyClash(co *models.Company) string {
	var other models.Company
	if database.DB.Where("id <> ? AND UPPER(invoice_prefix) = ?", co.ID, co.InvoicePrefix).First(&other).Error == nil {
		return other.Name + " already uses the invoice prefix " + co.InvoicePrefix + ". Each company needs its own."
	}
	if database.DB.Where("id <> ? AND LOWER(TRIM(name)) = LOWER(?)", co.ID, co.Name).First(&other).Error == nil {
		return "There is already a company called " + other.Name
	}
	return ""
}

// makeDefault: this company becomes the only default.
func makeDefault(tx *gorm.DB, id uint) error {
	if err := tx.Model(&models.Company{}).Where("id <> ? AND is_default", id).Update("is_default", false).Error; err != nil {
		return err
	}
	return tx.Model(&models.Company{}).Where("id = ?", id).Update("is_default", true).Error
}

func CreateCompany(c *gin.Context) {
	var in companyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	co := models.Company{Active: true}
	if msg := applyCompanyInput(in, &co); msg != "" {
		badRequest(c, msg)
		return
	}
	if msg := companyClash(&co); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	if in.IsDefault && !co.Active {
		badRequest(c, "The default company must be active")
		return
	}
	uid := auth.User(c).ID
	co.CreatedByID, co.UpdatedByID = uid, uid
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&co).Error; err != nil {
			return err
		}
		if in.IsDefault {
			if err := makeDefault(tx, co.ID); err != nil {
				return err
			}
			co.IsDefault = true
		}
		auditTx(tx, c, "created", "company", co.ID, fmt.Sprintf("%s (invoice prefix %s)%s", co.Name, co.InvoicePrefix,
			map[bool]string{true: ", default", false: ""}[co.IsDefault]))
		return nil
	})
	if err != nil {
		serverError(c, "Could not add the company", err)
		return
	}
	// Its invoice counter continues from any numbers already using this
	// prefix (never reuses one).
	ensureInvoiceCounter(co.InvoicePrefix)
	c.JSON(http.StatusCreated, gin.H{"company": companyViews([]models.Company{co})[0]})
}

func UpdateCompany(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in companyInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	co, err := companyByID(database.DB, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Company not found"})
		return
	}
	old := *co
	if msg := applyCompanyInput(in, co); msg != "" {
		badRequest(c, msg)
		return
	}
	if msg := companyClash(co); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	if old.IsDefault && !in.IsDefault {
		badRequest(c, "Make another company the default instead (there is always one default)")
		return
	}
	if (old.IsDefault || in.IsDefault) && !co.Active {
		badRequest(c, "The default company can't be made inactive. Make another company the default first.")
		return
	}
	if old.Active && !co.Active {
		var n int64
		database.DB.Model(&models.Subscription{}).Where("company_id = ?", co.ID).Where(currentServiceSQL, today()).Count(&n)
		if n > 0 {
			badRequest(c, fmt.Sprintf("%s still bills %d current service(s). Move them to another company or end them first.", co.Name, n))
			return
		}
		database.DB.Model(&models.Invoice{}).Where("company_id = ? AND status = 'draft'", co.ID).Count(&n)
		if n > 0 {
			badRequest(c, fmt.Sprintf("%s has %d draft invoice(s). Issue or delete them first.", co.Name, n))
			return
		}
	}
	activeLabel := map[bool]string{true: "Active", false: "Inactive"}
	var ch changeList
	ch.add("Name", old.Name, co.Name)
	ch.add("Address", old.Address, co.Address)
	ch.add("Phone", old.Phone, co.Phone)
	ch.add("Email", old.Email, co.Email)
	ch.add("Website", old.Website, co.Website)
	ch.add("NTN", old.NTN, co.NTN)
	ch.add("STRN", old.STRN, co.STRN)
	ch.add("Bank details", old.BankDetails, co.BankDetails)
	ch.add("Invoice footer", old.InvoiceFooter, co.InvoiceFooter)
	ch.add("Invoice prefix", old.InvoicePrefix, co.InvoicePrefix)
	ch.add("Status", activeLabel[old.Active], activeLabel[co.Active])
	if in.IsDefault && !old.IsDefault {
		ch = append(ch, "Made the default company")
	}
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"company": companyViews([]models.Company{*co})[0]})
		return
	}
	co.UpdatedByID = auth.User(c).ID
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Company{}).Where("id = ?", co.ID).Updates(map[string]interface{}{
			"name": co.Name, "address": co.Address, "phone": co.Phone, "email": co.Email, "website": co.Website,
			"ntn": co.NTN, "strn": co.STRN, "bank_details": co.BankDetails, "invoice_footer": co.InvoiceFooter,
			"invoice_prefix": co.InvoicePrefix, "active": co.Active, "updated_by_id": co.UpdatedByID,
		}).Error; err != nil {
			return err
		}
		if in.IsDefault && !old.IsDefault {
			if err := makeDefault(tx, co.ID); err != nil {
				return err
			}
		}
		auditTx(tx, c, "updated", "company", co.ID, co.Name+": "+ch.String())
		return nil
	})
	if err != nil {
		serverError(c, "Could not save the company", err)
		return
	}
	if co.InvoicePrefix != old.InvoicePrefix {
		ensureInvoiceCounter(co.InvoicePrefix)
	}
	co, _ = companyByID(database.DB, co.ID)
	c.JSON(http.StatusOK, gin.H{"company": companyViews([]models.Company{*co})[0]})
}

// ensureInvoiceCounter raises the counter for a prefix to the highest
// invoice number already using it.
func ensureInvoiceCounter(prefix string) {
	var maxNum int64
	q := fmt.Sprintf(`SELECT COALESCE(MAX(CAST(SUBSTRING(invoice_number FROM %d) AS BIGINT)), 0) FROM invoices WHERE invoice_number ~ ?`, len(prefix)+2)
	if err := database.DB.Raw(q, "^"+prefix+"-[0-9]+$").Scan(&maxNum).Error; err != nil {
		log.Printf("counters: reading %s: %v", prefix, err)
		return
	}
	database.DB.Exec(`INSERT INTO id_counters (prefix, last) VALUES (?, ?)
		ON CONFLICT (prefix) DO UPDATE SET last = GREATEST(id_counters.last, EXCLUDED.last)`, prefix, maxNum)
}

// ---- logo -------------------------------------------------------------------

// UploadCompanyLogo stores a new logo (PNG, JPG or WEBP up to 2 MB). The old
// file stays: invoices issued with it keep showing it.
func UploadCompanyLogo(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	co, err := companyByID(database.DB, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Company not found"})
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		badRequest(c, "Choose the logo picture (PNG, JPG or WEBP, up to 2 MB)")
		return
	}
	rel, ctype, size, msg, err := saveUploadOf(fh, "logos", maxLogoBytes, "logo", logoTypes, "The logo must be a picture: PNG, JPG or WEBP")
	if msg != "" {
		badRequest(c, msg)
		return
	}
	if err != nil {
		serverError(c, "Could not save the logo", err)
		return
	}
	name, _ := trimTo(filepath.Base(fh.Filename), 200)
	if err := database.DB.Model(&models.Company{}).Where("id = ?", co.ID).Updates(map[string]interface{}{
		"logo_path": rel, "logo_name": name, "logo_type": ctype, "logo_size": size, "updated_by_id": auth.User(c).ID,
	}).Error; err != nil {
		os.Remove(filepath.Join(uploadDir(), filepath.FromSlash(rel)))
		serverError(c, "Could not save the logo", err)
		return
	}
	action, details := "updated", co.Name+": logo "+name+" ("+rel+")"
	if co.LogoPath != "" {
		details += ", was " + co.LogoName + " (" + co.LogoPath + ")"
	}
	audit(c, action, "company", co.ID, details)
	co, _ = companyByID(database.DB, co.ID)
	c.JSON(http.StatusOK, gin.H{"company": companyViews([]models.Company{*co})[0]})
}

// RemoveCompanyLogo: invoices from now on print without a logo (the file is
// kept for invoices already issued with it).
func RemoveCompanyLogo(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	co, err := companyByID(database.DB, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Company not found"})
		return
	}
	if co.LogoPath == "" {
		c.JSON(http.StatusOK, gin.H{"company": companyViews([]models.Company{*co})[0]})
		return
	}
	if err := database.DB.Model(&models.Company{}).Where("id = ?", co.ID).Updates(map[string]interface{}{
		"logo_path": "", "logo_name": "", "logo_type": "", "logo_size": 0, "updated_by_id": auth.User(c).ID,
	}).Error; err != nil {
		serverError(c, "Could not remove the logo", err)
		return
	}
	audit(c, "updated", "company", co.ID, co.Name+": logo removed (was "+co.LogoName+", "+co.LogoPath+")")
	co, _ = companyByID(database.DB, co.ID)
	c.JSON(http.StatusOK, gin.H{"company": companyViews([]models.Company{*co})[0]})
}

// serveLogo sends a stored logo picture.
func serveLogo(c *gin.Context, rel, ctype string) {
	if rel == "" || strings.Contains(rel, "..") {
		c.JSON(http.StatusNotFound, gin.H{"error": "No logo"})
		return
	}
	full := filepath.Join(uploadDir(), filepath.FromSlash(rel))
	if _, err := os.Stat(full); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "The logo file is missing from the server's uploads folder"})
		return
	}
	if _, ok := logoTypes[ctype]; !ok {
		ctype = "application/octet-stream"
	}
	c.Header("Content-Type", ctype)
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.File(full)
}

func GetCompanyLogo(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	co, err := companyByID(database.DB, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Company not found"})
		return
	}
	serveLogo(c, co.LogoPath, co.LogoType)
}

// printCompany: the billing company as an invoice prints it — what was
// copied at issue, or (drafts, older invoices) the company as it is now.
type printCompany struct {
	ID            uint   `json:"id"`
	CompanyName   string `json:"company_name"`
	Address       string `json:"address"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	Website       string `json:"website"`
	NTN           string `json:"ntn"`
	STRN          string `json:"strn"`
	BankDetails   string `json:"bank_details"`
	InvoiceFooter string `json:"invoice_footer"`
	Currency      string `json:"currency"`
	HasLogo       bool   `json:"has_logo"`
}

// invoiceIssuer: the details and logo file printed on this invoice.
func invoiceIssuer(inv *models.Invoice) (printCompany, string, string) {
	if inv.Status != models.InvoiceDraft && inv.Issuer.Name != "" {
		is := inv.Issuer
		return printCompany{ID: inv.CompanyID, CompanyName: is.Name, Address: is.Address, Phone: is.Phone, Email: is.Email,
			Website: is.Website, NTN: is.NTN, STRN: is.STRN, BankDetails: is.BankDetails, InvoiceFooter: is.InvoiceFooter,
			Currency: inv.Currency, HasLogo: inv.IssuerLogoPath != ""}, inv.IssuerLogoPath, inv.IssuerLogoType
	}
	co := inv.Company
	if co == nil {
		co, _ = companyByID(database.DB, inv.CompanyID)
	}
	if co == nil {
		co, _ = defaultCompany(database.DB)
	}
	if co == nil {
		return printCompany{Currency: inv.Currency}, "", ""
	}
	return printCompany{ID: co.ID, CompanyName: co.Name, Address: co.Address, Phone: co.Phone, Email: co.Email,
		Website: co.Website, NTN: co.NTN, STRN: co.STRN, BankDetails: co.BankDetails, InvoiceFooter: co.InvoiceFooter,
		Currency: inv.Currency, HasLogo: co.LogoPath != ""}, co.LogoPath, co.LogoType
}

// GetInvoiceLogo: the logo printed on this invoice.
func GetInvoiceLogo(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var inv models.Invoice
	if err := database.DB.Preload("Company").First(&inv, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invoice not found"})
		return
	}
	_, rel, ctype := invoiceIssuer(&inv)
	serveLogo(c, rel, ctype)
}

// companyParam reads ?company_id= (empty = all companies).
func companyParam(c *gin.Context) (uint, bool) {
	v := strings.TrimSpace(c.Query("company_id"))
	if v == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n == 0 {
		badRequest(c, "Invalid company")
		return 0, false
	}
	return uint(n), true
}
