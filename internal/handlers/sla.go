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
)

// Service levels (SLA plans).
//
//	GET  /api/sla-plans       ?status=active (default) | inactive | all
//	GET  /api/sla-plans/:id   with how many services / contracts use it and its history
//	POST /api/sla-plans       (manage_billing)
//	PUT  /api/sla-plans/:id   (manage_billing; everything sent each time)
//
// Never deleted: a level no longer offered is made inactive; services that
// have it keep it.

type slaInput struct {
	Name             string                 `json:"name"`
	Code             string                 `json:"code"`
	UptimeTarget     float64                `json:"uptime_target"`
	ResponseMinutes  int                    `json:"response_minutes"`
	RestoreMinutes   int                    `json:"restore_minutes"`
	SupportHours     string                 `json:"support_hours"`
	SupportNote      string                 `json:"support_note"`
	CreditTiers      []models.SLACreditTier `json:"credit_tiers"`
	MaxCreditPercent float64                `json:"max_credit_percent"`
	Description      string                 `json:"description"`
	Active           *bool                  `json:"active"`
}

func applySLAInput(in slaInput, p *models.SLAPlan) string {
	var ok bool
	if p.Name, ok = trimTo(in.Name, 100); !ok {
		return "Name is too long (max 100)"
	}
	if p.Name == "" {
		return "Enter the name of the service level"
	}
	if p.Code, ok = trimTo(in.Code, 30); !ok {
		return "Code is too long (max 30)"
	}
	if badNumber(in.UptimeTarget) || in.UptimeTarget < 0 || in.UptimeTarget > 100 {
		return "Uptime target must be between 0 and 100%"
	}
	if in.ResponseMinutes < 0 || in.ResponseMinutes > 60*24*30 {
		return "Response time is not valid"
	}
	if in.RestoreMinutes < 0 || in.RestoreMinutes > 60*24*30 {
		return "Restore time is not valid"
	}
	hours := strings.TrimSpace(in.SupportHours)
	if hours == "" {
		hours = "24x7"
	}
	if _, ok := models.SupportHours[hours]; !ok {
		return "Choose the support hours"
	}
	if p.SupportNote, ok = trimTo(in.SupportNote, 200); !ok {
		return "Support hours note is too long (max 200)"
	}
	if hours == "custom" && p.SupportNote == "" {
		return "Describe the support hours (e.g. Mon–Sat 9:00–18:00)"
	}
	if len(in.CreditTiers) > 10 {
		return "At most 10 credit steps"
	}
	tiers := make([]models.SLACreditTier, 0, len(in.CreditTiers))
	seen := map[float64]bool{}
	for _, t := range in.CreditTiers {
		if badNumber(t.BelowPercent) || badNumber(t.CreditPercent) || t.BelowPercent <= 0 || t.BelowPercent > 100 {
			return "Each credit step needs an uptime % between 0 and 100"
		}
		if t.CreditPercent <= 0 || t.CreditPercent > 100 {
			return "Each credit step needs a credit % between 0 and 100"
		}
		b := roundTo(t.BelowPercent, 3)
		if seen[b] {
			return "Two credit steps have the same uptime %"
		}
		seen[b] = true
		tiers = append(tiers, models.SLACreditTier{BelowPercent: b, CreditPercent: roundMoney(t.CreditPercent)})
	}
	// Highest uptime first; a lower uptime must never earn a smaller credit.
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].BelowPercent > tiers[j].BelowPercent })
	for i := 1; i < len(tiers); i++ {
		if tiers[i].CreditPercent < tiers[i-1].CreditPercent {
			return "Lower uptime must give the same or a bigger credit"
		}
	}
	if len(tiers) > 0 && in.UptimeTarget > 0 && tiers[0].BelowPercent > in.UptimeTarget {
		return "Credits start below the uptime target: a credit step can't be above " + strconv.FormatFloat(in.UptimeTarget, 'f', -1, 64) + "%"
	}
	if badNumber(in.MaxCreditPercent) || in.MaxCreditPercent < 0 || in.MaxCreditPercent > 100 {
		return "The credit cap must be between 0 and 100%"
	}
	if p.Description, ok = trimTo(in.Description, 5000); !ok {
		return "Terms are too long"
	}
	p.UptimeTarget = roundTo(in.UptimeTarget, 3)
	p.ResponseMinutes, p.RestoreMinutes = in.ResponseMinutes, in.RestoreMinutes
	p.SupportHours = hours
	p.CreditTiers = tiers
	p.MaxCreditPercent = roundMoney(in.MaxCreditPercent)
	if in.Active != nil {
		p.Active = *in.Active
	}
	return ""
}

func slaClash(p *models.SLAPlan) string {
	var other models.SLAPlan
	if err := database.DB.Where("id <> ? AND LOWER(TRIM(name)) = LOWER(?)", p.ID, p.Name).First(&other).Error; err == nil {
		return "There is already a service level called " + other.Name
	}
	if p.Code != "" {
		if err := database.DB.Where("id <> ? AND LOWER(TRIM(code)) = LOWER(?)", p.ID, p.Code).First(&other).Error; err == nil {
			return "Code " + p.Code + " is already used by " + other.Name
		}
	}
	return ""
}

// slaSummary: one line for the audit log.
func slaSummary(p *models.SLAPlan) string {
	return p.Name + " (uptime " + pct(p.UptimeTarget) + ", response " + minutesText(p.ResponseMinutes) +
		", restore " + minutesText(p.RestoreMinutes) + ", " + models.SupportHours[p.SupportHours] + ")"
}

func tiersText(t []models.SLACreditTier) string {
	if len(t) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(t))
	for _, x := range t {
		parts = append(parts, "<"+pct(x.BelowPercent)+" → "+pct(x.CreditPercent))
	}
	return strings.Join(parts, ", ")
}

