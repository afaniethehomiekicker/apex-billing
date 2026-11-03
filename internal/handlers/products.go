package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Service catalogue (products).
//
//	GET  /api/products       ?q= &status=active (default) | inactive | all &charge_type=
//	GET  /api/products/:id   with how many clients use it and its change history
//	POST /api/products       (manage_billing)
//	PUT  /api/products/:id   (manage_billing; everything sent each time)
//	POST /api/products/:id/apply-taxes   put the product's taxes on all its current services (manage_billing)
//
// Never deleted: a product no longer sold is made inactive. Clients who
// already have it keep it, at their own rate.

type productInput struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Unit        string  `json:"unit"`
	ChargeType  string  `json:"charge_type"`
	DefaultRate float64 `json:"default_rate"`
	Active      *bool   `json:"active"`
	TaxIDs      []uint  `json:"tax_ids"` // taxes new services of this product get
}

func applyProductInput(in productInput, p *models.Product) string {
	var ok bool
	if p.Code, ok = trimTo(in.Code, 30); !ok {
		return "Code is too long (max 30)"
	}
	if p.Name, ok = trimTo(in.Name, 200); !ok {
		return "Name is too long"
	}
	if p.Name == "" {
		return "Enter the product name"
	}
	if p.Description, ok = trimTo(in.Description, 2000); !ok {
		return "Description is too long"
	}
	if p.Unit, ok = trimTo(in.Unit, 40); !ok {
		return "Unit is too long"
	}
	ct := strings.TrimSpace(in.ChargeType)
	if _, ok := models.ChargeTypes[ct]; !ok {
		return "Choose a charge type"
	}
	p.ChargeType = ct
	if badNumber(in.DefaultRate) || in.DefaultRate < 0 || in.DefaultRate > 999999999999 {
		return "The default rate is not valid"
	}
	p.DefaultRate = roundMoney(in.DefaultRate)
	if in.Active != nil {
		p.Active = *in.Active
	}
	return ""
}

// productClash: another product with the same name or code.
func productClash(p *models.Product) string {
	var other models.Product
	if err := database.DB.Where("id <> ? AND LOWER(TRIM(name)) = LOWER(?)", p.ID, p.Name).First(&other).Error; err == nil {
		return "There is already a product called " + other.Name
	}
	if p.Code != "" {
		if err := database.DB.Where("id <> ? AND LOWER(TRIM(code)) = LOWER(?)", p.ID, p.Code).First(&other).Error; err == nil {
			return "Code " + p.Code + " is already used by " + other.Name
		}
	}
	return ""
}

func GetProducts(c *gin.Context) {
	q := database.DB.Model(&models.Product{})
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
	if ct := c.Query("charge_type"); ct != "" {
		if _, ok := models.ChargeTypes[ct]; !ok {
			badRequest(c, "Unknown charge type")
			return
		}
		q = q.Where("charge_type = ?", ct)
	}
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		like := likeTerm(s)
		q = q.Where("(LOWER(name) LIKE ? OR LOWER(code) LIKE ? OR LOWER(description) LIKE ?)", like, like, like)
	}
	var list []models.Product
	if err := q.Order("LOWER(name) ASC, id ASC").Limit(1000).Find(&list).Error; err != nil {
		serverError(c, "Could not load products", err)
		return
	}
	if list == nil {
		list = []models.Product{}
	}
	// How many current services use each product.
	type useRow struct {
		ProductID uint
		N         int64
	}
	var uses []useRow
	database.DB.Model(&models.Subscription{}).Select("product_id, COUNT(*) AS n").
		Where(currentServiceSQL, today()).Group("product_id").Scan(&uses)
	inUse := map[uint]int64{}
	for _, u := range uses {
		inUse[u.ProductID] = u.N
	}
	c.JSON(http.StatusOK, gin.H{
		"products": list, "in_use": inUse, "charge_types": models.ChargeTypes,
		"can_manage": auth.Can(auth.User(c).Role, "manage_billing"),
	})
}

func GetProduct(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var p models.Product
	if err := database.DB.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	var inUse int64
	database.DB.Model(&models.Subscription{}).Where("product_id = ?", p.ID).Where(currentServiceSQL, today()).Count(&inUse)
	var history []models.AuditLog
	database.DB.Where("entity_type = ? AND entity_id = ?", "product", p.ID).Order("id DESC").Limit(50).Find(&history)
	c.JSON(http.StatusOK, gin.H{"product": p, "in_use": inUse, "history": history,
		"can_manage": auth.Can(auth.User(c).Role, "manage_billing")})
}

