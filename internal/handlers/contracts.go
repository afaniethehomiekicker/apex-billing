package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
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

// Client contracts.
//
//	GET  /api/contracts                 ?client_id= &q= &page= &page_size=
//	                                    &state=current (default) | due | expired | terminated | draft | all
//	GET  /api/contracts/:id             with its services, totals and history
//	POST /api/contracts                 (manage_billing)
//	PUT  /api/contracts/:id             (manage_billing; everything sent each time)
//	POST /api/contracts/:id/services    {add: [service ids], remove: [service ids]} (manage_billing)
//	POST /api/contracts/:id/renew       {months} add a term to the end date (manage_billing)
//	POST /api/contracts/:id/terminate   {date, reason, end_services} (manage_billing)
//	POST /api/contracts/:id/document    multipart "file": the signed copy (manage_billing)
//	GET  /api/contracts/:id/document    the signed copy
//
// What a contract means today (its "state") comes from its dates:
//
//	draft       not signed yet               upcoming    starts after today
//	active      in force                     due         notice date reached: decide on renewal now
//	expired     end date passed, no renewal  terminated  ended early / not renewed
//
// An auto-renewing contract never expires: its term rolls on by
// RenewMonths each time; TermEnd is the end of the current term.

type contractView struct {
	models.Contract
	State    string     `json:"state"`
	TermEnd  *time.Time `json:"term_end"`  // end of the current term (after auto-renewals)
	NoticeBy *time.Time `json:"notice_by"` // last day to give notice for TermEnd
	Renewals int        `json:"renewals"`  // automatic renewals so far
	Services int64      `json:"services"`  // current services under it
	Monthly  float64    `json:"monthly"`   // their MRC, billing now
	Annual   float64    `json:"annual"`
}

// addMonths adds months to a date, keeping the end of a month at the end
// of a month (31 Jan + 1 month = 28/29 Feb, not 2/3 Mar).
func addMonths(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, time.UTC)
}

// contractTerm: the end of the term running on "today" (rolled forward for
// auto-renewal), the notice date for it and how many renewals happened.
func contractTerm(c *models.Contract, today time.Time) (end *time.Time, noticeBy *time.Time, renewals int) {
	if c.EndDate == nil {
		return nil, nil, 0
	}
	e := *c.EndDate
	if c.AutoRenew && c.RenewMonths > 0 && c.Status != models.ContractTerminated {
		// Each term is RenewMonths from the original end date (not chained,
		// so month-end dates don't drift).
		for e.Before(today) && renewals < 1200 {
			renewals++
			e = addMonths(*c.EndDate, renewals*c.RenewMonths)
		}
	}
	n := e.AddDate(0, 0, -c.NoticeDays)
	return &e, &n, renewals
}

func contractState(c *models.Contract, today time.Time) (string, *time.Time, *time.Time, int) {
	end, notice, renewals := contractTerm(c, today)
	switch {
	case c.Status == models.ContractTerminated:
		return "terminated", end, notice, renewals
	case c.Status == models.ContractDraft:
		return "draft", end, notice, renewals
	case c.StartDate.After(today):
		return "upcoming", end, notice, renewals
	case end == nil:
		return "active", end, notice, renewals
	case end.Before(today):
		return "expired", end, notice, renewals
	case !notice.After(today):
		return "due", end, notice, renewals
	}
	return "active", end, notice, renewals
}

func contractViewOf(c models.Contract, today time.Time) contractView {
	st, end, notice, renewals := contractState(&c, today)
	return contractView{Contract: c, State: st, TermEnd: end, NoticeBy: notice, Renewals: renewals}
}

type contractTotals struct {
	ContractID uint
	N          int64
	Monthly    float64
	Annual     float64
}

