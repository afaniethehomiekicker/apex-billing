package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"apex-billing/internal/database"
	"apex-billing/internal/models"
	"apex-billing/internal/xlsx"

	"github.com/gin-gonic/gin"
)

// Reports (view_reports). Each answers JSON for the page, or an Excel file
// with ?format=xlsx — the same columns and rows.
//
//	GET /api/reports/taxes             ?from=YYYY-MM-DD&to=YYYY-MM-DD
//	GET /api/reports/ageing            ?as_of=YYYY-MM-DD
//	GET /api/reports/billed-collected  ?from=YYYY-MM &to=YYYY-MM
//	GET /api/reports/mrc               ?group=product|client
//	GET /api/reports/invoices          ?from=YYYY-MM-DD &to=YYYY-MM-DD   (invoice register, for tax returns)
//	GET /api/reports/payments          ?from=YYYY-MM-DD &to=YYYY-MM-DD &status=verified|all
//
// mrc, invoices and taxes also take &company_id= (one billing company).

type reportCol struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Kind  string  `json:"kind"` // text | money | number | date | percent
	Width float64 `json:"-"`
}

type report struct {
	Name     string      `json:"name"` // file name
	Tab      string      `json:"-"`    // Excel tab name (short)
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle"`
	Columns  []reportCol `json:"columns"`
	Rows     [][]any     `json:"rows"`
	Totals   []any       `json:"totals"`
	Links    []string    `json:"links,omitempty"` // per row: a page to open (e.g. "clients/3")
	Chart    any         `json:"chart,omitempty"`
	Company  string      `json:"company,omitempty"` // only this billing company
}

// reportCompany reads ?company_id=: 0 = all companies. Answers when invalid.
func reportCompany(c *gin.Context) (uint, string, bool) {
	id, ok := companyParam(c)
	if !ok || id == 0 {
		return 0, "", ok
	}
	co, err := companyByID(database.DB, id)
	if err != nil {
		badRequest(c, "Company not found")
		return 0, "", false
	}
	return co.ID, co.Name, true
}

