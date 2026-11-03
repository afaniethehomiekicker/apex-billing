package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"
	"apex-billing/internal/xlsx"

	"github.com/gin-gonic/gin"
)

// Client statement.
//
//	GET /api/clients/:id/statement   ?from=YYYY-MM-DD &to=YYYY-MM-DD [&format=xlsx]
//
// What counts is the same as the client balance: the opening balance,
// issued invoices (cancelled ones never count) and verified payments
// (amount received and tax deducted, as separate lines). Everything dated
// before "from" is brought forward as one figure; the period's entries
// follow with a running balance. The closing balance on the last day
// matches the client's balance.

type stmtLine struct {
	Date    *time.Time `json:"date"` // nil: opening balance with no date
	Kind    string     `json:"kind"` // opening | invoice | payment | wht
	Ref     string     `json:"ref"`
	Details string     `json:"details"`
	Debit   float64    `json:"debit"`  // invoiced
	Credit  float64    `json:"credit"` // paid
	Balance float64    `json:"balance"`
	Link    string     `json:"link,omitempty"`
	order   int
}

type statement struct {
	Client      models.Client          `json:"client"`
	Company     models.CompanySettings `json:"company"`
	From        string                 `json:"from"`
	To          string                 `json:"to"`
	BroughtFwd  float64                `json:"brought_forward"`
	Lines       []stmtLine             `json:"lines"`
	TotalDebit  float64                `json:"total_debit"`
	TotalCredit float64                `json:"total_credit"`
	Closing     float64                `json:"closing"`
	// Ageing of the closing balance (as of "to"): opening, not due, 1–30,
	// 31–60, 61–90, over 90, unapplied credit (negative).
	Ageing  [7]float64 `json:"ageing"`
	Pending float64    `json:"pending"` // payments recorded, waiting verification
}

func buildStatement(cl *models.Client, from, to time.Time) (*statement, error) {
	st := &statement{Client: *cl, Company: companySettings(), From: from.Format("2006-01-02"), To: to.Format("2006-01-02")}
	var all []stmtLine
	if cl.OpeningBalance != 0 {
		all = append(all, stmtLine{Date: cl.OpeningBalanceDate, Kind: "opening", Details: "Opening balance", Debit: max(cl.OpeningBalance, 0), Credit: max(-cl.OpeningBalance, 0)})
	}
	var invs []models.Invoice
	if err := database.DB.Where("client_id = ? AND status = ? AND invoice_date <= ?", cl.ID, models.InvoiceIssued, st.To).
		Order("invoice_date, invoice_number").Find(&invs).Error; err != nil {
		return nil, err
	}
	for _, v := range invs {
		d := v.InvoiceDate
		details := "Invoice, due " + v.DueDate.Format("02 Jan 2006")
		if v.BillingMonth != nil {
			details = "Invoice for " + v.BillingMonth.Format("January 2006") + ", due " + v.DueDate.Format("02 Jan 2006")
		}
		all = append(all, stmtLine{Date: &d, Kind: "invoice", Ref: v.InvoiceNumber, Details: details, Debit: v.Total,
			Link: fmt.Sprintf("invoices/%d", v.ID), order: 1})
	}
	var pays []models.Payment
	if err := database.DB.Preload("Allocations.Invoice").Where("client_id = ? AND status = ? AND payment_date <= ?", cl.ID, models.PaymentVerified, st.To).
		Order("payment_date, payment_number").Find(&pays).Error; err != nil {
		return nil, err
	}
	for _, p := range pays {
		d := p.PaymentDate
		details := "Payment, " + models.PaymentMethods[p.Method]
		if p.Reference != "" {
			details += " " + p.Reference
		}
		var against []string
		for _, a := range p.Allocations {
			if a.Invoice != nil {
				against = append(against, a.Invoice.InvoiceNumber)
			}
		}
		if len(against) > 0 {
			details += " (for " + joinMax(against, 4) + ")"
		}
		if p.Amount > 0 {
			all = append(all, stmtLine{Date: &d, Kind: "payment", Ref: p.PaymentNumber, Details: details, Credit: p.Amount,
				Link: fmt.Sprintf("payments/%d", p.ID), order: 2})
		}
		if p.TaxDeducted > 0 {
			all = append(all, stmtLine{Date: &d, Kind: "wht", Ref: p.PaymentNumber, Details: "Income tax deducted by you (WHT)",
				Credit: p.TaxDeducted, Link: fmt.Sprintf("payments/%d", p.ID), order: 3})
		}
	}
	// Date order; same day: invoices, then payments. An opening balance
	// without a date comes first.
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Date == nil || b.Date == nil {
			return a.Date == nil && b.Date != nil
		}
		if !a.Date.Equal(*b.Date) {
			return a.Date.Before(*b.Date)
		}
		return a.order < b.order
	})
	bal := 0.0
	for _, l := range all {
		if l.Date == nil || l.Date.Before(from) {
			bal += l.Debit - l.Credit
			continue
		}
		if st.Lines == nil {
			st.BroughtFwd = roundMoney(bal)
		}
		bal += l.Debit - l.Credit
		l.Balance = roundMoney(bal)
		st.TotalDebit += l.Debit
		st.TotalCredit += l.Credit
		st.Lines = append(st.Lines, l)
	}
	if st.Lines == nil {
		st.BroughtFwd = roundMoney(bal)
		st.Lines = []stmtLine{}
	}
	st.TotalDebit, st.TotalCredit, st.Closing = roundMoney(st.TotalDebit), roundMoney(st.TotalCredit), roundMoney(bal)

	per, _, err := computeAgeing(to, cl.ID)
	if err != nil {
		return nil, err
	}
	if a := per[cl.ID]; a != nil {
		for i := 0; i < 7; i++ {
			st.Ageing[i] = roundMoney(a[i])
		}
	}
	database.DB.Model(&models.Payment{}).Where("client_id = ? AND status = ?", cl.ID, models.PaymentPending).
		Select("COALESCE(SUM(amount + tax_deducted), 0)").Scan(&st.Pending)
	st.Pending = roundMoney(st.Pending)
	return st, nil
}

