package handlers

import (
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Billing clients.
//
//	GET  /api/clients            ?q= &status=active|inactive|archived|all (default: not archived) &city= &type=company|individual &page= &page_size=
//	GET  /api/clients/:id
//	POST /api/clients            (manage_clients)
//	PUT  /api/clients/:id        (manage_clients; everything sent each time)
//
// Never deleted: "archived" hides a client from the lists (status=archived
// shows them) and keeps its invoices and payments. Restoring is setting the
// status back to active.

var clientStatuses = map[string]string{"active": "Active", "inactive": "Inactive", "archived": "Archived"}

var provinces = map[string]bool{
	"": true, "Punjab": true, "Sindh": true, "Khyber Pakhtunkhwa": true, "Balochistan": true,
	"Islamabad Capital Territory": true, "Gilgit-Baltistan": true, "Azad Jammu & Kashmir": true,
}

type clientInput struct {
	ClientType         string  `json:"client_type"` // company (default) | individual
	Name               string  `json:"name"`
	ContactPerson      string  `json:"contact_person"`
	Email              string  `json:"email"`
	Phone              string  `json:"phone"`
	Mobile             string  `json:"mobile"`
	Address            string  `json:"address"`
	City               string  `json:"city"`
	Province           string  `json:"province"`
	NTN                string  `json:"ntn"`
	STRN               string  `json:"strn"`
	CNIC               string  `json:"cnic"`
	TaxPercent         float64 `json:"tax_percent"`
	TermsDays          int     `json:"terms_days"`
	OpeningBalance     float64 `json:"opening_balance"`
	OpeningBalanceDate string  `json:"opening_balance_date"`
	Notes              string  `json:"notes"`
	Status             string  `json:"status"`
	// Save even though another client already has this name.
	ConfirmDuplicate bool `json:"confirm_duplicate"`
}

// applyClientInput validates the input and copies it onto the client.
func applyClientInput(in clientInput, cl *models.Client) string {
	fields := []struct {
		v     *string
		max   int
		label string
	}{
		{&in.Name, 200, "Name"}, {&in.ContactPerson, 150, "Contact person"}, {&in.Email, 200, "Email"},
		{&in.Phone, 60, "Phone"}, {&in.Mobile, 60, "Mobile"}, {&in.Address, 1000, "Address"},
		{&in.City, 100, "City"}, {&in.NTN, 40, "NTN"}, {&in.STRN, 40, "STRN"}, {&in.CNIC, 20, "CNIC"},
		{&in.Notes, 5000, "Notes"},
	}
	for _, f := range fields {
		var ok bool
		if *f.v, ok = trimTo(*f.v, f.max); !ok {
			return f.label + " is too long"
		}
	}
	in.ClientType = strings.TrimSpace(in.ClientType)
	if in.ClientType == "" {
		in.ClientType = models.ClientCompany
	}
	if _, ok := models.ClientTypes[in.ClientType]; !ok {
		return "Choose company or individual"
	}
	if in.Name == "" {
		if in.ClientType == models.ClientIndividual {
			return "Enter the person's full name"
		}
		return "Enter the company name"
	}
	if in.CNIC != "" {
		cnic, ok := normalizeCNIC(in.CNIC)
		if !ok {
			return "CNIC must be 13 digits, e.g. 35202-1234567-1"
		}
		in.CNIC = cnic
	}
	if in.Email != "" {
		// Several addresses may be given, separated by commas or semicolons.
		for _, e := range strings.FieldsFunc(in.Email, func(r rune) bool { return r == ',' || r == ';' }) {
			if _, err := mail.ParseAddress(strings.TrimSpace(e)); err != nil {
				return "The billing email doesn't look right: " + strings.TrimSpace(e)
			}
		}
	}
	in.Province = strings.TrimSpace(in.Province)
	if !provinces[in.Province] {
		return "Choose a province from the list"
	}
	if badNumber(in.TaxPercent) || in.TaxPercent < 0 || in.TaxPercent > 100 {
		return "Tax % must be between 0 and 100"
	}
	if in.TermsDays < 0 || in.TermsDays > 365 {
		return "Payment terms must be between 0 and 365 days"
	}
	if badNumber(in.OpeningBalance) || in.OpeningBalance < -999999999999 || in.OpeningBalance > 999999999999 {
		return "The opening balance is not valid"
	}
	obDate, err := parseDay(in.OpeningBalanceDate)
	if err != nil {
		return "The opening balance date must be a date"
	}
	if roundMoney(in.OpeningBalance) != 0 && obDate == nil {
		return "Give the date of the opening balance"
	}
	in.Status = strings.TrimSpace(in.Status)
	if in.Status == "" {
		in.Status = "active"
	}
	if _, ok := clientStatuses[in.Status]; !ok {
		return "Invalid status"
	}

	cl.ClientType = in.ClientType
	cl.Name, cl.ContactPerson, cl.Email, cl.Phone, cl.Mobile = in.Name, in.ContactPerson, in.Email, in.Phone, in.Mobile
	cl.Address, cl.City, cl.Province = in.Address, in.City, in.Province
	cl.NTN, cl.STRN, cl.CNIC = in.NTN, in.STRN, in.CNIC
	cl.TaxPercent, cl.TermsDays = roundMoney(in.TaxPercent), in.TermsDays
	cl.OpeningBalance, cl.OpeningBalanceDate = roundMoney(in.OpeningBalance), obDate
	cl.Notes, cl.Status = in.Notes, in.Status
	return ""
}

// normalizeCNIC turns 13 digits (with or without dashes / spaces) into
// 12345-1234567-1.
func normalizeCNIC(s string) (string, bool) {
	var d []rune
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			d = append(d, r)
		case r == '-' || r == ' ':
		default:
			return "", false
		}
	}
	if len(d) != 13 {
		return "", false
	}
	return string(d[:5]) + "-" + string(d[5:12]) + "-" + string(d[12:]), true
}

