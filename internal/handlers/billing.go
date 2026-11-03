package handlers

import (
	"sort"
	"time"

	"apex-billing/internal/models"

	"gorm.io/gorm"
)

// The billing rules, in one place.
//
//   - Monthly (MRC) is billed in advance: the run for a month charges that
//     whole month. A month the service only partly covers (it starts or
//     ends inside it) is charged by days: amount × days used ÷ days in the
//     month. Anything not yet billed (a part month before the run's month,
//     or months missed) is added to the next run, one line per month.
//   - One-time: once, on the first run (or invoice) for a month on or
//     after its date.
//   - Per annum: one full year at a time, from the start date and each
//     anniversary, on the run for the month the year starts in.
//   - Suspended services are not billed; nothing before "don't bill
//     before" is billed.
//   - What is already on an issued or draft invoice is never billed again.

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func monthEnd(t time.Time) time.Time { return monthStart(t).AddDate(0, 1, -1) }

func minDay(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func dayPtr(t time.Time) *time.Time { return &t }

// lineAmount: quantity × rate, by days for a part month.
func lineAmount(qty, rate float64, days, of int) float64 {
	v := qty * rate
	if days > 0 && of > 0 {
		v = v * float64(days) / float64(of)
	}
	return roundMoney(v)
}

func taxOf(subtotal, pct float64) float64 { return roundMoney(subtotal * pct / 100) }

// Safety limit on lines one service can add in one go (5 years of months).
const maxLinesPerService = 60

// chargesDue lists the lines a service owes up to the end of upTo's month.
// covered: the last day already on an invoice (nil: none); for a one-time
// charge, any value means it is already invoiced.
func chargesDue(s *models.Subscription, upTo time.Time, covered *time.Time) []models.InvoiceLine {
	if s.Status != "active" {
		return nil
	}
	last := monthEnd(upTo)
	if s.EndDate != nil && s.EndDate.Before(last) {
		last = *s.EndDate
	}
	base := func() models.InvoiceLine {
		id := s.ID
		return models.InvoiceLine{
			SubscriptionID: &id, ChargeType: s.ChargeType, Description: s.Description,
			Reference: s.Reference, Quantity: s.Quantity, Rate: s.Rate,
		}
	}
	var out []models.InvoiceLine

	switch s.ChargeType {
	case models.ChargeOneTime:
		if covered != nil || s.StartDate.After(monthEnd(upTo)) {
			return nil
		}
		l := base()
		l.PeriodStart, l.PeriodEnd = dayPtr(s.StartDate), dayPtr(s.StartDate)
		l.Amount = lineAmount(s.Quantity, s.Rate, 0, 0)
		return []models.InvoiceLine{l}

	case models.ChargeMonthly:
		from := s.StartDate
		if s.BillFrom != nil && s.BillFrom.After(from) {
			from = *s.BillFrom
		}
		if covered != nil && !covered.Before(from) {
			from = covered.AddDate(0, 0, 1)
		}
		for cur := from; !cur.After(last) && len(out) < maxLinesPerService; {
			me := monthEnd(cur)
			end := minDay(me, last)
			l := base()
			l.PeriodStart, l.PeriodEnd = dayPtr(cur), dayPtr(end)
			days, of := end.Day()-cur.Day()+1, me.Day()
			if days < of {
				l.ProrateDays, l.ProrateOf = days, of
			}
			l.Amount = lineAmount(s.Quantity, s.Rate, l.ProrateDays, l.ProrateOf)
			out = append(out, l)
			cur = me.AddDate(0, 0, 1)
		}

	case models.ChargeAnnual:
		for k := 0; k < 100 && len(out) < maxLinesPerService; k++ {
			ps := s.StartDate.AddDate(k, 0, 0)
			if ps.After(last) {
				break
			}
			pe := s.StartDate.AddDate(k+1, 0, -1)
			if s.EndDate != nil && s.EndDate.Before(pe) {
				pe = *s.EndDate
			}
			if covered != nil && !pe.After(*covered) {
				continue // already billed
			}
			if s.BillFrom != nil && pe.Before(*s.BillFrom) {
				continue // before "don't bill before"
			}
			l := base()
			l.PeriodStart, l.PeriodEnd = dayPtr(ps), dayPtr(pe)
			l.Amount = lineAmount(s.Quantity, s.Rate, 0, 0)
			out = append(out, l)
		}
	}
	return out
}

// coveredUpTo: for each service, the last day already on an issued or
// draft invoice.
func coveredUpTo(tx *gorm.DB, subIDs []uint) (map[uint]time.Time, error) {
	out := map[uint]time.Time{}
	if len(subIDs) == 0 {
		return out, nil
	}
	type row struct {
		SubscriptionID uint
		Last           time.Time
	}
	var rows []row
	err := tx.Raw(`SELECT l.subscription_id, MAX(l.period_end) AS last
		FROM invoice_lines l JOIN invoices i ON i.id = l.invoice_id
		WHERE l.subscription_id IN ? AND l.period_end IS NOT NULL AND i.status IN ('draft', 'issued')
		GROUP BY l.subscription_id`, subIDs).Scan(&rows).Error
	for _, r := range rows {
		out[r.SubscriptionID] = r.Last
	}
	return out, err
}

// billKey: one invoice per client and billing company.
type billKey struct {
	ClientID  uint
	CompanyID uint
}

// dueForServices works out the due lines for a set of services, grouped by
// client and the company that bills them, in a stable order.
func dueForServices(tx *gorm.DB, subs []models.Subscription, upTo time.Time) (map[billKey][]models.InvoiceLine, error) {
	ids := make([]uint, 0, len(subs))
	for _, s := range subs {
		ids = append(ids, s.ID)
	}
	covered, err := coveredUpTo(tx, ids)
	if err != nil {
		return nil, err
	}
	taxes, err := allTaxes(tx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(subs, func(i, j int) bool {
		if !subs[i].StartDate.Equal(subs[j].StartDate) {
			return subs[i].StartDate.Before(subs[j].StartDate)
		}
		return subs[i].ID < subs[j].ID
	})
	out := map[billKey][]models.InvoiceLine{}
	for i := range subs {
		var cov *time.Time
		if c, ok := covered[subs[i].ID]; ok {
			cov = &c
		}
		if lines := chargesDue(&subs[i], upTo, cov); len(lines) > 0 {
			// Each line carries the service's taxes at today's rates.
			for j := range lines {
				lines[j].Taxes = snapshotTaxes(subs[i].TaxIDs, taxes)
			}
			k := billKey{subs[i].ClientID, subs[i].CompanyID}
			out[k] = append(out[k], lines...)
		}
	}
	// Within a client: by period, so a part month comes before the month
	// after it.
	for k := range out {
		ls := out[k]
		sort.SliceStable(ls, func(i, j int) bool { return ls[i].PeriodStart.Before(*ls[j].PeriodStart) })
	}
	return out, nil
}

// copyClientDetails puts the client's billing details on the invoice.
func copyClientDetails(inv *models.Invoice, cl *models.Client) {
	inv.BillName, inv.BillContact, inv.BillEmail = cl.Name, cl.ContactPerson, cl.Email
	inv.BillClientType = cl.ClientType
	inv.BillPhone = cl.Phone
	if inv.BillPhone == "" {
		inv.BillPhone = cl.Mobile
	}
	inv.BillAddress, inv.BillCity, inv.BillProvince = cl.Address, cl.City, cl.Province
	inv.BillNTN, inv.BillSTRN, inv.BillCNIC = cl.NTN, cl.STRN, cl.CNIC
}

// refreshBilledThrough sets each service's "billed up to" from the issued
// invoices that bill it (so cancelling an invoice un-bills its periods).
func refreshBilledThrough(tx *gorm.DB, subIDs []uint) error {
	if len(subIDs) == 0 {
		return nil
	}
	return tx.Exec(`UPDATE subscriptions s SET billed_through = (
			SELECT MAX(l.period_end) FROM invoice_lines l JOIN invoices i ON i.id = l.invoice_id
			WHERE l.subscription_id = s.id AND i.status = 'issued')
		WHERE s.id IN ?`, subIDs).Error
}

func lineServiceIDs(lines []models.InvoiceLine) []uint {
	seen := map[uint]bool{}
	var ids []uint
	for _, l := range lines {
		if l.SubscriptionID != nil && !seen[*l.SubscriptionID] {
			seen[*l.SubscriptionID] = true
			ids = append(ids, *l.SubscriptionID)
		}
	}
	return ids
}

// lockBilling serialises invoice generation and issuing (one at a time),
// for the rest of the transaction.
func lockBilling(tx *gorm.DB) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(73315001)").Error
}
