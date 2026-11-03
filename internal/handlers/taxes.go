package handlers

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Taxes, set up by hand: as many as needed.
//
//	GET  /api/taxes        ?status=active (default) | inactive | all  &company_id=
//	GET  /api/taxes/:id    with how many products / services use it and its history
//	POST /api/taxes        (manage_billing)
//	PUT  /api/taxes/:id    (manage_billing; everything sent each time)
//
// Each tax belongs to one company (fixed when it is added): a service and an
// invoice can only use their company's taxes. A product may suggest taxes
// of any company; a new service takes those of its own company.
// Products and services choose their taxes; invoice lines copy them (with
// the rate at that time). How a line's taxes are worked out (lineTaxes):
// in Order (then id), each tax = rate % of the line amount, or for a
// compound tax of the line amount plus the taxes added before it. "add"
// taxes go on the total, "deduct" taxes come off it. Never deleted.

type taxInput struct {
	CompanyID   uint    `json:"company_id"` // new taxes (0 = the default company)
	Name        string  `json:"name"`
	ShortName   string  `json:"short_name"`
	Rate        float64 `json:"rate"`
	Kind        string  `json:"kind"`
	Compound    bool    `json:"compound"`
	Order       int     `json:"order"`
	Description string  `json:"description"`
	Active      *bool   `json:"active"`
}

var taxKinds = map[string]string{models.TaxAdd: "Added", models.TaxDeduct: "Deducted"}

func applyTaxInput(in taxInput, t *models.Tax) string {
	var ok bool
	if t.Name, ok = trimTo(in.Name, 100); !ok {
		return "Name is too long (max 100)"
	}
	if t.Name == "" {
		return "Enter the tax name (e.g. Punjab Sales Tax on Services)"
	}
	if t.ShortName, ok = trimTo(in.ShortName, 30); !ok {
		return "Short name is too long (max 30)"
	}
	if badNumber(in.Rate) || in.Rate < 0 || in.Rate > 100 {
		return "Rate must be between 0 and 100%"
	}
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = models.TaxAdd
	}
	if _, ok := taxKinds[kind]; !ok {
		return "Choose added or deducted"
	}
	if in.Order < 0 || in.Order > 999 {
		return "Order must be 0 to 999"
	}
	if t.Description, ok = trimTo(in.Description, 2000); !ok {
		return "Description is too long"
	}
	t.Rate = math.Round(in.Rate*10000) / 10000
	t.Kind, t.Compound, t.Order = kind, in.Compound, in.Order
	if in.Active != nil {
		t.Active = *in.Active
	}
	return ""
}

func taxClash(t *models.Tax) string {
	var other models.Tax
	if err := database.DB.Where("id <> ? AND company_id = ? AND LOWER(TRIM(name)) = LOWER(?)", t.ID, t.CompanyID, t.Name).First(&other).Error; err == nil {
		return "This company already has a tax called " + other.Name
	}
	return ""
}

func taxLabel(t *models.Tax) string {
	s := t.Name + " " + rateText(t.Rate)
	if t.Kind == models.TaxDeduct {
		s += " (deducted)"
	}
	if t.Compound {
		s += " (compound)"
	}
	return s
}

func rateText(r float64) string { return strconv.FormatFloat(r, 'f', -1, 64) + "%" }

func GetTaxes(c *gin.Context) {
	q := database.DB.Model(&models.Tax{})
	switch c.DefaultQuery("status", "active") {
	case "active":
		q = q.Where("active = ?", true)
	case "inactive":
		q = q.Where("active = ?", false)
	case "all":
	default:
		badRequest(c, "Unknown status")
		return
	}
	if id, ok := companyParam(c); !ok {
		return
	} else if id != 0 {
		q = q.Where("company_id = ?", id)
	}
	var list []models.Tax
	if err := q.Order("sort_order ASC, LOWER(name) ASC, id ASC").Limit(1000).Find(&list).Error; err != nil {
		serverError(c, "Could not load taxes", err)
		return
	}
	if list == nil {
		list = []models.Tax{}
	}
	// How many products and current services use each.
	products, services := map[uint]int{}, map[uint]int{}
	var ps []models.Product
	database.DB.Select("id, tax_ids").Where("tax_ids IS NOT NULL").Find(&ps)
	for _, p := range ps {
		for _, id := range p.TaxIDs {
			products[id]++
		}
	}
	var ss []models.Subscription
	database.DB.Select("id, tax_ids").Where("tax_ids IS NOT NULL").Where(currentServiceSQL, today()).Find(&ss)
	for _, s := range ss {
		for _, id := range s.TaxIDs {
			services[id]++
		}
	}
	c.JSON(http.StatusOK, gin.H{"taxes": list, "products": products, "services": services, "kinds": taxKinds,
		"can_manage": auth.Can(auth.User(c).Role, "manage_billing")})
}

