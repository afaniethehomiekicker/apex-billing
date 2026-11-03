package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Client services (subscriptions): what each client takes, at what rate.
//
//	GET  /api/subscriptions       ?client_id= &product_id= &company_id= &charge_type= &q= &page= &page_size=
//	                              &state=current (default) | suspended | ended | all
//	GET  /api/subscriptions/:id   with its change history
//	POST /api/subscriptions       (manage_billing)
//	PUT  /api/subscriptions/:id   (manage_billing; everything sent each time)
//
// Ending a service is setting its end date; suspending pauses billing.
// Nothing is deleted. Once invoicing has billed part of a service, the
// fields that decide what was billed (start date, charge type) are locked.

// currentServiceSQL: services not finished yet: no end date or ending today
// or later, and not a one-time charge that was already billed. One "?" for
// today's date.
const currentServiceSQL = `(subscriptions.end_date IS NULL OR subscriptions.end_date >= ?)
	AND NOT (subscriptions.charge_type = 'one_time' AND subscriptions.billed_through IS NOT NULL)`

// billingNowSQL: services being charged right now (for the MRC totals).
const billingNowSQL = `subscriptions.status = 'active' AND subscriptions.start_date <= ?
	AND (subscriptions.end_date IS NULL OR subscriptions.end_date >= ?)`

// serviceState is what the service's dates and status mean today:
//
//	to_bill    one-time charge not billed yet      billed     one-time charge billed
//	upcoming   starts after today                  active     being billed
//	ending     active with an end date ahead       ended      end date has passed
//	suspended  paused
func serviceState(s *models.Subscription, today time.Time) string {
	if s.ChargeType == models.ChargeOneTime {
		if s.BilledThrough != nil {
			return "billed"
		}
		return "to_bill"
	}
	if s.EndDate != nil && s.EndDate.Before(today) {
		return "ended"
	}
	if s.Status == "suspended" {
		return "suspended"
	}
	if s.StartDate.After(today) {
		return "upcoming"
	}
	if s.EndDate != nil {
		return "ending"
	}
	return "active"
}

type serviceView struct {
	models.Subscription
	State  string  `json:"state"`
	Amount float64 `json:"amount"` // quantity × rate
}

func viewOf(s models.Subscription, today time.Time) serviceView {
	return serviceView{Subscription: s, State: serviceState(&s, today), Amount: roundMoney(s.Quantity * s.Rate)}
}

type subscriptionInput struct {
	ClientID    uint    `json:"client_id"`
	ProductID   uint    `json:"product_id"`
	CompanyID   uint    `json:"company_id"` // the company that bills it (0 = the default)
	Description string  `json:"description"`
	Reference   string  `json:"reference"`
	ChargeType  string  `json:"charge_type"`
	Quantity    float64 `json:"quantity"`
	Rate        float64 `json:"rate"`
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	BillFrom    string  `json:"bill_from"`
	Status      string  `json:"status"`
	Notes       string  `json:"notes"`
	ContractID  uint    `json:"contract_id"` // 0 = none
	SLAPlanID   uint    `json:"sla_plan_id"` // 0 = none
	// The service's taxes. Left out: a new service gets the product's, an
	// existing one keeps its own.
	TaxIDs []uint `json:"tax_ids"`
	// Save even though the client already has a service with this reference.
	ConfirmDuplicate bool `json:"confirm_duplicate"`
}