// duplicateClient: another client (not archived) with the same name, NTN or
// CNIC. Returns a message, or "".
func duplicateClient(cl *models.Client) string {
	var other models.Client
	if err := database.DB.Where("id <> ? AND status <> ? AND LOWER(TRIM(name)) = LOWER(?)", cl.ID, "archived", cl.Name).
		First(&other).Error; err == nil {
		return "There is already a client called " + other.Name + " (" + other.ClientNumber + ")"
	}
	if cl.NTN != "" {
		if err := database.DB.Where("id <> ? AND status <> ? AND LOWER(TRIM(ntn)) = LOWER(?)", cl.ID, "archived", cl.NTN).
			First(&other).Error; err == nil {
			return "NTN " + cl.NTN + " is already on " + other.Name + " (" + other.ClientNumber + ")"
		}
	}
	if cl.CNIC != "" {
		if err := database.DB.Where("id <> ? AND status <> ? AND cnic = ?", cl.ID, "archived", cl.CNIC).
			First(&other).Error; err == nil {
			return "CNIC " + cl.CNIC + " is already on " + other.Name + " (" + other.ClientNumber + ")"
		}
	}
	return ""
}

func GetClients(c *gin.Context) {
	q := database.DB.Model(&models.Client{})
	switch st := c.DefaultQuery("status", ""); st {
	case "":
		q = q.Where("status <> ?", "archived")
	case "all":
	default:
		if _, ok := clientStatuses[st]; !ok {
			badRequest(c, "Unknown status")
			return
		}
		q = q.Where("status = ?", st)
	}
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		like := likeTerm(s)
		q = q.Where(`(LOWER(name) LIKE ? OR LOWER(client_number) LIKE ? OR LOWER(contact_person) LIKE ?
			OR LOWER(email) LIKE ? OR LOWER(ntn) LIKE ? OR phone LIKE ? OR mobile LIKE ? OR cnic LIKE ?
			OR REPLACE(cnic, '-', '') LIKE ?)`, like, like, like, like, like, like, like, like, like)
	}
	if t := c.Query("type"); t != "" {
		if _, ok := models.ClientTypes[t]; !ok {
			badRequest(c, "Unknown client type")
			return
		}
		q = q.Where("client_type = ?", t)
	}
	if city := strings.TrimSpace(c.Query("city")); city != "" {
		q = q.Where("LOWER(city) = LOWER(?)", city)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		serverError(c, "Could not load clients", err)
		return
	}
	page, size := pageParams(c)
	var list []models.Client
	if err := q.Order("LOWER(name) ASC, id ASC").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		serverError(c, "Could not load clients", err)
		return
	}
	if list == nil {
		list = []models.Client{}
	}
	var cities []string
	database.DB.Model(&models.Client{}).Where("city <> '' AND status <> 'archived'").
		Distinct("city").Order("city").Limit(300).Pluck("city", &cities)
	c.JSON(http.StatusOK, gin.H{
		"clients": list, "total": total, "page": page, "page_size": size, "cities": cities,
		"can_manage": auth.Can(auth.User(c).Role, "manage_clients"),
	})
}

