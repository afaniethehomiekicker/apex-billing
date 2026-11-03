package handlers

import (
	"testing"
	"time"

	"apex-billing/internal/models"
)

func TestAddMonths(t *testing.T) {
	cases := [][3]interface{}{
		{"2026-01-31", 1, "2026-02-28"},
		{"2028-01-31", 1, "2028-02-29"},
		{"2026-03-15", 12, "2027-03-15"},
		{"2026-12-31", 2, "2027-02-28"},
	}
	for _, c := range cases {
		if got := addMonths(d(c[0].(string)), c[1].(int)).Format("2006-01-02"); got != c[2] {
			t.Errorf("addMonths(%v, %v) = %s, want %s", c[0], c[1], got, c[2])
		}
	}
}

func TestContractState(t *testing.T) {
	today := d("2026-11-01")
	k := func(start string, end *time.Time, auto bool, notice int, status string) *models.Contract {
		return &models.Contract{StartDate: d(start), EndDate: end, AutoRenew: auto, RenewMonths: 12, NoticeDays: notice, Status: status}
	}
	cases := []struct {
		c       *models.Contract
		state   string
		termEnd string
	}{
		{k("2026-01-01", nil, false, 30, "active"), "active", ""},
		{k("2026-12-01", dp("2027-11-30"), false, 30, "active"), "upcoming", "2027-11-30"},
		{k("2025-01-01", dp("2026-12-31"), false, 30, "active"), "active", "2026-12-31"},
		{k("2025-01-01", dp("2026-11-30"), false, 30, "active"), "due", "2026-11-30"},
		{k("2025-01-01", dp("2026-10-31"), false, 30, "active"), "expired", "2026-10-31"},
		{k("2024-01-01", dp("2024-12-31"), true, 30, "active"), "active", "2026-12-31"},
		{k("2024-12-01", dp("2025-11-30"), true, 60, "active"), "due", "2026-11-30"},
		{k("2024-11-01", dp("2025-10-31"), true, 60, "active"), "active", "2027-10-31"},
		{k("2025-01-01", dp("2026-12-31"), false, 30, "draft"), "draft", "2026-12-31"},
		{k("2025-01-01", dp("2025-06-30"), true, 30, "terminated"), "terminated", "2025-06-30"},
	}
	for i, c := range cases {
		st, end, _, _ := contractState(c.c, today)
		got := ""
		if end != nil {
			got = end.Format("2006-01-02")
		}
		if st != c.state || got != c.termEnd {
			t.Errorf("case %d: got %s / %s, want %s / %s", i, st, got, c.state, c.termEnd)
		}
	}
}