// applySubscriptionInput validates and copies the input (not the client,
// which is fixed when the service is added).
func applySubscriptionInput(in subscriptionInput, s *models.Subscription) string {
	var ok bool
	if in.ProductID == 0 {
		return "Choose a product"
	}
	if s.Description, ok = trimTo(in.Description, 300); !ok {
		return "Description is too long (max 300)"
	}
	if s.Reference, ok = trimTo(in.Reference, 100); !ok {
		return "Circuit / reference is too long (max 100)"
	}
	if s.Notes, ok = trimTo(in.Notes, 5000); !ok {
		return "Notes are too long"
	}
	ct := strings.TrimSpace(in.ChargeType)
	if _, ok := models.ChargeTypes[ct]; !ok {
		return "Choose a charge type"
	}
	if badNumber(in.Quantity) || in.Quantity <= 0 || in.Quantity > 1000000 {
		return "Quantity must be more than 0"
	}
	if badNumber(in.Rate) || in.Rate < 0 || in.Rate > 999999999999 {
		return "The rate is not valid"
	}
	start, err := parseDay(in.StartDate)
	if err != nil || start == nil {
		if ct == models.ChargeOneTime {
			return "Give the date of the charge"
		}
		return "Give the start date"
	}
	end, err := parseDay(in.EndDate)
	if err != nil {
		return "The end date must be a date"
	}
	if ct == models.ChargeOneTime {
		end = nil // a one-time charge has no period
	}
	if end != nil && end.Before(*start) {
		return "The end date can't be before the start date"
	}
	billFrom, err := parseDay(in.BillFrom)
	if err != nil {
		return "\"Don't bill before\" must be a date"
	}
	if billFrom != nil && !billFrom.After(*start) {
		billFrom = nil // the start date already does this
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "suspended" {
		return "Invalid status"
	}
	if ct == models.ChargeOneTime {
		status = "active"
	}
	s.ProductID = in.ProductID
	s.ChargeType = ct
	s.Quantity = roundMoney(in.Quantity)
	s.Rate = roundMoney(in.Rate)
	s.StartDate, s.EndDate = *start, end
	if ct != models.ChargeOneTime {
		s.BillFrom = billFrom
	} else {
		s.BillFrom = nil
	}
	s.Status = status
	s.ContractID, s.SLAPlanID = optID(in.ContractID), optID(in.SLAPlanID)
	return ""
}

func optID(id uint) *uint {
	if id == 0 {
		return nil
	}
	return &id
}

// checkContractAndSLA: a chosen contract must be this client's and not
// terminated; a chosen SLA must exist and (for a new choice) be offered.
// Unchanged choices are always allowed. Returns the names for the log.
func checkContractAndSLA(s *models.Subscription, old *models.Subscription) (contract, sla, msg string) {
	if s.ContractID != nil {
		var k models.Contract
		if err := database.DB.First(&k, *s.ContractID).Error; err != nil {
			return "", "", "Contract not found"
		}
		if k.ClientID != s.ClientID {
			return "", "", k.ContractNumber + " is another client's contract"
		}
		changed := old == nil || idText(old.ContractID) != idText(s.ContractID)
		if changed && k.Status == models.ContractTerminated {
			return "", "", k.ContractNumber + " was terminated; choose another contract"
		}
		contract = k.ContractNumber
	}
	if s.SLAPlanID != nil {
		changed := old == nil || idText(old.SLAPlanID) != idText(s.SLAPlanID)
		p, m := loadSLA(*s.SLAPlanID, changed)
		if m != "" {
			return "", "", m
		}
		sla = p.Name
	}
	return contract, sla, ""
}

func contractNumberOf(id *uint) string {
	if id == nil {
		return ""
	}
	var k models.Contract
	if database.DB.Select("contract_number").First(&k, *id).Error != nil {
		return "#" + idText(id)
	}
	return k.ContractNumber
}

// loadProduct checks the product exists (and, for a new choice, is still sold).
func loadProduct(id uint, mustBeActive bool) (*models.Product, string) {
	var p models.Product
	if err := database.DB.First(&p, id).Error; err != nil {
		return nil, "Product not found"
	}
	if mustBeActive && !p.Active {
		return nil, p.Name + " is no longer sold (inactive). Choose another product."
	}
	return &p, ""
}

// duplicateService: the client has another current service with the same
// circuit / reference.
func duplicateService(s *models.Subscription) string {
	if s.Reference == "" {
		return ""
	}
	var other models.Subscription
	err := database.DB.Where("subscriptions.id <> ? AND client_id = ? AND LOWER(TRIM(reference)) = LOWER(?)", s.ID, s.ClientID, s.Reference).
		Where(currentServiceSQL, today()).First(&other).Error
	if err == nil {
		return "This client already has a service with reference " + s.Reference + " (" + other.SubscriptionNumber + " " + other.Description + ")"
	}
	return ""
}

func GetSubscriptions(c *gin.Context) {
	td := today()
	base := database.DB.Model(&models.Subscription{}).Joins("JOIN clients ON clients.id = subscriptions.client_id")
	if v := c.Query("client_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			badRequest(c, "Invalid client")
			return
		}
		base = base.Where("subscriptions.client_id = ?", id)
	}
	if v := c.Query("product_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			badRequest(c, "Invalid product")
			return
		}
		base = base.Where("subscriptions.product_id = ?", id)
	}
	if id, ok := companyParam(c); !ok {
		return
	} else if id != 0 {
		base = base.Where("subscriptions.company_id = ?", id)
	}
	if v := c.Query("contract_id"); v != "" {
		if v == "none" {
			base = base.Where("subscriptions.contract_id IS NULL")
		} else {
			id, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				badRequest(c, "Invalid contract")
				return
			}
			base = base.Where("subscriptions.contract_id = ?", id)
		}
	}
	if v := c.Query("sla_plan_id"); v != "" {
		if v == "none" {
			base = base.Where("subscriptions.sla_plan_id IS NULL")
		} else {
			id, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				badRequest(c, "Invalid service level")
				return
			}
			base = base.Where("subscriptions.sla_plan_id = ?", id)
		}
	}
	if ct := c.Query("charge_type"); ct != "" {
		if _, ok := models.ChargeTypes[ct]; !ok {
			badRequest(c, "Unknown charge type")
			return
		}
		base = base.Where("subscriptions.charge_type = ?", ct)
	}
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		like := likeTerm(s)
		base = base.Where(`(LOWER(subscriptions.description) LIKE ? OR LOWER(subscriptions.reference) LIKE ?
			OR LOWER(subscriptions.subscription_number) LIKE ? OR LOWER(clients.name) LIKE ? OR LOWER(clients.client_number) LIKE ?)`,
			like, like, like, like, like)
	}

	q := base.Session(&gorm.Session{})
	switch st := c.DefaultQuery("state", "current"); st {
	case "current":
		q = q.Where(currentServiceSQL, td)
	case "suspended":
		q = q.Where(currentServiceSQL, td).Where("subscriptions.status = ?", "suspended")
	case "ended":
		q = q.Where("NOT ("+currentServiceSQL+")", td)
	case "all":
	default:
		badRequest(c, "Unknown state")
		return
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		serverError(c, "Could not load services", err)
		return
	}
	page, size := pageParams(c)
	var list []models.Subscription
	if err := q.Session(&gorm.Session{}).Preload("Client").Preload("Product").Preload("Company").Preload("Contract").Preload("SLAPlan").
		Order("LOWER(clients.name) ASC, subscriptions.start_date ASC, subscriptions.id ASC").
		Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		serverError(c, "Could not load services", err)
		return
	}
	tdT := todayDate()
	out := make([]serviceView, 0, len(list))
	for _, s := range list {
		out = append(out, viewOf(s, tdT))
	}

	// Recurring totals of what is being billed today, with the same
	// client / product / search filters.
	var totals struct {
		Monthly float64 `json:"monthly"`
		Annual  float64 `json:"annual"`
		Count   int64   `json:"count"`
	}
	base.Session(&gorm.Session{}).Where(billingNowSQL, td, td).Select(`
		COALESCE(SUM(CASE WHEN subscriptions.charge_type = 'monthly' THEN subscriptions.quantity * subscriptions.rate END), 0) AS monthly,
		COALESCE(SUM(CASE WHEN subscriptions.charge_type = 'annual' THEN subscriptions.quantity * subscriptions.rate END), 0) AS annual,
		COUNT(CASE WHEN subscriptions.charge_type <> 'one_time' THEN 1 END) AS count`).Scan(&totals)
	totals.Monthly, totals.Annual = roundMoney(totals.Monthly), roundMoney(totals.Annual)

	c.JSON(http.StatusOK, gin.H{
		"services": out, "total": total, "page": page, "page_size": size, "totals": totals,
		"today": td, "charge_types": models.ChargeTypes,
		"can_manage": auth.Can(auth.User(c).Role, "manage_billing"),
	})
}

