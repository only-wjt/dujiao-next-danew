package presenter

import (
	"testing"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/shopspring/decimal"
)

func TestNewOrderDetailAllocatesInvoiceFeeToChildren(t *testing.T) {
	order := &orderdomain.Order{
		OrderNo:          "DJ1",
		TotalAmount:      newMoney("143.00"),
		InvoiceRequested: true,
		InvoiceFeeAmount: newMoney("13.00"),
		Children: []orderdomain.Order{
			{OrderNo: "DJ1-01", TotalAmount: newMoney("130.00")},
		},
	}

	detail := NewOrderDetail(order)
	if got := detail.ActualPaidAmount.String(); got != "143.00" {
		t.Fatalf("parent actual paid = %s, want 143.00", got)
	}
	child := detail.Children[0]
	if child.InvoiceFeeAmount.String() != "13.00" || child.ActualPaidAmount.String() != "143.00" {
		t.Fatalf("child invoice fee = %s, actual paid = %s", child.InvoiceFeeAmount.String(), child.ActualPaidAmount.String())
	}

	ApplyPaymentFee(&detail, decimal.RequireFromString("1.43"))
	if got := detail.ActualPaidAmount.String(); got != "144.43" {
		t.Fatalf("parent actual paid with fee = %s, want 144.43", got)
	}
	child = detail.Children[0]
	if child.PaymentFeeAmount.String() != "1.43" || child.ActualPaidAmount.String() != "144.43" {
		t.Fatalf("child payment fee = %s, actual paid = %s", child.PaymentFeeAmount.String(), child.ActualPaidAmount.String())
	}
}

func TestApplyPaymentFeeSplitsProportionallyAndKeepsTotal(t *testing.T) {
	order := &orderdomain.Order{
		OrderNo:          "DJ2",
		TotalAmount:      newMoney("110.00"),
		InvoiceFeeAmount: newMoney("10.00"),
		Children: []orderdomain.Order{
			{OrderNo: "DJ2-01", TotalAmount: newMoney("33.33")},
			{OrderNo: "DJ2-02", TotalAmount: newMoney("33.33")},
			{OrderNo: "DJ2-03", TotalAmount: newMoney("33.34")},
		},
	}
	detail := NewOrderDetail(order)
	ApplyPaymentFee(&detail, decimal.RequireFromString("1.00"))

	sum := decimal.Zero
	for _, child := range detail.Children {
		sum = sum.Add(child.ActualPaidAmount.Decimal)
	}
	if !sum.Equal(detail.ActualPaidAmount.Decimal) {
		t.Fatalf("children actual paid sum = %s, parent = %s", sum.String(), detail.ActualPaidAmount.String())
	}
	if got := detail.Children[0].InvoiceFeeAmount.String(); got != "3.33" {
		t.Fatalf("first child invoice fee = %s, want 3.33", got)
	}
}