func CreateProduct(c *gin.Context) {
	var in productInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	p := models.Product{Active: true}
	if msg := applyProductInput(in, &p); msg != "" {
		badRequest(c, msg)
		return
	}
	taxes, err := allTaxes(database.DB)
	if err != nil {
		serverError(c, "Could not load taxes", err)
		return
	}
	ids, msg := checkTaxIDs(in.TaxIDs, nil, taxes, 0)
	if msg != "" {
		badRequest(c, msg)
		return
	}
	p.TaxIDs = ids
	if msg := productClash(&p); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	uid := auth.User(c).ID
	p.CreatedByID, p.UpdatedByID = uid, uid
	if err := database.DB.Create(&p).Error; err != nil {
		serverError(c, "Could not add the product", err)
		return
	}
	audit(c, "created", "product", p.ID, p.Name+" ("+models.ChargeTypes[p.ChargeType]+", rate "+money(p.DefaultRate)+", taxes: "+orDash(taxNames(p.TaxIDs, taxes))+")")
	c.JSON(http.StatusCreated, gin.H{"product": p})
}

func UpdateProduct(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in productInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	var p models.Product
	if err := database.DB.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	old := p
	if msg := applyProductInput(in, &p); msg != "" {
		badRequest(c, msg)
		return
	}
	taxes, err := allTaxes(database.DB)
	if err != nil {
		serverError(c, "Could not load taxes", err)
		return
	}
	if in.TaxIDs != nil {
		ids, msg := checkTaxIDs(in.TaxIDs, old.TaxIDs, taxes, 0)
		if msg != "" {
			badRequest(c, msg)
			return
		}
		p.TaxIDs = ids
	}
	if msg := productClash(&p); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	activeLabel := map[bool]string{true: "Active", false: "Inactive"}
	var ch changeList
	ch.add("Code", old.Code, p.Code)
	ch.add("Name", old.Name, p.Name)
	ch.add("Description", old.Description, p.Description)
	ch.add("Unit", old.Unit, p.Unit)
	ch.add("Charge type", models.ChargeTypes[old.ChargeType], models.ChargeTypes[p.ChargeType])
	ch.add("Default rate", money(old.DefaultRate), money(p.DefaultRate))
	ch.add("Status", activeLabel[old.Active], activeLabel[p.Active])
	ch.add("Taxes", taxNames(old.TaxIDs, taxes), taxNames(p.TaxIDs, taxes))
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"product": p})
		return
	}
	p.UpdatedByID = auth.User(c).ID
	if err := database.DB.Save(&p).Error; err != nil {
		serverError(c, "Could not save the product", err)
		return
	}
	audit(c, "updated", "product", p.ID, p.Name+": "+ch.String())
	c.JSON(http.StatusOK, gin.H{"product": p})
}

// ApplyProductTaxes puts the product's taxes on all its current services
// (what they bill from now on; invoices already made keep theirs).
func ApplyProductTaxes(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var p models.Product
	if err := database.DB.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}
	taxes, err := allTaxes(database.DB)
	if err != nil {
		serverError(c, "Could not load taxes", err)
		return
	}
	var subs []models.Subscription
	if err := database.DB.Where("product_id = ?", p.ID).Where(currentServiceSQL, today()).Find(&subs).Error; err != nil {
		serverError(c, "Could not load the services", err)
		return
	}
	changed := 0
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		for _, s := range subs {
			// Each service gets the product's taxes of its own company.
			ids := taxesOfCompany(p.TaxIDs, taxes, s.CompanyID)
			want := taxNames(ids, taxes)
			idsJSON, _ := json.Marshal(ids) // a map update doesn't go through the column's JSON serializer
			before := taxNames(s.TaxIDs, taxes)
			if before == want {
				continue
			}
			if err := tx.Model(&models.Subscription{}).Where("id = ?", s.ID).
				Updates(map[string]interface{}{"tax_ids": string(idsJSON), "updated_by_id": auth.User(c).ID}).Error; err != nil {
				return err
			}
			auditTx(tx, c, "updated", "subscription", s.ID, s.SubscriptionNumber+": Taxes: "+orDash(before)+" → "+orDash(want)+" (from product "+p.Name+")")
			changed++
		}
		if changed > 0 {
			auditTx(tx, c, "updated", "product", p.ID, p.Name+": taxes ("+orDash(taxNames(p.TaxIDs, taxes))+") put on "+strconv.Itoa(changed)+" service(s)")
		}
		return nil
	})
	if err != nil {
		serverError(c, "Could not change the services", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"changed": changed, "services": len(subs)})
}

func money(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