func joinMax(s []string, n int) string {
	if len(s) <= n {
		return strings.Join(s, ", ")
	}
	return strings.Join(s[:n], ", ") + fmt.Sprintf(" and %d more", len(s)-n)
}

func GetStatement(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var cl models.Client
	if err := database.DB.First(&cl, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Client not found"})
		return
	}
	td := todayDate()
	from, ok := dayParam(c, "from", monthStart(td).AddDate(0, -2, 0))
	if !ok {
		return
	}
	to, ok := dayParam(c, "to", td)
	if !ok {
		return
	}
	if to.Before(from) {
		from, to = to, from
	}
	if from.AddDate(10, 0, 0).Before(to) {
		badRequest(c, "Choose at most 10 years")
		return
	}
	st, err := buildStatement(&cl, from, to)
	if err != nil {
		serverError(c, "Could not make the statement", err)
		return
	}
	if c.Query("format") != "xlsx" {
		c.JSON(http.StatusOK, gin.H{"statement": st, "can_record": auth.Can(auth.User(c).Role, "record_payments")})
		return
	}
	r := &report{
		Name:     fmt.Sprintf("Statement %s %s to %s", cl.ClientNumber, st.From, st.To),
		Tab:      "Statement",
		Title:    "Statement of account: " + cl.Name + " (" + cl.ClientNumber + ")",
		Subtitle: fmtDayLong(from) + " – " + fmtDayLong(to),
		Columns: []reportCol{{"date", "Date", xlsx.Date, 0}, {"ref", "Reference", xlsx.Text, 13}, {"details", "Details", xlsx.Text, 48},
			{"debit", "Invoiced", xlsx.Money, 0}, {"credit", "Paid", xlsx.Money, 0}, {"balance", "Balance", xlsx.Money, 0}},
	}
	r.Rows = append(r.Rows, []any{st.From, nil, "Balance brought forward", nil, nil, st.BroughtFwd})
	for _, l := range st.Lines {
		var d any
		if l.Date != nil {
			d = l.Date.Format("2006-01-02")
		}
		var dr, cr any
		if l.Debit != 0 {
			dr = l.Debit
		}
		if l.Credit != 0 {
			cr = l.Credit
		}
		r.Rows = append(r.Rows, []any{d, l.Ref, l.Details, dr, cr, l.Balance})
	}
	r.Totals = []any{st.To, nil, "Closing balance", st.TotalDebit, st.TotalCredit, st.Closing}
	sendReport(c, r)
}
