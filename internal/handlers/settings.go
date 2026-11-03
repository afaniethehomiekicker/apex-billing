package handlers

import (
	"net/http"
	"strings"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
)

// General settings: the defaults for new clients and invoices, and the
// security switches. Each company's details (printed on its invoices) are
// on the companies (companies.go).
//
//	GET /api/settings     (everyone signed in) — also the default company's
//	                      details as company_name, address, … (for pages
//	                      that print one company, e.g. statements)
//	PUT /api/settings     (manage_settings) currency, default_terms_days,
//	                      default_tax_percent, require_2fa

func loadSettingsRow() models.CompanySettings {
	var s models.CompanySettings
	if err := database.DB.First(&s, 1).Error; err != nil {
		s = models.CompanySettings{ID: 1, InvoicePrefix: "INV", Currency: "PKR", DefaultTermsDays: 15}
	}
	if s.Currency == "" {
		s.Currency = "PKR"
	}
	return s
}

// loadSettings: the settings with the default company's details filled in.
func loadSettings() models.CompanySettings {
	s := loadSettingsRow()
	if co, err := defaultCompany(database.DB); err == nil {
		s.CompanyName, s.Address, s.Phone, s.Email, s.Website = co.Name, co.Address, co.Phone, co.Email, co.Website
		s.NTN, s.STRN, s.BankDetails, s.InvoiceFooter, s.InvoicePrefix = co.NTN, co.STRN, co.BankDetails, co.InvoiceFooter, co.InvoicePrefix
	}
	return s
}

func GetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"settings": loadSettings(), "can_edit": auth.Can(auth.User(c).Role, "manage_settings")})
}

type settingsInput struct {
	Currency          string  `json:"currency"`
	DefaultTermsDays  int     `json:"default_terms_days"`
	DefaultTaxPercent float64 `json:"default_tax_percent"`
	Require2FA        bool    `json:"require_2fa"`
}

func UpdateSettings(c *gin.Context) {
	var in settingsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	cur, ok := trimTo(strings.ToUpper(in.Currency), 10)
	if !ok {
		badRequest(c, "Currency is too long")
		return
	}
	if cur == "" {
		cur = "PKR"
	}
	if in.DefaultTermsDays < 0 || in.DefaultTermsDays > 365 {
		badRequest(c, "Payment terms must be between 0 and 365 days")
		return
	}
	if badNumber(in.DefaultTaxPercent) || in.DefaultTaxPercent < 0 || in.DefaultTaxPercent > 100 {
		badRequest(c, "Tax % must be between 0 and 100")
		return
	}
	in.DefaultTaxPercent = roundMoney(in.DefaultTaxPercent)

	old := loadSettingsRow()
	yes := map[bool]string{true: "Yes", false: "No"}
	var ch changeList
	ch.add("Currency", old.Currency, cur)
	ch.add("Default payment terms", itoa(old.DefaultTermsDays), itoa(in.DefaultTermsDays))
	ch.add("Two-step sign-in required", yes[old.Require2FA], yes[in.Require2FA])

	row := models.CompanySettings{ID: 1}
	database.DB.FirstOrCreate(&row, models.CompanySettings{ID: 1})
	if err := database.DB.Model(&models.CompanySettings{}).Where("id = 1").Updates(map[string]interface{}{
		"currency": cur, "default_terms_days": in.DefaultTermsDays, "default_tax_percent": in.DefaultTaxPercent,
		"require_2fa": in.Require2FA, "updated_by_id": auth.User(c).ID,
	}).Error; err != nil {
		serverError(c, "Could not save the settings", err)
		return
	}
	if len(ch) > 0 {
		audit(c, "updated", "settings", 1, ch.String())
	}
	c.JSON(http.StatusOK, gin.H{"settings": loadSettings()})
}