func GetTax(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var t models.Tax
	if err := database.DB.First(&t, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tax not found"})
		return
	}
	var history []models.AuditLog
	database.DB.Where("entity_type = ? AND entity_id = ?", "tax", t.ID).Order("id DESC").Limit(50).Find(&history)
	c.JSON(http.StatusOK, gin.H{"tax": t, "history": history, "can_manage": auth.Can(auth.User(c).Role, "manage_billing")})
}

func CreateTax(c *gin.Context) {
	var in taxInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	t := models.Tax{Active: true}
	if msg := applyTaxInput(in, &t); msg != "" {
		badRequest(c, msg)
		return
	}
	co, msg := pickCompany(database.DB, in.CompanyID, true)
	if msg != "" {
		badRequest(c, msg)
		return
	}
	t.CompanyID = co.ID
	if msg := taxClash(&t); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	uid := auth.User(c).ID
	t.CreatedByID, t.UpdatedByID = uid, uid
	if err := database.DB.Create(&t).Error; err != nil {
		serverError(c, "Could not add the tax", err)
		return
	}
	audit(c, "created", "tax", t.ID, taxLabel(&t)+", order "+strconv.Itoa(t.Order)+", company "+co.Name)
	c.JSON(http.StatusCreated, gin.H{"tax": t})
}

func UpdateTax(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in taxInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	var t models.Tax
	if err := database.DB.First(&t, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tax not found"})
		return
	}
	old := t
	if in.CompanyID != 0 && in.CompanyID != t.CompanyID {
		badRequest(c, "A tax stays with its company. Add a new tax for the other company.")
		return
	}
	if msg := applyTaxInput(in, &t); msg != "" {
		badRequest(c, msg)
		return
	}
	if msg := taxClash(&t); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	activeLabel := map[bool]string{true: "Active", false: "Inactive"}
	var ch changeList
	ch.add("Name", old.Name, t.Name)
	ch.add("Short name", old.ShortName, t.ShortName)
	ch.add("Rate", rateText(old.Rate), rateText(t.Rate))
	ch.add("Kind", taxKinds[old.Kind], taxKinds[t.Kind])
	ch.add("Compound", boolText(old.Compound), boolText(t.Compound))
	ch.add("Order", strconv.Itoa(old.Order), strconv.Itoa(t.Order))
	ch.add("Description", old.Description, t.Description)
	ch.add("Status", activeLabel[old.Active], activeLabel[t.Active])
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"tax": t})
		return
	}
	t.UpdatedByID = auth.User(c).ID
	if err := database.DB.Save(&t).Error; err != nil {
		serverError(c, "Could not save the tax", err)
		return
	}
	audit(c, "updated", "tax", t.ID, t.Name+": "+ch.String())
	c.JSON(http.StatusOK, gin.H{"tax": t})
}

// ---- choosing taxes ---------------------------------------------------------

func allTaxes(tx *gorm.DB) (map[uint]models.Tax, error) {
	var list []models.Tax
	if err := tx.Find(&list).Error; err != nil {
		return nil, err
	}
	m := make(map[uint]models.Tax, len(list))
	for _, t := range list {
		m[t.ID] = t
	}
	return m, nil
}