// fillContractTotals adds each contract's current services and MRC.
func fillContractTotals(list []contractView) {
	if len(list) == 0 {
		return
	}
	ids := make([]uint, 0, len(list))
	for _, v := range list {
		ids = append(ids, v.ID)
	}
	td := today()
	var rows []contractTotals
	database.DB.Model(&models.Subscription{}).Where("contract_id IN ?", ids).Where(currentServiceSQL, td).
		Select(`contract_id, COUNT(*) AS n,
			COALESCE(SUM(CASE WHEN `+billingNowSQL+` AND charge_type = 'monthly' THEN quantity * rate END), 0) AS monthly,
			COALESCE(SUM(CASE WHEN `+billingNowSQL+` AND charge_type = 'annual' THEN quantity * rate END), 0) AS annual`,
			td, td, td, td).
		Group("contract_id").Scan(&rows)
	by := map[uint]contractTotals{}
	for _, r := range rows {
		by[r.ContractID] = r
	}
	for i := range list {
		r := by[list[i].ID]
		list[i].Services, list[i].Monthly, list[i].Annual = r.N, roundMoney(r.Monthly), roundMoney(r.Annual)
	}
}

type contractInput struct {
	ClientID        uint   `json:"client_id"`
	Title           string `json:"title"`
	ClientReference string `json:"client_reference"`
	SignedDate      string `json:"signed_date"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	AutoRenew       bool   `json:"auto_renew"`
	RenewMonths     int    `json:"renew_months"`
	NoticeDays      int    `json:"notice_days"`
	SLAPlanID       uint   `json:"sla_plan_id"`
	Status          string `json:"status"` // draft | active
	Notes           string `json:"notes"`
}

func applyContractInput(in contractInput, k *models.Contract) string {
	var ok bool
	if k.Title, ok = trimTo(in.Title, 200); !ok {
		return "Title is too long (max 200)"
	}
	if k.Title == "" {
		return "Give the contract a title (e.g. \"MPLS connectivity agreement 2026\")"
	}
	if k.ClientReference, ok = trimTo(in.ClientReference, 100); !ok {
		return "Client's reference is too long (max 100)"
	}
	if k.Notes, ok = trimTo(in.Notes, 5000); !ok {
		return "Notes are too long"
	}
	start, err := parseDay(in.StartDate)
	if err != nil || start == nil {
		return "Give the start date"
	}
	end, err := parseDay(in.EndDate)
	if err != nil {
		return "The end date must be a date"
	}
	if end != nil && !end.After(*start) {
		return "The end date must be after the start date"
	}
	signed, err := parseDay(in.SignedDate)
	if err != nil {
		return "The signing date must be a date"
	}
	if signed != nil && signed.After(todayDate()) {
		return "The signing date can't be in the future"
	}
	if in.RenewMonths == 0 {
		in.RenewMonths = 12
	}
	if in.RenewMonths < 1 || in.RenewMonths > 120 {
		return "Renewal term must be 1 to 120 months"
	}
	if in.AutoRenew && end == nil {
		return "An auto-renewing contract needs an end date for its first term"
	}
	if in.NoticeDays < 0 || in.NoticeDays > 365 {
		return "Notice period must be 0 to 365 days"
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = models.ContractActive
	}
	if status != models.ContractDraft && status != models.ContractActive {
		return "Invalid status"
	}
	k.StartDate, k.EndDate, k.SignedDate = *start, end, signed
	k.AutoRenew, k.RenewMonths, k.NoticeDays = in.AutoRenew, in.RenewMonths, in.NoticeDays
	k.Status = status
	if in.SLAPlanID == 0 {
		k.SLAPlanID = nil
	} else {
		id := in.SLAPlanID
		k.SLAPlanID = &id
	}
	return ""
}

func idText(p *uint) string {
	if p == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*p), 10)
}

func slaName(id *uint) string {
	if id == nil {
		return ""
	}
	var p models.SLAPlan
	if database.DB.Select("name").First(&p, *id).Error != nil {
		return "#" + idText(id)
	}
	return p.Name
}

var contractStatusLabel = map[string]string{"draft": "Draft", "active": "Active", "terminated": "Terminated"}

func boolText(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func GetContracts(c *gin.Context) {
	q := database.DB.Model(&models.Contract{}).Joins("JOIN clients ON clients.id = contracts.client_id")
	if v := c.Query("client_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			badRequest(c, "Invalid client")
			return
		}
		q = q.Where("contracts.client_id = ?", id)
	}
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		like := likeTerm(s)
		q = q.Where(`(LOWER(contracts.title) LIKE ? OR LOWER(contracts.contract_number) LIKE ? OR LOWER(contracts.client_reference) LIKE ?
			OR LOWER(clients.name) LIKE ? OR LOWER(clients.client_number) LIKE ?)`, like, like, like, like, like)
	}
	want := c.DefaultQuery("state", "current")
	keep := map[string][]string{
		"current":    {"draft", "upcoming", "active", "due"},
		"due":        {"due", "expired"},
		"expired":    {"expired"},
		"terminated": {"terminated"},
		"draft":      {"draft"},
	}
	if _, ok := keep[want]; !ok && want != "all" {
		badRequest(c, "Unknown state")
		return
	}
	// States depend on today's date and auto-renewal, so they are worked out
	// here (a company has hundreds of contracts, not millions).
	var all []models.Contract
	if err := q.Preload("Client").Preload("SLAPlan").
		Order("LOWER(clients.name) ASC, contracts.start_date DESC, contracts.id DESC").Limit(5000).Find(&all).Error; err != nil {
		serverError(c, "Could not load contracts", err)
		return
	}
	td := todayDate()
	views := make([]contractView, 0, len(all))
	counts := map[string]int{}
	for _, k := range all {
		v := contractViewOf(k, td)
		counts[v.State]++
		if want != "all" && !contains(keep[want], v.State) {
			continue
		}
		views = append(views, v)
	}
	if want == "due" {
		// Most urgent first.
		sort.SliceStable(views, func(i, j int) bool { return views[i].TermEnd.Before(*views[j].TermEnd) })
	}
	page, size := pageParams(c)
	total := len(views)
	from, to := (page-1)*size, page*size
	if from > total {
		from = total
	}
	if to > total {
		to = total
	}
	views = views[from:to]
	fillContractTotals(views)
	c.JSON(http.StatusOK, gin.H{
		"contracts": views, "total": total, "page": page, "page_size": size, "counts": counts, "today": today(),
		"can_manage": auth.Can(auth.User(c).Role, "manage_billing"),
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func GetContract(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var k models.Contract
	if err := database.DB.Preload("Client").Preload("SLAPlan").First(&k, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Contract not found"})
		return
	}
	td := todayDate()
	v := contractViewOf(k, td)
	one := []contractView{v}
	fillContractTotals(one)
	var subs []models.Subscription
	database.DB.Preload("Product").Preload("SLAPlan").Where("contract_id = ?", k.ID).
		Order("start_date ASC, id ASC").Find(&subs)
	services := make([]serviceView, 0, len(subs))
	for _, s := range subs {
		services = append(services, viewOf(s, td))
	}
	var history []models.AuditLog
	database.DB.Where("entity_type = ? AND entity_id = ?", "contract", k.ID).Order("id DESC").Limit(50).Find(&history)
	c.JSON(http.StatusOK, gin.H{"contract": one[0], "services": services, "history": history, "today": today(),
		"can_manage": auth.Can(auth.User(c).Role, "manage_billing")})
}

func CreateContract(c *gin.Context) {
	var in contractInput
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
	k := models.Contract{ClientID: cl.ID}
	if msg := applyContractInput(in, &k); msg != "" {
		badRequest(c, msg)
		return
	}
	if k.SLAPlanID != nil {
		if _, msg := loadSLA(*k.SLAPlanID, true); msg != "" {
			badRequest(c, msg)
			return
		}
	}
	uid := auth.User(c).ID
	k.CreatedByID, k.UpdatedByID = uid, uid
	if err := database.DB.Omit("Client", "SLAPlan").Create(&k).Error; err != nil {
		serverError(c, "Could not add the contract", err)
		return
	}
	details := k.ContractNumber + " for " + cl.ClientNumber + " " + cl.Name + ": " + k.Title + " (from " + dayString(&k.StartDate)
	if k.EndDate != nil {
		details += " to " + dayString(k.EndDate)
	}
	if k.AutoRenew {
		details += ", auto-renews every " + strconv.Itoa(k.RenewMonths) + " months"
	}
	audit(c, "created", "contract", k.ID, details+")")
	k.Client = &cl
	c.JSON(http.StatusCreated, gin.H{"contract": contractViewOf(k, todayDate())})
}

// loadContractForChange loads a contract that may still be changed.
func loadContractForChange(c *gin.Context, tx *gorm.DB) *models.Contract {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var k models.Contract
	if err := tx.Preload("Client").First(&k, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Contract not found"})
		return nil
	}
	if k.Status == models.ContractTerminated {
		badRequest(c, "This contract was terminated; it can't be changed")
		return nil
	}
	return &k
}

func UpdateContract(c *gin.Context) {
	var in contractInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	k := loadContractForChange(c, database.DB)
	if k == nil {
		return
	}
	old := *k
	if msg := applyContractInput(in, k); msg != "" {
		badRequest(c, msg)
		return
	}
	if k.SLAPlanID != nil && idText(k.SLAPlanID) != idText(old.SLAPlanID) {
		if _, msg := loadSLA(*k.SLAPlanID, true); msg != "" {
			badRequest(c, msg)
			return
		}
	}
	if old.Status == models.ContractActive && k.Status == models.ContractDraft {
		badRequest(c, "An active contract can't go back to draft")
		return
	}
	// Services under it must not start before the contract.
	if k.StartDate.After(old.StartDate) {
		var early models.Subscription
		if database.DB.Where("contract_id = ? AND start_date < ?", k.ID, dayString(&k.StartDate)).First(&early).Error == nil {
			badRequest(c, early.SubscriptionNumber+" "+early.Description+" starts on "+dayString(&early.StartDate)+", before this start date")
			return
		}
	}
	var ch changeList
	ch.add("Title", old.Title, k.Title)
	ch.add("Client's reference", old.ClientReference, k.ClientReference)
	ch.add("Signed", dayString(old.SignedDate), dayString(k.SignedDate))
	ch.add("Start", dayString(&old.StartDate), dayString(&k.StartDate))
	ch.add("End", dayString(old.EndDate), dayString(k.EndDate))
	ch.add("Auto-renew", boolText(old.AutoRenew), boolText(k.AutoRenew))
	ch.add("Renewal term (months)", strconv.Itoa(old.RenewMonths), strconv.Itoa(k.RenewMonths))
	ch.add("Notice (days)", strconv.Itoa(old.NoticeDays), strconv.Itoa(k.NoticeDays))
	ch.add("Default SLA", slaName(old.SLAPlanID), slaName(k.SLAPlanID))
	ch.add("Status", contractStatusLabel[old.Status], contractStatusLabel[k.Status])
	ch.add("Notes", old.Notes, k.Notes)
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"contract": contractViewOf(*k, todayDate())})
		return
	}
	k.UpdatedByID = auth.User(c).ID
	cl := k.Client
	k.Client, k.SLAPlan = nil, nil
	if err := database.DB.Omit("Client", "SLAPlan").Save(k).Error; err != nil {
		serverError(c, "Could not save the contract", err)
		return
	}
	action := "updated"
	if old.Status == models.ContractDraft && k.Status == models.ContractActive {
		action = "activated"
	}
	audit(c, action, "contract", k.ID, k.ContractNumber+" ("+cl.Name+"): "+ch.String())
	k.Client = cl
	c.JSON(http.StatusOK, gin.H{"contract": contractViewOf(*k, todayDate())})
}

// LinkContractServices puts services under this contract or takes them off.
// Services must be the same client's. With apply_sla, a linked service
// with no SLA gets the contract's default one.
func LinkContractServices(c *gin.Context) {
	var in struct {
		Add      []uint `json:"add"`
		Remove   []uint `json:"remove"`
		ApplySLA bool   `json:"apply_sla"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.Add)+len(in.Remove) == 0 || len(in.Add)+len(in.Remove) > 500 {
		badRequest(c, "Choose the services")
		return
	}
	var linked, unlinked []string
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		k := loadContractForChange(c, tx)
		if k == nil {
			return errAnswered
		}
		uid := auth.User(c).ID
		if len(in.Add) > 0 {
			var subs []models.Subscription
			if err := tx.Where("id IN ?", in.Add).Find(&subs).Error; err != nil {
				return err
			}
			if len(subs) != len(uniq(in.Add)) {
				badRequest(c, "Some of those services were not found")
				return errAnswered
			}
			for _, s := range subs {
				if s.ClientID != k.ClientID {
					badRequest(c, s.SubscriptionNumber+" belongs to another client")
					return errAnswered
				}
				if s.ContractID != nil && *s.ContractID == k.ID {
					continue
				}
				upd := map[string]interface{}{"contract_id": k.ID, "updated_by_id": uid}
				note := ""
				if in.ApplySLA && s.SLAPlanID == nil && k.SLAPlanID != nil {
					upd["sla_plan_id"] = *k.SLAPlanID
					note = ", SLA " + slaName(k.SLAPlanID)
				}
				if err := tx.Model(&models.Subscription{}).Where("id = ?", s.ID).Updates(upd).Error; err != nil {
					return err
				}
				from := ""
				if s.ContractID != nil {
					var prev models.Contract
					if tx.Select("contract_number").First(&prev, *s.ContractID).Error == nil {
						from = " (moved from " + prev.ContractNumber + ")"
					}
				}
				auditTx(tx, c, "updated", "subscription", s.ID, s.SubscriptionNumber+": contract → "+k.ContractNumber+from+note)
				linked = append(linked, s.SubscriptionNumber)
			}
		}
		if len(in.Remove) > 0 {
			var subs []models.Subscription
			if err := tx.Where("id IN ? AND contract_id = ?", in.Remove, k.ID).Find(&subs).Error; err != nil {
				return err
			}
			for _, s := range subs {
				if err := tx.Model(&models.Subscription{}).Where("id = ?", s.ID).
					Updates(map[string]interface{}{"contract_id": nil, "updated_by_id": uid}).Error; err != nil {
					return err
				}
				auditTx(tx, c, "updated", "subscription", s.ID, s.SubscriptionNumber+": contract "+k.ContractNumber+" → —")
				unlinked = append(unlinked, s.SubscriptionNumber)
			}
		}
		var parts []string
		if len(linked) > 0 {
			parts = append(parts, "added "+strings.Join(linked, ", "))
		}
		if len(unlinked) > 0 {
			parts = append(parts, "removed "+strings.Join(unlinked, ", "))
		}
		if len(parts) > 0 {
			auditTx(tx, c, "services", "contract", k.ID, k.ContractNumber+" services: "+strings.Join(parts, "; "))
		}
		return nil
	})
	if err == errAnswered {
		return
	}
	if err != nil {
		serverError(c, "Could not change the contract's services", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"added": len(linked), "removed": len(unlinked)})
}