func pct(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }

func minutesText(m int) string {
	switch {
	case m == 0:
		return "—"
	case m%(60*24) == 0:
		return strconv.Itoa(m/(60*24)) + "d"
	case m%60 == 0:
		return strconv.Itoa(m/60) + "h"
	case m > 60:
		return strconv.Itoa(m/60) + "h " + strconv.Itoa(m%60) + "m"
	}
	return strconv.Itoa(m) + "m"
}

func GetSLAPlans(c *gin.Context) {
	q := database.DB.Model(&models.SLAPlan{})
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
	var list []models.SLAPlan
	if err := q.Order("uptime_target DESC, LOWER(name) ASC").Limit(500).Find(&list).Error; err != nil {
		serverError(c, "Could not load service levels", err)
		return
	}
	if list == nil {
		list = []models.SLAPlan{}
	}
	type useRow struct {
		SLAPlanID uint
		N         int64
	}
	var uses []useRow
	database.DB.Model(&models.Subscription{}).Select("sla_plan_id, COUNT(*) AS n").
		Where("sla_plan_id IS NOT NULL").Where(currentServiceSQL, today()).Group("sla_plan_id").Scan(&uses)
	inUse := map[uint]int64{}
	for _, u := range uses {
		inUse[u.SLAPlanID] = u.N
	}
	c.JSON(http.StatusOK, gin.H{
		"plans": list, "in_use": inUse, "support_hours": models.SupportHours,
		"can_manage": auth.Can(auth.User(c).Role, "manage_billing"),
	})
}

func GetSLAPlan(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var p models.SLAPlan
	if err := database.DB.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service level not found"})
		return
	}
	var services, contracts int64
	database.DB.Model(&models.Subscription{}).Where("sla_plan_id = ?", p.ID).Where(currentServiceSQL, today()).Count(&services)
	database.DB.Model(&models.Contract{}).Where("sla_plan_id = ? AND status <> ?", p.ID, models.ContractTerminated).Count(&contracts)
	var history []models.AuditLog
	database.DB.Where("entity_type = ? AND entity_id = ?", "sla_plan", p.ID).Order("id DESC").Limit(50).Find(&history)
	c.JSON(http.StatusOK, gin.H{"plan": p, "services": services, "contracts": contracts, "history": history,
		"support_hours": models.SupportHours, "can_manage": auth.Can(auth.User(c).Role, "manage_billing")})
}

func CreateSLAPlan(c *gin.Context) {
	var in slaInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	p := models.SLAPlan{Active: true}
	if msg := applySLAInput(in, &p); msg != "" {
		badRequest(c, msg)
		return
	}
	if msg := slaClash(&p); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	uid := auth.User(c).ID
	p.CreatedByID, p.UpdatedByID = uid, uid
	if err := database.DB.Create(&p).Error; err != nil {
		serverError(c, "Could not add the service level", err)
		return
	}
	audit(c, "created", "sla_plan", p.ID, slaSummary(&p)+"; credits "+tiersText(p.CreditTiers))
	c.JSON(http.StatusCreated, gin.H{"plan": p})
}

func UpdateSLAPlan(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in slaInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	var p models.SLAPlan
	if err := database.DB.First(&p, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service level not found"})
		return
	}
	old := p
	old.CreditTiers = append([]models.SLACreditTier(nil), p.CreditTiers...)
	if msg := applySLAInput(in, &p); msg != "" {
		badRequest(c, msg)
		return
	}
	if msg := slaClash(&p); msg != "" {
		c.JSON(http.StatusConflict, gin.H{"error": msg})
		return
	}
	activeLabel := map[bool]string{true: "Active", false: "Inactive"}
	var ch changeList
	ch.add("Name", old.Name, p.Name)
	ch.add("Code", old.Code, p.Code)
	ch.add("Uptime target", pct(old.UptimeTarget), pct(p.UptimeTarget))
	ch.add("Response", minutesText(old.ResponseMinutes), minutesText(p.ResponseMinutes))
	ch.add("Restore", minutesText(old.RestoreMinutes), minutesText(p.RestoreMinutes))
	ch.add("Support hours", models.SupportHours[old.SupportHours], models.SupportHours[p.SupportHours])
	ch.add("Support note", old.SupportNote, p.SupportNote)
	ch.add("Credits", tiersText(old.CreditTiers), tiersText(p.CreditTiers))
	ch.add("Credit cap", pct(old.MaxCreditPercent), pct(p.MaxCreditPercent))
	ch.add("Terms", old.Description, p.Description)
	ch.add("Status", activeLabel[old.Active], activeLabel[p.Active])
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"plan": p})
		return
	}
	p.UpdatedByID = auth.User(c).ID
	if err := database.DB.Save(&p).Error; err != nil {
		serverError(c, "Could not save the service level", err)
		return
	}
	audit(c, "updated", "sla_plan", p.ID, p.Name+": "+ch.String())
	c.JSON(http.StatusOK, gin.H{"plan": p})
}

// loadSLA checks a chosen service level exists (and, for a new choice, is
// still offered). id 0 = none.
func loadSLA(id uint, mustBeActive bool) (*models.SLAPlan, string) {
	if id == 0 {
		return nil, ""
	}
	var p models.SLAPlan
	if err := database.DB.First(&p, id).Error; err != nil {
		return nil, "Service level not found"
	}
	if mustBeActive && !p.Active {
		return nil, p.Name + " is no longer offered (inactive). Choose another service level."
	}
	return &p, ""
}

func roundTo(v float64, places int) float64 {
	m := math.Pow(10, float64(places))
	return math.Round(v*m) / m
}