// checkTaxIDs: every id must be a tax (of companyID, unless 0); a newly
// chosen one must be active (one already chosen may stay after being made
// inactive). Returns the ids without repeats, in tax order.
func checkTaxIDs(ids []uint, already []uint, taxes map[uint]models.Tax, companyID uint) ([]uint, string) {
	if len(ids) > 20 {
		return nil, "At most 20 taxes"
	}
	had := map[uint]bool{}
	for _, id := range already {
		had[id] = true
	}
	seen := map[uint]bool{}
	out := []uint{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		t, ok := taxes[id]
		if !ok {
			return nil, "Tax not found"
		}
		if companyID != 0 && t.CompanyID != companyID {
			return nil, t.Name + " is another company's tax"
		}
		if !t.Active && !had[id] {
			return nil, t.Name + " is inactive; choose another tax"
		}
		out = append(out, id)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := taxes[out[i]], taxes[out[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
	return out, ""
}

// taxNames: "PST 16%, WHT 4%" for the audit log.
func taxNames(ids []uint, taxes map[uint]models.Tax) string {
	var parts []string
	for _, id := range ids {
		if t, ok := taxes[id]; ok {
			n := t.ShortName
			if n == "" {
				n = t.Name
			}
			parts = append(parts, n+" "+rateText(t.Rate))
		}
	}
	return strings.Join(parts, ", ")
}

// snapshotTaxes copies the chosen taxes (with today's rates) for a line.
func snapshotTaxes(ids []uint, taxes map[uint]models.Tax) []models.TaxApplied {
	out := []models.TaxApplied{}
	for _, id := range ids {
		t, ok := taxes[id]
		if !ok {
			continue
		}
		out = append(out, models.TaxApplied{TaxID: t.ID, Name: t.Name, ShortName: t.ShortName, Rate: t.Rate,
			Kind: t.Kind, Compound: t.Compound, Order: t.Order})
	}
	return out
}

func taxIDsOf(applied []models.TaxApplied) []uint {
	ids := make([]uint, 0, len(applied))
	for _, a := range applied {
		if a.TaxID != 0 {
			ids = append(ids, a.TaxID)
		}
	}
	return ids
}

// ---- working out taxes ------------------------------------------------------

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// lineTaxes works out each tax on one line (unrounded amounts kept to 4
// decimals; the invoice rounds each tax's total once).
func lineTaxes(amount float64, taxes []models.TaxApplied) []models.TaxApplied {
	out := append([]models.TaxApplied(nil), taxes...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].TaxID < out[j].TaxID
	})
	added := 0.0
	for i := range out {
		base := amount
		if out[i].Compound {
			base += added
		}
		amt := base * out[i].Rate / 100
		if out[i].Kind == models.TaxAdd {
			added += amt
		}
		out[i].Base, out[i].Amount = round4(base), round4(amt)
	}
	return out
}

// recalcInvoice works out each line's taxes and the invoice totals:
// one summary row per tax (same tax and rate), each rounded once.
// Total = subtotal + added taxes − deducted taxes. An older invoice's
// single tax % (TaxPercent) still applies to its whole subtotal.
func recalcInvoice(inv *models.Invoice) {
	sub := 0.0
	type key struct {
		id   uint
		rate float64
		kind string
		name string
	}
	sums := map[key]*models.TaxApplied{}
	var order []key
	for i := range inv.Lines {
		l := &inv.Lines[i]
		sub += l.Amount
		l.Taxes = lineTaxes(l.Amount, l.Taxes)
		for _, t := range l.Taxes {
			k := key{t.TaxID, t.Rate, t.Kind, t.Name}
			s, ok := sums[k]
			if !ok {
				c := t
				c.Base, c.Amount = 0, 0
				s = &c
				sums[k] = s
				order = append(order, k)
			}
			s.Base += t.Base
			s.Amount += t.Amount
		}
	}
	inv.Subtotal = roundMoney(sub)
	lines := make([]models.TaxApplied, 0, len(order)+1)
	for _, k := range order {
		s := *sums[k]
		s.Base, s.Amount = roundMoney(s.Base), roundMoney(s.Amount)
		lines = append(lines, s)
	}
	if inv.TaxPercent > 0 {
		lines = append(lines, models.TaxApplied{Name: "Sales tax", Rate: inv.TaxPercent, Kind: models.TaxAdd, Order: 1000,
			Base: inv.Subtotal, Amount: taxOf(inv.Subtotal, inv.TaxPercent)})
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].Kind != lines[j].Kind {
			return lines[i].Kind == models.TaxAdd // added first, then deducted
		}
		return lines[i].Order < lines[j].Order
	})
	var add, ded float64
	for _, t := range lines {
		if t.Kind == models.TaxDeduct {
			ded += t.Amount
		} else {
			add += t.Amount
		}
	}
	inv.TaxLines = lines
	inv.TaxAmount, inv.DeductedAmount = roundMoney(add), roundMoney(ded)
	inv.Total = roundMoney(inv.Subtotal + inv.TaxAmount - inv.DeductedAmount)
}

// taxesOfCompany keeps the ids of taxes that belong to the company.
func taxesOfCompany(ids []uint, taxes map[uint]models.Tax, companyID uint) []uint {
	out := []uint{}
	for _, id := range ids {
		if t, ok := taxes[id]; ok && t.CompanyID == companyID {
			out = append(out, id)
		}
	}
	return out
}