func uniq(ids []uint) []uint {
	seen := map[uint]bool{}
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// RenewContract adds a term (default RenewMonths) to the end of the current
// term. For contracts that don't renew by themselves, or to make an
// auto-renewal permanent on record.
func RenewContract(c *gin.Context) {
	var in struct {
		Months int `json:"months"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	k := loadContractForChange(c, database.DB)
	if k == nil {
		return
	}
	if k.EndDate == nil {
		badRequest(c, "This contract has no end date, so there is nothing to renew")
		return
	}
	if in.Months == 0 {
		in.Months = k.RenewMonths
	}
	if in.Months < 1 || in.Months > 120 {
		badRequest(c, "Renew for 1 to 120 months")
		return
	}
	termEnd, _, _ := contractTerm(k, todayDate())
	newEnd := addMonths(*termEnd, in.Months)
	old := dayString(k.EndDate)
	if err := database.DB.Model(&models.Contract{}).Where("id = ?", k.ID).
		Updates(map[string]interface{}{"end_date": newEnd, "updated_by_id": auth.User(c).ID}).Error; err != nil {
		serverError(c, "Could not renew the contract", err)
		return
	}
	audit(c, "renewed", "contract", k.ID, k.ContractNumber+" ("+k.Client.Name+"): renewed for "+strconv.Itoa(in.Months)+
		" months; end "+old+" → "+dayString(&newEnd))
	k.EndDate = &newEnd
	c.JSON(http.StatusOK, gin.H{"contract": contractViewOf(*k, todayDate())})
}

// TerminateContract ends a contract on a date. With end_services, its
// current services end on that date too (each must not be billed past it).
func TerminateContract(c *gin.Context) {
	var in struct {
		Date        string `json:"date"`
		Reason      string `json:"reason"`
		EndServices bool   `json:"end_services"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	reason, ok := trimTo(in.Reason, 2000)
	if !ok || reason == "" {
		badRequest(c, "Give the reason (up to 2000 characters)")
		return
	}
	date, err := parseDay(in.Date)
	if err != nil || date == nil {
		badRequest(c, "Give the last day of the contract")
		return
	}
	var ended []string
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		k := loadContractForChange(c, tx)
		if k == nil {
			return errAnswered
		}
		if date.Before(k.StartDate) && k.Status != models.ContractDraft {
			badRequest(c, "The last day can't be before the contract starts ("+dayString(&k.StartDate)+")")
			return errAnswered
		}
		uid := auth.User(c).ID
		if in.EndServices {
			var subs []models.Subscription
			if err := tx.Where("contract_id = ?", k.ID).Where(currentServiceSQL, today()).
				Where("charge_type <> ?", models.ChargeOneTime).Find(&subs).Error; err != nil {
				return err
			}
			for _, s := range subs {
				if s.EndDate != nil && !s.EndDate.After(*date) {
					continue // already ends by then
				}
				if s.StartDate.After(*date) {
					badRequest(c, s.SubscriptionNumber+" "+s.Description+" starts on "+dayString(&s.StartDate)+
						", after that day. Take it off the contract or change its start first.")
					return errAnswered
				}
				if s.BilledThrough != nil && s.BilledThrough.After(*date) {
					badRequest(c, s.SubscriptionNumber+" "+s.Description+" is already billed up to "+dayString(s.BilledThrough)+
						". Choose that day or later, or cancel the invoice first.")
					return errAnswered
				}
				if err := tx.Model(&models.Subscription{}).Where("id = ?", s.ID).
					Updates(map[string]interface{}{"end_date": *date, "updated_by_id": uid}).Error; err != nil {
					return err
				}
				auditTx(tx, c, "ended", "subscription", s.ID, s.SubscriptionNumber+" ("+k.Client.Name+"): End: "+
					orDash(dayString(s.EndDate))+" → "+dayString(date)+" (contract "+k.ContractNumber+" terminated)")
				ended = append(ended, s.SubscriptionNumber)
			}
		}
		if err := tx.Model(&models.Contract{}).Where("id = ?", k.ID).Updates(map[string]interface{}{
			"status": models.ContractTerminated, "terminated_on": *date, "terminate_reason": reason,
			"terminated_by_id": uid, "updated_by_id": uid,
		}).Error; err != nil {
			return err
		}
		details := k.ContractNumber + " (" + k.Client.Name + "): terminated, last day " + dayString(date) + ". Reason: " + reason
		if len(ended) > 0 {
			details += ". Services ended: " + strings.Join(ended, ", ")
		}
		auditTx(tx, c, "terminated", "contract", k.ID, details)
		return nil
	})
	if err == errAnswered {
		return
	}
	if err != nil {
		serverError(c, "Could not terminate the contract", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ended_services": ended})
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ---- the signed copy --------------------------------------------------------

const maxContractDocBytes = 20 << 20

// saveUpload stores a picture or PDF under UPLOAD_DIR/<folder>/YYYY/MM/ with
// a random name; the type is read from the content, not the file name.
func saveUpload(fh *multipart.FileHeader, folder string, max int64, what string) (rel, ctype string, size int64, msg string, err error) {
	return saveUploadOf(fh, folder, max, what, slipTypes, "The "+what+" must be a PDF or a photo / scan (JPG, PNG, WEBP)")
}

// saveUploadOf: saveUpload for the given types (content type → extension).
func saveUploadOf(fh *multipart.FileHeader, folder string, max int64, what string, types map[string]string, typeMsg string) (rel, ctype string, size int64, msg string, err error) {
	tooBig := fmt.Sprintf("The %s is too big (max %d MB)", what, max>>20)
	if fh.Size > max {
		return "", "", 0, tooBig, nil
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
	ext, ok := types[ctype]
	if !ok {
		return "", "", 0, typeMsg, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", "", 0, "", err
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", "", 0, "", err
	}
	now := time.Now()
	rel = filepath.ToSlash(filepath.Join(folder, now.Format("2006"), now.Format("01"), hex.EncodeToString(buf)+ext))
	full := filepath.Join(uploadDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", "", 0, "", err
	}
	out, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", "", 0, "", err
	}
	size, err = io.Copy(out, io.LimitReader(f, max+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil || size > max {
		os.Remove(full)
		if err == nil {
			return "", "", 0, tooBig, nil
		}
		return "", "", 0, "", err
	}
	return rel, ctype, size, "", nil
}

// UploadContractDocument attaches (or replaces) the signed copy. A replaced
// file stays in the uploads folder; the audit log says which was which.
func UploadContractDocument(c *gin.Context) {
	k := loadContractForChange(c, database.DB)
	if k == nil {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		badRequest(c, "Choose the file (PDF or scan, up to 20 MB)")
		return
	}
	rel, ctype, size, msg, err := saveUpload(fh, "contracts", maxContractDocBytes, "file")
	if msg != "" {
		badRequest(c, msg)
		return
	}
	if err != nil {
		serverError(c, "Could not save the file", err)
		return
	}
	name, _ := trimTo(filepath.Base(fh.Filename), 200)
	oldName := k.DocName
	if err := database.DB.Model(&models.Contract{}).Where("id = ?", k.ID).Updates(map[string]interface{}{
		"doc_path": rel, "doc_name": name, "doc_type": ctype, "doc_size": size, "updated_by_id": auth.User(c).ID,
	}).Error; err != nil {
		os.Remove(filepath.Join(uploadDir(), filepath.FromSlash(rel)))
		serverError(c, "Could not save the file", err)
		return
	}
	action := "attached"
	details := k.ContractNumber + " signed copy: " + name + " (" + rel + ")"
	if oldName != "" {
		action = "replaced"
		details += ", was " + oldName + " (" + k.DocPath + ")"
	}
	audit(c, action, "contract", k.ID, details)
	c.JSON(http.StatusOK, gin.H{"doc_name": name, "doc_type": ctype, "doc_size": size})
}

func GetContractDocument(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var k models.Contract
	if err := database.DB.First(&k, id).Error; err != nil || k.DocPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "No signed copy for this contract"})
		return
	}
	full := filepath.Join(uploadDir(), filepath.FromSlash(k.DocPath))
	if _, err := os.Stat(full); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "The file is missing from the server's uploads folder"})
		return
	}
	c.Header("Content-Type", k.DocType)
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, strings.ReplaceAll(k.ContractNumber+filepath.Ext(k.DocPath), `"`, "")))
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; plugin-types application/pdf; sandbox")
	c.File(full)
}

// contractsDue counts contracts needing a decision: notice date reached
// (renew or not) or expired without renewal.
func contractsDue() (due, expired int) {
	var list []models.Contract
	database.DB.Where("status = ? AND end_date IS NOT NULL", models.ContractActive).Limit(5000).Find(&list)
	td := todayDate()
	for i := range list {
		switch st, _, _, _ := contractState(&list[i], td); st {
		case "due":
			due++
		case "expired":
			expired++
		}
	}
	return
}