func sendReport(c *gin.Context, r *report) {
	if r.Rows == nil {
		r.Rows = [][]any{}
	}
	if c.Query("format") != "xlsx" {
		c.JSON(http.StatusOK, gin.H{"report": r})
		return
	}
	cols := make([]xlsx.Column, len(r.Columns))
	for i, col := range r.Columns {
		cols[i] = xlsx.Column{Label: col.Label, Kind: col.Kind, Width: col.Width}
	}
	sub := r.Subtitle
	if r.Company != "" {
		sub = r.Company + " · " + sub
	} else if co := companySettings(); co.CompanyName != "" {
		sub = co.CompanyName + " · " + sub
	}
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, &xlsx.Sheet{Name: r.Tab, Title: r.Title, Subtitle: sub + " · printed " + time.Now().In(businessZone()).Format("02 Jan 2006 15:04"),
		Columns: cols, Rows: r.Rows, Totals: r.Totals}); err != nil {
		serverError(c, "Could not make the Excel file", err)
		return
	}
	file := strings.ReplaceAll(r.Name+".xlsx", `"`, "")
	c.Header("Content-Disposition", `attachment; filename="`+file+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

func dayParam(c *gin.Context, key string, def time.Time) (time.Time, bool) {
	d, err := parseDay(c.Query(key))
	if err != nil {
		badRequest(c, "Dates must be YYYY-MM-DD")
		return def, false
	}
	if d == nil {
		return def, true
	}
	return *d, true
}

func fmtDayLong(t time.Time) string { return t.Format("02 Jan 2006") }

// ---- ageing -----------------------------------------------------------------

// ReportAgeing: what each client owed on a date, by how late it was.
// Unpaid = invoice total − verified payments dated on or before that day.
// Opening balances (as of their date) and unapplied credit are shown apart.
func ReportAgeing(c *gin.Context) {
	asOf, ok := dayParam(c, "as_of", todayDate())
	if !ok {
		return
	}
	d := asOf.Format("2006-01-02")
	per, clients, err := computeAgeing(asOf, 0)
	if err != nil {
		serverError(c, "Could not make the report", err)
		return
	}

	r := &report{
		Name:     "Ageing " + d,
		Tab:      "Ageing",
		Title:    "Receivables ageing",
		Subtitle: "As of " + fmtDayLong(asOf) + " · days past due date · verified payments only",
		Columns: []reportCol{
			{"client_number", "Client no.", xlsx.Text, 12}, {"client", "Client", xlsx.Text, 32},
			{"opening", "Opening balance", xlsx.Money, 0}, {"current", "Not yet due", xlsx.Money, 0},
			{"d30", "1–30 days", xlsx.Money, 0}, {"d60", "31–60 days", xlsx.Money, 0}, {"d90", "61–90 days", xlsx.Money, 0},
			{"d90p", "Over 90 days", xlsx.Money, 0}, {"credit", "Unapplied credit", xlsx.Money, 0}, {"balance", "Balance", xlsx.Money, 0},
		},
	}
	var tot [8]float64
	for _, cl := range clients {
		v := per[cl.ID]
		if v == nil {
			continue
		}
		bal := 0.0
		for i := 0; i < 7; i++ {
			v[i] = roundMoney(v[i])
			bal += v[i]
		}
		v[7] = roundMoney(bal)
		if v[7] == 0 && v[6] == 0 {
			continue
		}
		row := []any{cl.ClientNumber, cl.Name}
		for i := 0; i < 8; i++ {
			row = append(row, v[i])
			tot[i] += v[i]
		}
		r.Rows = append(r.Rows, row)
		r.Links = append(r.Links, fmt.Sprintf("clients/%d", cl.ID))
	}
	r.Totals = []any{"Total", fmt.Sprintf("%d clients", len(r.Rows))}
	for i := 0; i < 8; i++ {
		r.Totals = append(r.Totals, roundMoney(tot[i]))
	}
	sendReport(c, r)
}

// ---- billed vs collected ------------------------------------------------------

func ReportBilledCollected(c *gin.Context) {
	to := monthStart(todayDate())
	from := to.AddDate(0, -11, 0)
	if m, err := parseMonth(c.Query("from")); err != nil {
		badRequest(c, "Months must be YYYY-MM")
		return
	} else if m != nil {
		from = *m
	}
	if m, err := parseMonth(c.Query("to")); err != nil {
		badRequest(c, "Months must be YYYY-MM")
		return
	} else if m != nil {
		to = *m
	}
	if to.Before(from) {
		from, to = to, from
	}
	if from.AddDate(5, 0, 0).Before(to) {
		badRequest(c, "Choose at most 5 years")
		return
	}
	end := to.AddDate(0, 1, 0).Format("2006-01-02")
	type inv struct {
		M        time.Time
		N        int64
		Subtotal float64
		Tax      float64
		Total    float64
	}
	var invs []inv
	database.DB.Raw(`SELECT date_trunc('month', invoice_date)::date AS m, COUNT(*) AS n, SUM(subtotal) AS subtotal,
			SUM(tax_amount) AS tax, SUM(total) AS total
		FROM invoices WHERE status = 'issued' AND invoice_date >= ? AND invoice_date < ? GROUP BY 1`, from.Format("2006-01-02"), end).Scan(&invs)
	type pay struct {
		M      time.Time
		N      int64
		Amount float64
		WHT    float64
	}
	var pays []pay
	database.DB.Raw(`SELECT date_trunc('month', payment_date)::date AS m, COUNT(*) AS n, SUM(amount) AS amount, SUM(tax_deducted) AS wht
		FROM payments WHERE status = 'verified' AND payment_date >= ? AND payment_date < ? GROUP BY 1`, from.Format("2006-01-02"), end).Scan(&pays)
	im := map[string]inv{}
	for _, v := range invs {
		im[v.M.Format("2006-01")] = v
	}
	pm := map[string]pay{}
	for _, v := range pays {
		pm[v.M.Format("2006-01")] = v
	}
	r := &report{
		Name:     fmt.Sprintf("Billed vs collected %s to %s", from.Format("2006-01"), to.Format("2006-01")),
		Title:    "Billed vs collected",
		Tab:      "Billed vs collected",
		Subtitle: from.Format("January 2006") + " – " + to.Format("January 2006") + " · issued invoices by invoice date · verified payments by payment date",
		Columns: []reportCol{
			{"month", "Month", xlsx.Text, 16}, {"invoices", "Invoices", xlsx.Number, 9},
			{"subtotal", "Billed before tax", xlsx.Money, 0}, {"tax", "Sales tax", xlsx.Money, 0}, {"billed", "Billed (total)", xlsx.Money, 0},
			{"payments", "Payments", xlsx.Number, 9}, {"received", "Received", xlsx.Money, 0}, {"wht", "Tax deducted (WHT)", xlsx.Money, 0},
			{"collected", "Collected (total)", xlsx.Money, 0}, {"diff", "Billed − collected", xlsx.Money, 0},
		},
	}
	type point struct {
		Month     string  `json:"month"`
		Billed    float64 `json:"billed"`
		Collected float64 `json:"collected"`
	}
	var chart []point
	var t [8]float64
	for m := from; !m.After(to); m = m.AddDate(0, 1, 0) {
		k := m.Format("2006-01")
		iv, pv := im[k], pm[k]
		coll := roundMoney(pv.Amount + pv.WHT)
		row := []any{m.Format("Jan 2006"), iv.N, roundMoney(iv.Subtotal), roundMoney(iv.Tax), roundMoney(iv.Total),
			pv.N, roundMoney(pv.Amount), roundMoney(pv.WHT), coll, roundMoney(iv.Total - coll)}
		r.Rows = append(r.Rows, row)
		chart = append(chart, point{k, roundMoney(iv.Total), coll})
		for i, v := range []float64{float64(iv.N), iv.Subtotal, iv.Tax, iv.Total, float64(pv.N), pv.Amount, pv.WHT, coll} {
			t[i] += v
		}
	}
	r.Totals = []any{"Total", int64(t[0]), roundMoney(t[1]), roundMoney(t[2]), roundMoney(t[3]), int64(t[4]), roundMoney(t[5]), roundMoney(t[6]), roundMoney(t[7]), roundMoney(t[3] - t[7])}
	r.Chart = chart
	sendReport(c, r)
}

// ---- MRC ----------------------------------------------------------------------

func ReportMRC(c *gin.Context) {
	td := today()
	group := c.DefaultQuery("group", "product")
	coID, coName, ok := reportCompany(c)
	if !ok {
		return
	}
	coSQL := ""
	if coID != 0 {
		coSQL = fmt.Sprintf(" AND subscriptions.company_id = %d", coID)
	}
	type row struct {
		ID      uint
		Name    string
		Number  string
		N       int64
		Monthly float64
		Annual  float64
	}
	var rows []row
	var q string
	switch group {
	case "product":
		q = `SELECT pr.id, pr.name, pr.code AS number, COUNT(*) AS n,
				COALESCE(SUM(CASE WHEN subscriptions.charge_type = 'monthly' THEN subscriptions.quantity * subscriptions.rate END), 0) AS monthly,
				COALESCE(SUM(CASE WHEN subscriptions.charge_type = 'annual' THEN subscriptions.quantity * subscriptions.rate END), 0) AS annual
			FROM subscriptions JOIN products pr ON pr.id = subscriptions.product_id
			WHERE subscriptions.charge_type <> 'one_time' AND ` + billingNowSQL + coSQL + ` GROUP BY pr.id, pr.name, pr.code ORDER BY monthly DESC, pr.name`
	case "client":
		q = `SELECT cl.id, cl.name, cl.client_number AS number, COUNT(*) AS n,
				COALESCE(SUM(CASE WHEN subscriptions.charge_type = 'monthly' THEN subscriptions.quantity * subscriptions.rate END), 0) AS monthly,
				COALESCE(SUM(CASE WHEN subscriptions.charge_type = 'annual' THEN subscriptions.quantity * subscriptions.rate END), 0) AS annual
			FROM subscriptions JOIN clients cl ON cl.id = subscriptions.client_id
			WHERE subscriptions.charge_type <> 'one_time' AND ` + billingNowSQL + coSQL + ` GROUP BY cl.id, cl.name, cl.client_number ORDER BY monthly DESC, cl.name`
	default:
		badRequest(c, "Group by product or client")
		return
	}
	if err := database.DB.Raw(q, td, td).Scan(&rows).Error; err != nil {
		serverError(c, "Could not make the report", err)
		return
	}
	label := map[string]string{"product": "Product", "client": "Client"}[group]
	numLabel := map[string]string{"product": "Code", "client": "Client no."}[group]
	r := &report{
		Name:     "MRC by " + group + " " + td,
		Tab:      "MRC by " + group,
		Title:    "Monthly recurring revenue by " + group,
		Subtitle: "Services being billed on " + fmtDayLong(todayDate()) + " · before tax",
		Company:  coName,
		Columns: []reportCol{{"number", numLabel, xlsx.Text, 14}, {"name", label, xlsx.Text, 32},
			{"services", "Services", xlsx.Number, 10}, {"monthly", "Monthly (MRC)", xlsx.Money, 0},
			{"share", "Share of MRC", xlsx.Percent, 12}, {"annual", "Per annum", xlsx.Money, 0}},
	}
	var tm, ta float64
	var tn int64
	for _, v := range rows {
		tm += v.Monthly
		ta += v.Annual
		tn += v.N
	}
	for _, v := range rows {
		share := 0.0
		if tm > 0 {
			share = roundMoney(v.Monthly / tm * 100)
		}
		r.Rows = append(r.Rows, []any{v.Number, v.Name, v.N, roundMoney(v.Monthly), share, roundMoney(v.Annual)})
		if group == "product" {
			r.Links = append(r.Links, fmt.Sprintf("services?product=%d", v.ID))
		} else {
			r.Links = append(r.Links, fmt.Sprintf("clients/%d", v.ID))
		}
	}
	r.Totals = []any{"Total", nil, tn, roundMoney(tm), 100.0, roundMoney(ta)}
	if tm == 0 {
		r.Totals[4] = nil
	}
	sendReport(c, r)
}

// ---- registers ----------------------------------------------------------------

func rangeParams(c *gin.Context) (time.Time, time.Time, bool) {
	ms := monthStart(todayDate())
	from, ok := dayParam(c, "from", ms)
	if !ok {
		return from, from, false
	}
	to, ok := dayParam(c, "to", monthEnd(ms))
	if !ok {
		return from, to, false
	}
	if to.Before(from) {
		from, to = to, from
	}
	if from.AddDate(5, 0, 0).Before(to) {
		badRequest(c, "Choose at most 5 years")
		return from, to, false
	}
	return from, to, true
}

// ReportInvoices: every issued (and cancelled) invoice dated in the range —
// the sales register for sales-tax returns.
func ReportInvoices(c *gin.Context) {
	from, to, ok := rangeParams(c)
	if !ok {
		return
	}
	coID, coName, ok := reportCompany(c)
	if !ok {
		return
	}
	iq := database.DB.Preload("Client")
	if coID != 0 {
		iq = iq.Where("company_id = ?", coID)
	}
	coNames := companyNames(database.DB)
	var invs []models.Invoice
	if err := iq.Where("status IN ? AND invoice_date >= ? AND invoice_date <= ?",
		[]string{models.InvoiceIssued, models.InvoiceCancelled}, from.Format("2006-01-02"), to.Format("2006-01-02")).
		Order("invoice_date, invoice_number").Limit(20000).Find(&invs).Error; err != nil {
		serverError(c, "Could not make the report", err)
		return
	}
	r := &report{
		Name:     fmt.Sprintf("Invoice register %s to %s", from.Format("2006-01-02"), to.Format("2006-01-02")),
		Title:    "Invoice register",
		Tab:      "Invoices",
		Subtitle: fmtDayLong(from) + " – " + fmtDayLong(to) + " · totals leave out cancelled invoices",
		Company:  coName,
		Columns: []reportCol{
			{"number", "Invoice no.", xlsx.Text, 13}, {"date", "Date", xlsx.Date, 0}, {"due", "Due", xlsx.Date, 0},
			{"company", "Billed by", xlsx.Text, 22},
			{"client_number", "Client no.", xlsx.Text, 12}, {"client", "Client", xlsx.Text, 30}, {"ntn", "NTN", xlsx.Text, 14},
			{"strn", "STRN", xlsx.Text, 18}, {"cnic", "CNIC", xlsx.Text, 16}, {"subtotal", "Before tax", xlsx.Money, 0},
			{"tax", "Taxes added", xlsx.Money, 0}, {"deducted", "Taxes deducted", xlsx.Money, 0}, {"total", "Total", xlsx.Money, 0}, {"paid", "Paid", xlsx.Money, 0},
			{"balance", "Balance", xlsx.Money, 0}, {"status", "Status", xlsx.Text, 14},
		},
	}
	var ts, tt, td, tg, tp, tb float64
	n := 0
	for _, v := range invs {
		status := "Unpaid"
		bal := roundMoney(v.Total - v.AmountPaid)
		switch {
		case v.Status == models.InvoiceCancelled:
			status = "Cancelled"
		case bal <= 0:
			status = "Paid"
		case v.AmountPaid > 0:
			status = "Partly paid"
		case v.DueDate.Before(todayDate()):
			status = "Overdue"
		}
		cn := ""
		if v.Client != nil {
			cn = v.Client.ClientNumber
		}
		paid, balance := any(roundMoney(v.AmountPaid)), any(bal)
		if v.Status == models.InvoiceCancelled {
			paid, balance = nil, nil
		} else {
			ts, tt, td, tg, tp, tb = ts+v.Subtotal, tt+v.TaxAmount, td+v.DeductedAmount, tg+v.Total, tp+v.AmountPaid, tb+bal
			n++
		}
		billedBy := v.Issuer.Name
		if billedBy == "" {
			billedBy = coNames[v.CompanyID]
		}
		r.Rows = append(r.Rows, []any{v.InvoiceNumber, v.InvoiceDate.Format("2006-01-02"), v.DueDate.Format("2006-01-02"), billedBy, cn, v.BillName,
			v.BillNTN, v.BillSTRN, v.BillCNIC, v.Subtotal, v.TaxAmount, v.DeductedAmount, v.Total, paid, balance, status})
		r.Links = append(r.Links, fmt.Sprintf("invoices/%d", v.ID))
	}
	r.Totals = []any{"Total", fmt.Sprintf("%d invoices", n), nil, nil, nil, nil, nil, nil, nil, roundMoney(ts), roundMoney(tt), roundMoney(td), roundMoney(tg), roundMoney(tp), roundMoney(tb), nil}
	sendReport(c, r)
}

// ReportPayments: payments dated in the range (verified only by default).
func ReportPayments(c *gin.Context) {
	from, to, ok := rangeParams(c)
	if !ok {
		return
	}
	status := c.DefaultQuery("status", models.PaymentVerified)
	q := database.DB.Preload("Client").Preload("Allocations").
		Where("payment_date >= ? AND payment_date <= ?", from.Format("2006-01-02"), to.Format("2006-01-02"))
	switch status {
	case "all":
	case models.PaymentPending, models.PaymentVerified, models.PaymentRejected, models.PaymentReversed:
		q = q.Where("status = ?", status)
	default:
		badRequest(c, "Unknown status")
		return
	}
	var pays []models.Payment
	if err := q.Order("payment_date, payment_number").Limit(20000).Find(&pays).Error; err != nil {
		serverError(c, "Could not make the report", err)
		return
	}
	label := map[string]string{"all": "all statuses", "verified": "verified only", "pending": "waiting for verification",
		"rejected": "rejected", "reversed": "reversed"}[status]
	r := &report{
		Name:     fmt.Sprintf("Payment register %s to %s", from.Format("2006-01-02"), to.Format("2006-01-02")),
		Title:    "Payment register",
		Tab:      "Payments",
		Subtitle: fmtDayLong(from) + " – " + fmtDayLong(to) + " · " + label,
		Columns: []reportCol{
			{"number", "Payment no.", xlsx.Text, 13}, {"date", "Date", xlsx.Date, 0}, {"client_number", "Client no.", xlsx.Text, 12},
			{"client", "Client", xlsx.Text, 30}, {"method", "Method", xlsx.Text, 20}, {"reference", "Reference", xlsx.Text, 18},
			{"bank", "Their bank", xlsx.Text, 14}, {"amount", "Received", xlsx.Money, 0}, {"wht", "Tax deducted (WHT)", xlsx.Money, 0},
			{"total", "Total credited", xlsx.Money, 0}, {"applied", "Applied to invoices", xlsx.Money, 0},
			{"status", "Status", xlsx.Text, 12}, {"recorded_by", "Recorded by", xlsx.Text, 18}, {"checked_by", "Verified / rejected by", xlsx.Text, 18},
		},
	}
	statusLabel := map[string]string{"pending": "Waiting", "verified": "Verified", "rejected": "Rejected", "reversed": "Reversed"}
	var ta, tw, tt, tp float64
	for _, p := range pays {
		cn, name := "", ""
		if p.Client != nil {
			cn, name = p.Client.ClientNumber, p.Client.Name
		}
		applied := allocated(&p)
		r.Rows = append(r.Rows, []any{p.PaymentNumber, p.PaymentDate.Format("2006-01-02"), cn, name, models.PaymentMethods[p.Method],
			p.Reference, p.BankName, p.Amount, p.TaxDeducted, credited(&p), applied, statusLabel[p.Status], p.RecordedBy, p.CheckedBy})
		r.Links = append(r.Links, fmt.Sprintf("payments/%d", p.ID))
		ta, tw, tt, tp = ta+p.Amount, tw+p.TaxDeducted, tt+credited(&p), tp+applied
	}
	r.Totals = []any{"Total", fmt.Sprintf("%d payments", len(pays)), nil, nil, nil, nil, nil, roundMoney(ta), roundMoney(tw), roundMoney(tt), roundMoney(tp), nil, nil, nil}
	sendReport(c, r)
}

// computeAgeing: per client, as of a day: [opening, not due, 1–30, 31–60,
// 61–90, over 90, −unapplied credit, (balance: filled by the caller)].
// clientID 0 = every client.
func computeAgeing(asOf time.Time, clientID uint) (map[uint]*[8]float64, []models.Client, error) {
	d := asOf.Format("2006-01-02")
	type invRow struct {
		ClientID uint
		DueDate  time.Time
		Open     float64
	}
	var invs []invRow
	if err := database.DB.Raw(`SELECT i.client_id, i.due_date, i.total - COALESCE((
			SELECT SUM(a.amount) FROM payment_allocations a JOIN payments p ON p.id = a.payment_id
			WHERE a.invoice_id = i.id AND p.status = 'verified' AND p.payment_date <= ?), 0) AS open
		FROM invoices i WHERE i.status = 'issued' AND i.invoice_date <= ? AND (? = 0 OR i.client_id = ?)`, d, d, clientID, clientID).Scan(&invs).Error; err != nil {
		return nil, nil, err
	}
	type creditRow struct {
		ClientID uint
		Credit   float64
	}
	var credits []creditRow
	database.DB.Raw(`SELECT p.client_id, SUM(p.amount + p.tax_deducted - COALESCE((
			SELECT SUM(a.amount) FROM payment_allocations a WHERE a.payment_id = p.id), 0)) AS credit
		FROM payments p WHERE p.status = 'verified' AND p.payment_date <= ? AND (? = 0 OR p.client_id = ?)
		GROUP BY p.client_id`, d, clientID, clientID).Scan(&credits)
	var clients []models.Client
	cq := database.DB.Order("LOWER(name), id")
	if clientID != 0 {
		cq = cq.Where("id = ?", clientID)
	}
	if err := cq.Find(&clients).Error; err != nil {
		return nil, nil, err
	}

	per := map[uint]*[8]float64{} // opening, not due, 1–30, 31–60, 61–90, 90+, credit, balance
	get := func(id uint) *[8]float64 {
		if per[id] == nil {
			per[id] = &[8]float64{}
		}
		return per[id]
	}
	for _, v := range invs {
		open := roundMoney(v.Open)
		if open == 0 {
			continue
		}
		late := int(asOf.Sub(v.DueDate).Hours() / 24)
		idx := 1
		switch {
		case late <= 0:
			idx = 1
		case late <= 30:
			idx = 2
		case late <= 60:
			idx = 3
		case late <= 90:
			idx = 4
		default:
			idx = 5
		}
		get(v.ClientID)[idx] += open
	}
	for _, cr := range credits {
		if roundMoney(cr.Credit) != 0 {
			get(cr.ClientID)[6] -= cr.Credit
		}
	}
	for _, cl := range clients {
		if cl.OpeningBalance != 0 && (cl.OpeningBalanceDate == nil || !cl.OpeningBalanceDate.After(asOf)) {
			get(cl.ID)[0] += cl.OpeningBalance
		}
	}

	return per, clients, nil
}

// ReportTaxes: each tax on the issued invoices dated in the range — taxable
// amount and tax, for tax returns. Older invoices with the single tax %
// show as "Sales tax (single %)".
func ReportTaxes(c *gin.Context) {
	from, to, ok := rangeParams(c)
	if !ok {
		return
	}
	coID, coName, ok := reportCompany(c)
	if !ok {
		return
	}
	tq := database.DB
	if coID != 0 {
		tq = tq.Where("company_id = ?", coID)
	}
	var invs []models.Invoice
	if err := tq.Where("status = ? AND invoice_date >= ? AND invoice_date <= ?", models.InvoiceIssued,
		from.Format("2006-01-02"), to.Format("2006-01-02")).Limit(50000).Find(&invs).Error; err != nil {
		serverError(c, "Could not make the report", err)
		return
	}
	type key struct {
		id   uint
		name string
		rate float64
		kind string
	}
	type row struct {
		models.TaxApplied
		Invoices int
	}
	sums := map[key]*row{}
	var order []key
	add := func(t models.TaxApplied) {
		k := key{t.TaxID, t.Name, t.Rate, t.Kind}
		r, ok := sums[k]
		if !ok {
			r = &row{TaxApplied: t}
			r.Base, r.Amount = 0, 0
			sums[k] = r
			order = append(order, k)
		}
		r.Base += t.Base
		r.Amount += t.Amount
		r.Invoices++
	}
	for _, v := range invs {
		if len(v.TaxLines) > 0 {
			for _, t := range v.TaxLines {
				add(t)
			}
		} else if v.TaxAmount != 0 || v.TaxPercent > 0 {
			add(models.TaxApplied{Name: "Sales tax (single %)", Rate: v.TaxPercent, Kind: models.TaxAdd, Order: 1000,
				Base: v.Subtotal, Amount: v.TaxAmount})
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := sums[order[i]], sums[order[j]]
		if a.Kind != b.Kind {
			return a.Kind == models.TaxAdd
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Name < b.Name
	})
	r := &report{
		Name:     fmt.Sprintf("Taxes %s to %s", from.Format("2006-01-02"), to.Format("2006-01-02")),
		Title:    "Tax summary",
		Tab:      "Taxes",
		Subtitle: fmtDayLong(from) + " – " + fmtDayLong(to) + " · issued invoices by invoice date",
		Company:  coName,
		Columns: []reportCol{
			{"tax", "Tax", xlsx.Text, 34}, {"rate", "Rate", xlsx.Percent, 9}, {"kind", "Added / deducted", xlsx.Text, 16},
			{"invoices", "Invoices", xlsx.Number, 10}, {"base", "Taxable amount", xlsx.Money, 0}, {"amount", "Tax", xlsx.Money, 0},
		},
	}
	var added, deducted float64
	for _, k := range order {
		s := sums[k]
		amt := roundMoney(s.Amount)
		if s.Kind == models.TaxDeduct {
			deducted += amt
		} else {
			added += amt
		}
		r.Rows = append(r.Rows, []any{s.Name, s.Rate, taxKinds[s.Kind], s.Invoices, roundMoney(s.Base), amt})
	}
	r.Totals = []any{"Added " + money(roundMoney(added)) + " · deducted " + money(roundMoney(deducted)), nil, nil, len(invs), nil, roundMoney(added - deducted)}
	sendReport(c, r)
}
