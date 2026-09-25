package application

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func TestCalculateInvoiceFeeAddsTenPercent(t *testing.T) {
	tests := []struct {
		total string
		want  string
	}{
		{"150", "15"},
		{"10900", "1090"},
		{"10.01", "1.00"},
		{"0", "0"},
	}
	for _, test := range tests {
		got := CalculateInvoiceFee(decimal.RequireFromString(test.total))
		if !got.Equal(decimal.RequireFromString(test.want)) {
			t.Fatalf("fee(%s) = %s, want %s", test.total, got, test.want)
		}
	}
}

func TestNormalizeInvoiceInputRequiresDetailsOnCreate(t *testing.T) {
	if _, err := NormalizeInvoiceInput(InvoiceInput{Requested: true}, true); !errors.Is(err, ErrInvoiceTitleRequired) {
		t.Fatalf("missing title: %v", err)
	}
	if _, err := NormalizeInvoiceInput(InvoiceInput{Requested: true, Title: "上海某某科技"}, true); !errors.Is(err, ErrInvoiceEmailRequired) {
		t.Fatalf("missing email: %v", err)
	}
	if _, err := NormalizeInvoiceInput(InvoiceInput{Requested: true, Title: "上海某某科技", Email: "bad"}, true); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("invalid email: %v", err)
	}

	got, err := NormalizeInvoiceInput(InvoiceInput{
		Requested: true,
		Title:     "  上海某某科技  ",
		TaxNo:     " 91310000 ma1fl0000x ",
		Note:      "  备注  ",
		Email:     "  Billing@Example.com  ",
	}, true)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !got.Requested || got.Title != "上海某某科技" || got.TaxNo != "91310000MA1FL0000X" || got.Note != "备注" || got.Email != "billing@example.com" {
		t.Fatalf("unexpected invoice: %#v", got)
	}
}

func TestNormalizeInvoiceInputPreviewOnlyNeedsSwitch(t *testing.T) {
	got, err := NormalizeInvoiceInput(InvoiceInput{Requested: true}, false)
	if err != nil {
		t.Fatalf("preview normalize: %v", err)
	}
	if !got.Requested || got.Title != "" || got.Email != "" {
		t.Fatalf("preview should only keep the switch: %#v", got)
	}
}

func TestApplyInvoiceToBuildResultInflatesTotal(t *testing.T) {
	result := &orderBuildResult{TotalAmount: decimal.RequireFromString("150")}
	applyInvoiceToBuildResult(result, InvoiceInput{Requested: true})
	if !result.InvoiceFeeAmount.Equal(decimal.NewFromInt(15)) {
		t.Fatalf("fee = %s", result.InvoiceFeeAmount)
	}
	if !result.TotalAmount.Equal(decimal.NewFromInt(165)) {
		t.Fatalf("total = %s", result.TotalAmount)
	}
}