func GetSubscription(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var s models.Subscription
	if err := database.DB.Preload("Client").Preload("Product").Preload("Company").Preload("Contract").Preload("SLAPlan").First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}
	var history []models.AuditLog
	database.DB.Where("entity_type = ? AND entity_id = ?", "subscription", s.ID).Order("id DESC").Limit(50).Find(&history)
	c.JSON(http.StatusOK, gin.H{"service": viewOf(s, todayDate()), "history": history, "today": today(),
		"can_manage": auth.Can(auth.User(c).Role, "manage_billing")})
}

func CreateSubscription(c *gin.Context) {
	var in subscriptionInput
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
	s := models.Subscription{ClientID: cl.ID}
	if msg := applySubscriptionInput(in, &s); msg != "" {
		badRequest(c, msg)
		return
	}
	co, msg := pickCompany(database.DB, in.CompanyID, true)
	if msg != "" {
		badRequest(c, msg)
		return
	}
	s.CompanyID = co.ID
	p, msg2 := loadProduct(s.ProductID, true)
	if msg2 != "" {
		badRequest(c, msg2)
		return
	}
	if s.Description == "" {
		s.Description = p.Name
	}
	contractNo, slaLabel, msg := checkContractAndSLA(&s, nil)
	if msg != "" {
		badRequest(c, msg)
		return
	}
	taxes, err := allTaxes(database.DB)
	if err != nil {
		serverError(c, "Could not load taxes", err)
		return
	}
	// Left out: the product's taxes that belong to this company.
	want := in.TaxIDs
	if want == nil {
		want = taxesOfCompany(p.TaxIDs, taxes, co.ID)
	}
	if s.TaxIDs, msg = checkTaxIDs(want, p.TaxIDs, taxes, co.ID); msg != "" {
		badRequest(c, msg)
		return
	}
	if !in.ConfirmDuplicate {
		if msg := duplicateService(&s); msg != "" {
			c.JSON(http.StatusConflict, gin.H{"error": msg, "duplicate": true})
			return
		}
	}
	uid := auth.User(c).ID
	s.CreatedByID, s.UpdatedByID = uid, uid
	if err := database.DB.Omit("Client", "Product", "Company", "Contract", "SLAPlan").Create(&s).Error; err != nil {
		serverError(c, "Could not add the service", err)
		return
	}
	details := s.SubscriptionNumber + " for " + cl.ClientNumber + " " + cl.Name + ", billed by " + co.Name + ": " + s.Description +
		" (" + models.ChargeTypes[s.ChargeType] + ", " + money(s.Quantity) + " × " + money(s.Rate) + ", from " + dayString(&s.StartDate)
	if s.EndDate != nil {
		details += " to " + dayString(s.EndDate)
	}
	details += ")"
	if contractNo != "" {
		details += "; contract " + contractNo
	}
	if slaLabel != "" {
		details += "; SLA " + slaLabel
	}
	details += "; taxes " + orDash(taxNames(s.TaxIDs, taxes))
	audit(c, "created", "subscription", s.ID, details)
	s.Client, s.Product, s.Company = &cl, p, co
	if s.ContractID != nil {
		var k models.Contract
		if database.DB.First(&k, *s.ContractID).Error == nil {
			s.Contract = &k
		}
	}
	if s.SLAPlanID != nil {
		s.SLAPlan, _ = loadSLA(*s.SLAPlanID, false)
	}
	c.JSON(http.StatusCreated, gin.H{"service": viewOf(s, todayDate())})
}