func GetClient(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var cl models.Client
	if err := database.DB.First(&cl, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
		return
	}
	// The client's own changes and those of its services.
	var history []models.AuditLog
	database.DB.Where(`(entity_type = 'client' AND entity_id = ?) OR
		(entity_type = 'subscription' AND entity_id IN (SELECT id FROM subscriptions WHERE client_id = ?)) OR
		(entity_type = 'invoice' AND entity_id IN (SELECT id FROM invoices WHERE client_id = ?)) OR
		(entity_type = 'payment' AND entity_id IN (SELECT id FROM payments WHERE client_id = ?))`, cl.ID, cl.ID, cl.ID, cl.ID).
		Order("id DESC").Limit(100).Find(&history)
	c.JSON(http.StatusOK, gin.H{"client": cl, "history": history, "balance": balanceOf(&cl),
		"can_manage": auth.Can(auth.User(c).Role, "manage_clients")})
}

func CreateClient(c *gin.Context) {
	var in clientInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	cl := models.Client{}
	if msg := applyClientInput(in, &cl); msg != "" {
		badRequest(c, msg)
		return
	}
	if !in.ConfirmDuplicate {
		if msg := duplicateClient(&cl); msg != "" {
			c.JSON(http.StatusConflict, gin.H{"error": msg, "duplicate": true})
			return
		}
	}
	uid := auth.User(c).ID
	cl.CreatedByID, cl.UpdatedByID = uid, uid
	if err := database.DB.Create(&cl).Error; err != nil {
		serverError(c, "Could not add the client", err)
		return
	}
	audit(c, "created", "client", cl.ID, cl.ClientNumber+" "+cl.Name+" ("+models.ClientTypes[cl.ClientType]+")")
	c.JSON(http.StatusCreated, gin.H{"client": cl})
}

func UpdateClient(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in clientInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	var cl models.Client
	if err := database.DB.First(&cl, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
		return
	}
	old := cl
	if msg := applyClientInput(in, &cl); msg != "" {
		badRequest(c, msg)
		return
	}
	if !in.ConfirmDuplicate && cl.Status != "archived" &&
		(!strings.EqualFold(old.Name, cl.Name) || !strings.EqualFold(old.NTN, cl.NTN) || old.CNIC != cl.CNIC || old.Status == "archived") {
		if msg := duplicateClient(&cl); msg != "" {
			c.JSON(http.StatusConflict, gin.H{"error": msg, "duplicate": true})
			return
		}
	}
	if cl.Status == "archived" && old.Status != "archived" {
		if n := clientHasCurrentServices(cl.ID); n > 0 {
			badRequest(c, "This client still has "+strconv.FormatInt(n, 10)+" service(s) running. End them first (set an end date), then archive.")
			return
		}
	}
	day := func(t interface{ Format(string) string }) string { return t.Format("2006-01-02") }
	obd := func(cl models.Client) string {
		if cl.OpeningBalanceDate == nil {
			return ""
		}
		return day(*cl.OpeningBalanceDate)
	}
	var ch changeList
	ch.add("Type", models.ClientTypes[old.ClientType], models.ClientTypes[cl.ClientType])
	ch.add("Name", old.Name, cl.Name)
	ch.add("Contact", old.ContactPerson, cl.ContactPerson)
	ch.add("Email", old.Email, cl.Email)
	ch.add("Phone", old.Phone, cl.Phone)
	ch.add("Mobile", old.Mobile, cl.Mobile)
	ch.add("Address", old.Address, cl.Address)
	ch.add("City", old.City, cl.City)
	ch.add("Province", old.Province, cl.Province)
	ch.add("NTN", old.NTN, cl.NTN)
	ch.add("STRN", old.STRN, cl.STRN)
	ch.add("CNIC", old.CNIC, cl.CNIC)
	ch.add("Tax %", money(old.TaxPercent), money(cl.TaxPercent))
	ch.add("Terms (days)", strconv.Itoa(old.TermsDays), strconv.Itoa(cl.TermsDays))
	ch.add("Opening balance", money(old.OpeningBalance), money(cl.OpeningBalance))
	ch.add("Opening balance date", obd(old), obd(cl))
	ch.add("Notes", old.Notes, cl.Notes)
	ch.add("Status", clientStatuses[old.Status], clientStatuses[cl.Status])
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"client": cl})
		return
	}
	cl.UpdatedByID = auth.User(c).ID
	if err := database.DB.Save(&cl).Error; err != nil {
		serverError(c, "Could not save the client", err)
		return
	}
	action := "updated"
	if old.Status != cl.Status && cl.Status == "archived" {
		action = "archived"
	} else if old.Status == "archived" && cl.Status != "archived" {
		action = "restored"
	}
	audit(c, action, "client", cl.ID, cl.ClientNumber+" "+ch.String())
	c.JSON(http.StatusOK, gin.H{"client": cl})
}
