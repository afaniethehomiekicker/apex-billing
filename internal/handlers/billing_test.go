package handlers

import (
	"testing"
	"time"

	"apex-billing/internal/models"
)

// Run with: go test ./internal/handlers/

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func dp(s string) *time.Time { t := d(s); return &t }

type want struct {
	from, to string
	days, of int
	amount   float64
}

func check(t *testing.T, name string, got []models.InvoiceLine, w []want) {
	t.Helper()
	if len(got) != len(w) {
		t.Fatalf("%s: got %d lines, want %d: %+v", name, len(got), len(w), got)
	}
	for i, l := range got {
		if l.PeriodStart.Format("2006-01-02") != w[i].from || l.PeriodEnd.Format("2006-01-02") != w[i].to ||
			l.ProrateDays != w[i].days || l.ProrateOf != w[i].of || l.Amount != w[i].amount {
			t.Errorf("%s line %d: got %s..%s %d/%d %.2f, want %+v", name, i+1,
				l.PeriodStart.Format("2006-01-02"), l.PeriodEnd.Format("2006-01-02"), l.ProrateDays, l.ProrateOf, l.Amount, w[i])
		}
	}
}

func TestMonthly(t *testing.T) {
	s := models.Subscription{ID: 1, ChargeType: models.ChargeMonthly, Status: "active", Quantity: 1, Rate: 45000, StartDate: d("2026-11-18")}

	// Started mid-November, first run is December's: the part month and December.
	check(t, "first run", chargesDue(&s, d("2026-12-01"), nil), []want{
		{"2026-11-18", "2026-11-30", 13, 30, 19500},
		{"2026-12-01", "2026-12-31", 0, 0, 45000},
	})
	// November's run (in advance) already covers the part month.
	check(t, "november run", chargesDue(&s, d("2026-11-01"), nil), []want{{"2026-11-18", "2026-11-30", 13, 30, 19500}})
	// Billed to the end of November: only December.
	check(t, "after november", chargesDue(&s, d("2026-12-01"), dp("2026-11-30")), []want{{"2026-12-01", "2026-12-31", 0, 0, 45000}})
	// Already billed for December: nothing.
	check(t, "already billed", chargesDue(&s, d("2026-12-01"), dp("2026-12-31")), nil)

	// Ending 10 Feb 2027: February is 10 of 28 days, nothing after.
	s.EndDate = dp("2027-02-10")
	check(t, "last month", chargesDue(&s, d("2027-02-01"), dp("2027-01-31")), []want{{"2027-02-01", "2027-02-10", 10, 28, 16071.43}})
	check(t, "after end", chargesDue(&s, d("2027-03-01"), dp("2027-02-10")), nil)

	// Quantity 2.
	q := models.Subscription{ID: 2, ChargeType: models.ChargeMonthly, Status: "active", Quantity: 2, Rate: 42000, StartDate: d("2026-10-01")}
	check(t, "quantity", chargesDue(&q, d("2026-10-01"), nil), []want{{"2026-10-01", "2026-10-31", 0, 0, 84000}})

	// Suspended: nothing.
	q.Status = "suspended"
	check(t, "suspended", chargesDue(&q, d("2026-11-01"), nil), nil)

	// Moved from an older system: started 2020, bill from 10 Dec 2026.
	m := models.Subscription{ID: 3, ChargeType: models.ChargeMonthly, Status: "active", Quantity: 1, Rate: 31000,
		StartDate: d("2020-03-01"), BillFrom: dp("2026-12-10")}
	check(t, "bill from", chargesDue(&m, d("2026-12-01"), nil), []want{{"2026-12-10", "2026-12-31", 22, 31, 22000}})

	// Starts and ends inside one month.
	o := models.Subscription{ID: 4, ChargeType: models.ChargeMonthly, Status: "active", Quantity: 1, Rate: 30000,
		StartDate: d("2026-11-05"), EndDate: dp("2026-11-14")}
	check(t, "inside one month", chargesDue(&o, d("2026-11-01"), nil), []want{{"2026-11-05", "2026-11-14", 10, 30, 10000}})

	// Starts after the run's month: nothing yet.
	f := models.Subscription{ID: 5, ChargeType: models.ChargeMonthly, Status: "active", Quantity: 1, Rate: 1000, StartDate: d("2027-01-01")}
	check(t, "future", chargesDue(&f, d("2026-12-01"), nil), nil)
}

func TestOneTimeAndAnnual(t *testing.T) {
	ot := models.Subscription{ID: 1, ChargeType: models.ChargeOneTime, Status: "active", Quantity: 1, Rate: 25000, StartDate: d("2026-10-14")}
	check(t, "one-time due", chargesDue(&ot, d("2026-10-01"), nil), []want{{"2026-10-14", "2026-10-14", 0, 0, 25000}})
	check(t, "one-time later run", chargesDue(&ot, d("2026-12-01"), nil), []want{{"2026-10-14", "2026-10-14", 0, 0, 25000}})
	check(t, "one-time not yet", chargesDue(&ot, d("2026-09-01"), nil), nil)
	check(t, "one-time billed", chargesDue(&ot, d("2026-12-01"), dp("2026-10-14")), nil)

	an := models.Subscription{ID: 2, ChargeType: models.ChargeAnnual, Status: "active", Quantity: 1, Rate: 12000, StartDate: d("2026-01-15")}
	check(t, "annual first", chargesDue(&an, d("2026-01-01"), nil), []want{{"2026-01-15", "2027-01-14", 0, 0, 12000}})
	check(t, "annual mid-year", chargesDue(&an, d("2026-06-01"), dp("2027-01-14")), nil)
	check(t, "annual renewal", chargesDue(&an, d("2027-01-01"), dp("2027-01-14")), []want{{"2027-01-15", "2028-01-14", 0, 0, 12000}})
}

func TestTax(t *testing.T) {
	inv := models.Invoice{TaxPercent: 16, Lines: []models.InvoiceLine{{Amount: 19500}, {Amount: 45000}}}
	recalcInvoice(&inv)
	if inv.Subtotal != 64500 || inv.TaxAmount != 10320 || inv.Total != 74820 {
		t.Fatalf("tax: %+v", inv)
	}
}