func UpdateSubscription(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in subscriptionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	var s models.Subscription
	if err := database.DB.Preload("Client").Preload("Product").Preload("Company").First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}
	old := s
	if msg := applySubscriptionInput(in, &s); msg != "" {
		badRequest(c, msg)
		return
	}
	p := old.Product
	if s.ProductID != old.ProductID {
		var msg string
		if p, msg = loadProduct(s.ProductID, true); msg != "" {
			badRequest(c, msg)
			return
		}
	}
	if s.Description == "" {
		s.Description = p.Name
	}
	if _, _, msg := checkContractAndSLA(&s, &old); msg != "" {
		badRequest(c, msg)
		return
	}
	taxes, err := allTaxes(database.DB)
	if err != nil {
		serverError(c, "Could not load taxes", err)
		return
	}
	// The billing company can change only before anything of the service
	// is on an invoice (its invoices must all come from one company).
	co := old.Company
	if in.CompanyID != 0 && in.CompanyID != old.CompanyID {
		var msg string
		if co, msg = pickCompany(database.DB, in.CompanyID, true); msg != "" {
			badRequest(c, msg)
			return
		}
		var n int64
		database.DB.Raw(`SELECT COUNT(*) FROM invoice_lines l JOIN invoices i ON i.id = l.invoice_id
			WHERE l.subscription_id = ? AND i.status IN ('draft', 'issued')`, s.ID).Scan(&n)
		if n > 0 {
			badRequest(c, "This service is already on an invoice, so the company that bills it can't change. End it and add a new service under the other company.")
			return
		}
		s.CompanyID = co.ID
	}
	s.TaxIDs = old.TaxIDs
	if s.CompanyID != old.CompanyID {
		s.TaxIDs = taxesOfCompany(old.TaxIDs, taxes, s.CompanyID)
	}
	if in.TaxIDs != nil {
		ids, msg := checkTaxIDs(in.TaxIDs, old.TaxIDs, taxes, s.CompanyID)
		if msg != "" {
			badRequest(c, msg)
			return
		}
		s.TaxIDs = ids
	}

	// Once (part of) a service is billed, what was billed must stay true.
	if old.BilledThrough != nil {
		billed := dayString(old.BilledThrough)
		switch {
		case old.ChargeType == models.ChargeOneTime &&
			(s.Rate != old.Rate || s.Quantity != old.Quantity || !s.StartDate.Equal(old.StartDate) || s.ChargeType != old.ChargeType):
			badRequest(c, "This one-time charge is already billed; its amount and date can't change")
			return
		case s.ChargeType != old.ChargeType:
			badRequest(c, "Already billed up to "+billed+": the charge type can't change. End this service and add a new one.")
			return
		case !s.StartDate.Equal(old.StartDate):
			badRequest(c, "Already billed up to "+billed+": the start date can't change")
			return
		case s.EndDate != nil && s.EndDate.Before(*old.BilledThrough):
			badRequest(c, "Already billed up to "+billed+": the end date can't be earlier than that")
			return
		}
	}
	// Resuming: the suspended time is not billed. Billing restarts today
	// (or later, if a later "don't bill before" date was given).
	if old.Status == "suspended" && s.Status == "active" {
		td := todayDate()
		if s.BillFrom == nil || s.BillFrom.Before(td) {
			if td.After(s.StartDate) {
				s.BillFrom = &td
			}
		}
	}
	if !in.ConfirmDuplicate && !strings.EqualFold(old.Reference, s.Reference) {
		if msg := duplicateService(&s); msg != "" {
			c.JSON(http.StatusConflict, gin.H{"error": msg, "duplicate": true})
			return
		}
	}

	statusLabel := map[string]string{"active": "Active", "suspended": "Suspended"}
	var ch changeList
	if s.ProductID != old.ProductID {
		ch.add("Product", old.Product.Name, p.Name)
	}
	if s.CompanyID != old.CompanyID {
		ch.add("Billed by", companyNameOf(old.Company), companyNameOf(co))
	}
	ch.add("Description", old.Description, s.Description)
	ch.add("Reference", old.Reference, s.Reference)
	ch.add("Charge type", models.ChargeTypes[old.ChargeType], models.ChargeTypes[s.ChargeType])
	ch.add("Quantity", money(old.Quantity), money(s.Quantity))
	ch.add("Rate", money(old.Rate), money(s.Rate))
	ch.add("Start", dayString(&old.StartDate), dayString(&s.StartDate))
	ch.add("End", dayString(old.EndDate), dayString(s.EndDate))
	ch.add("Don't bill before", dayString(old.BillFrom), dayString(s.BillFrom))
	ch.add("Status", statusLabel[old.Status], statusLabel[s.Status])
	ch.add("Notes", old.Notes, s.Notes)
	ch.add("Contract", contractNumberOf(old.ContractID), contractNumberOf(s.ContractID))
	ch.add("SLA", slaName(old.SLAPlanID), slaName(s.SLAPlanID))
	ch.add("Taxes", taxNames(old.TaxIDs, taxes), taxNames(s.TaxIDs, taxes))
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"service": viewOf(s, todayDate())})
		return
	}
	s.UpdatedByID = auth.User(c).ID
	s.Product, s.Client, s.Contract, s.SLAPlan, s.Company = nil, nil, nil, nil, nil
	if err := database.DB.Omit("Client", "Product", "Company", "Contract", "SLAPlan").Save(&s).Error; err != nil {
		serverError(c, "Could not save the service", err)
		return
	}
	action := "updated"
	switch {
	case old.EndDate == nil && s.EndDate != nil:
		action = "ended"
	case old.Status != s.Status && s.Status == "suspended":
		action = "suspended"
	case old.Status != s.Status && s.Status == "active":
		action = "resumed"
	}
	audit(c, action, "subscription", s.ID, s.SubscriptionNumber+" ("+old.Client.Name+"): "+ch.String())
	s.Client, s.Product, s.Company = old.Client, p, co
	c.JSON(http.StatusOK, gin.H{"service": viewOf(s, todayDate())})
}

func companyNameOf(co *models.Company) string {
	if co == nil {
		return ""
	}
	return co.Name
}

// clientHasCurrentServices: services that still need ending before the
// client can be archived.
func clientHasCurrentServices(clientID uint) int64 {
	var n int64
	database.DB.Model(&models.Subscription{}).Where("client_id = ?", clientID).Where(currentServiceSQL, today()).Count(&n)
	return n
}
