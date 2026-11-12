package handlers

import (
	"testing"

	"apex-billing/internal/models"
)

func TestInvoiceTaxes(t *testing.T) {
	pst := models.TaxApplied{TaxID: 1, Name: "PST", Rate: 16, Kind: models.TaxAdd, Order: 1}
	fed := models.TaxApplied{TaxID: 2, Name: "Extra", Rate: 10, Kind: models.TaxAdd, Compound: true, Order: 2}
	wht := models.TaxApplied{TaxID: 3, Name: "WHT", Rate: 4, Kind: models.TaxDeduct, Order: 3}

	// One line: 16% on 1000 = 160; compound 10% on 1160 = 116; 4% off 1000 = 40.
	inv := models.Invoice{Lines: []models.InvoiceLine{{Amount: 1000, Taxes: []models.TaxApplied{wht, fed, pst}}}}
	recalcInvoice(&inv)
	if inv.Subtotal != 1000 || inv.TaxAmount != 276 || inv.DeductedAmount != 40 || inv.Total != 1236 {
		t.Fatalf("got subtotal %v tax %v deducted %v total %v", inv.Subtotal, inv.TaxAmount, inv.DeductedAmount, inv.Total)
	}
	if len(inv.TaxLines) != 3 || inv.TaxLines[0].Name != "PST" || inv.TaxLines[1].Base != 1160 || inv.TaxLines[2].Kind != models.TaxDeduct {
		t.Fatalf("tax lines: %+v", inv.TaxLines)
	}

	// Lines with different taxes; each tax rounded once over its lines.
	inv = models.Invoice{Lines: []models.InvoiceLine{
		{Amount: 333.33, Taxes: []models.TaxApplied{pst}},
		{Amount: 333.33, Taxes: []models.TaxApplied{pst}},
		{Amount: 500}, // no tax
	}}
	recalcInvoice(&inv)
	if inv.Subtotal != 1166.66 || inv.TaxAmount != 106.67 || inv.Total != 1273.33 || len(inv.TaxLines) != 1 || inv.TaxLines[0].Base != 666.66 {
		t.Fatalf("got %v %v %v %+v", inv.Subtotal, inv.TaxAmount, inv.Total, inv.TaxLines)
	}

	// A discount line lowers the tax.
	inv = models.Invoice{Lines: []models.InvoiceLine{
		{Amount: 1000, Taxes: []models.TaxApplied{pst}},
		{Amount: -100, Taxes: []models.TaxApplied{pst}},
	}}
	recalcInvoice(&inv)
	if inv.TaxAmount != 144 || inv.Total != 1044 {
		t.Fatalf("discount: tax %v total %v", inv.TaxAmount, inv.Total)
	}

	// An older draft with the single tax %.
	inv = models.Invoice{TaxPercent: 16, Lines: []models.InvoiceLine{{Amount: 1000}}}
	recalcInvoice(&inv)
	if inv.TaxAmount != 160 || inv.Total != 1160 || len(inv.TaxLines) != 1 {
		t.Fatalf("legacy: %v %v %+v", inv.TaxAmount, inv.Total, inv.TaxLines)
	}
}

func TestCheckTaxIDs(t *testing.T) {
	taxes := map[uint]models.Tax{
		1: {ID: 1, Name: "B", Order: 2, Active: true},
		2: {ID: 2, Name: "A", Order: 1, Active: true},
		3: {ID: 3, Name: "Old", Order: 0, Active: false},
	}
	ids, msg := checkTaxIDs([]uint{1, 2, 1}, nil, taxes, 0)
	if msg != "" || len(ids) != 2 || ids[0] != 2 || ids[1] != 1 {
		t.Fatalf("got %v %q", ids, msg)
	}
	if _, msg := checkTaxIDs([]uint{3}, nil, taxes, 0); msg == "" {
		t.Fatal("inactive tax accepted as a new choice")
	}
	if _, msg := checkTaxIDs([]uint{3}, []uint{3}, taxes, 0); msg != "" {
		t.Fatalf("inactive tax already chosen refused: %s", msg)
	}
	if _, msg := checkTaxIDs([]uint{9}, nil, taxes, 0); msg == "" {
		t.Fatal("unknown tax accepted")
	}
}

func TestTaxesPerCompany(t *testing.T) {
	taxes := map[uint]models.Tax{
		1: {ID: 1, CompanyID: 1, Name: "PST", Active: true},
		2: {ID: 2, CompanyID: 2, Name: "SST", Active: true},
	}
	if _, msg := checkTaxIDs([]uint{2}, nil, taxes, 1); msg == "" {
		t.Fatal("another company's tax accepted")
	}
	if ids, msg := checkTaxIDs([]uint{1}, nil, taxes, 1); msg != "" || len(ids) != 1 {
		t.Fatalf("own tax refused: %v %q", ids, msg)
	}
	if ids, msg := checkTaxIDs([]uint{1, 2}, nil, taxes, 0); msg != "" || len(ids) != 2 {
		t.Fatalf("product (any company) refused: %v %q", ids, msg)
	}
	if got := taxesOfCompany([]uint{1, 2}, taxes, 2); len(got) != 1 || got[0] != 2 {
		t.Fatalf("taxesOfCompany: %v", got)
	}
}

func TestInvoicePrefixes(t *testing.T) {
	cases := map[string]string{"inv": "INV", "Apx-1": "APX-1", "": "INV", "ABCDEFGHIJKLMN": "ABCDEFGHIJKL", "jb--inv-": "JB-INV", "J B_inv": "JBINV"}
	for in, want := range cases {
		if got := invoicePrefixOf(&models.Company{InvoicePrefix: in}); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	co := models.Company{}
	for _, p := range []string{"CUS", "pay", "AB_C", "-JB", "JB-", "JB--INV", "TOOLONGPREFIX1"} {
		if msg := applyCompanyInput(companyInput{Name: "X", InvoicePrefix: p}, &co); msg == "" {
			t.Errorf("prefix %q accepted", p)
		}
	}
	if msg := applyCompanyInput(companyInput{Name: "X", InvoicePrefix: "net"}, &co); msg != "" || co.InvoicePrefix != "NET" {
		t.Errorf("prefix net: %q %q", msg, co.InvoicePrefix)
	}
	if msg := applyCompanyInput(companyInput{Name: "X", InvoicePrefix: "jb-inv"}, &co); msg != "" || co.InvoicePrefix != "JB-INV" {
		t.Errorf("prefix jb-inv: %q %q", msg, co.InvoicePrefix)
	}
	if msg := applyCompanyInput(companyInput{InvoicePrefix: "NET"}, &co); msg == "" {
		t.Error("company without a name accepted")
	}
}
