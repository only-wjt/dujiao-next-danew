package application

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/dujiao-next/internal/constants"
	"github.com/shopspring/decimal"
)

const (
	invoiceTitleMaxLen = 200
	invoiceNoteMaxLen  = 500
)

// InvoiceInput 结账开票请求。
type InvoiceInput struct {
	Requested bool
	Title     string
	Note      string
	Email     string
}

func normalizeOrderAmount(amount decimal.Decimal) decimal.Decimal {
	normalized := amount.Round(2)
	if normalized.LessThan(decimal.Zero) {
		return decimal.Zero
	}
	return normalized
}

// NormalizeInvoiceInput 归一化开票请求。预览只认开关；创建时抬头和收票邮箱必填。
func NormalizeInvoiceInput(input InvoiceInput, requireDetails bool) (InvoiceInput, error) {
	next := InvoiceInput{
		Requested: input.Requested,
		Title:     strings.TrimSpace(input.Title),
		Note:      strings.TrimSpace(input.Note),
		Email:     strings.ToLower(strings.TrimSpace(input.Email)),
	}
	if utf8.RuneCountInString(next.Title) > invoiceTitleMaxLen {
		next.Title = string([]rune(next.Title)[:invoiceTitleMaxLen])
	}
	if utf8.RuneCountInString(next.Note) > invoiceNoteMaxLen {
		next.Note = string([]rune(next.Note)[:invoiceNoteMaxLen])
	}
	if !next.Requested {
		return InvoiceInput{}, nil
	}
	if !requireDetails {
		return InvoiceInput{Requested: true}, nil
	}
	if next.Title == "" {
		return InvoiceInput{}, ErrInvoiceTitleRequired
	}
	if next.Email == "" {
		return InvoiceInput{}, ErrInvoiceEmailRequired
	}
	if _, err := mail.ParseAddress(next.Email); err != nil {
		return InvoiceInput{}, ErrInvalidEmail
	}
	return next, nil
}

// CalculateInvoiceFee 按应付金额计算开票加价。
func CalculateInvoiceFee(total decimal.Decimal) decimal.Decimal {
	if !total.GreaterThan(decimal.Zero) {
		return decimal.Zero
	}
	return total.Mul(decimal.NewFromInt(int64(constants.InvoiceFeePercent))).Div(decimal.NewFromInt(100)).Round(2)
}

func applyInvoiceToBuildResult(result *orderBuildResult, invoice InvoiceInput) {
	if result == nil || !invoice.Requested {
		return
	}
	fee := CalculateInvoiceFee(result.TotalAmount)
	result.InvoiceFeeAmount = fee
	result.TotalAmount = normalizeOrderAmount(result.TotalAmount.Add(fee))
}
