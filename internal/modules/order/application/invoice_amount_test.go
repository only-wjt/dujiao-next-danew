package application

import (
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func amt(s string) money.Amount {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return money.FromDecimal(d)
}

func pay(id uint, amount, fee, policy, exception string) paymentdomain.Payment {
	return paymentdomain.Payment{
		ID: id, Status: constants.PaymentStatusSuccess, ProviderType: "epay", Currency: "CNY",
		Amount: amt(amount), FeeAmount: amt(fee), FeePolicy: policy, ExceptionCode: exception,
	}
}

func TestComputeInvoiceableAmountTable(t *testing.T) {
	now := time.Date(2026, 9, 25, 13, 0, 0, 0, time.FixedZone("CST", 8*3600))
	cases := []struct {
		name   string
		order  orderdomain.Order
		pays   []paymentdomain.Payment
		refund []orderdomain.OrderRefundRecord
		state  string
		amount string
		exc    bool
	}{
		{
			name: "customer surcharge",
			order: orderdomain.Order{OrderNo: "A", Status: constants.OrderStatusPaid, Currency: "CNY", InvoiceRequested: true, TotalAmount: amt("165.00")},
			pays:  []paymentdomain.Payment{pay(1, "166.65", "1.65", "customer_surcharge", "")},
			state: "ok", amount: "166.65",
		},
		{
			name: "merchant fee",
			order: orderdomain.Order{OrderNo: "B", Status: constants.OrderStatusPaid, Currency: "CNY", InvoiceRequested: true, TotalAmount: amt("165.00")},
			pays:  []paymentdomain.Payment{pay(1, "165.00", "1.65", "none", "")},
			state: "ok", amount: "165.00",
		},
		{
			name: "wallet plus online",
			order: orderdomain.Order{OrderNo: "C", Status: constants.OrderStatusDelivered, Currency: "CNY", InvoiceRequested: true, TotalAmount: amt("165.00"), WalletPaidAmount: amt("65.00")},
			pays:  []paymentdomain.Payment{pay(2, "101.00", "1.00", "customer_surcharge", "")},
			state: "ok", amount: "166.00",
		},
		{
			name:  "not requested",
			order: orderdomain.Order{OrderNo: "D", Status: constants.OrderStatusPaid, Currency: "CNY", TotalAmount: amt("150.00")},
			pays:  []paymentdomain.Payment{pay(1, "151.50", "1.50", "customer_surcharge", "")},
			state: "not_requested", amount: "0.00",
		},
		{
			name: "full refund",
			order: orderdomain.Order{OrderNo: "E", Status: constants.OrderStatusPaid, Currency: "CNY", InvoiceRequested: true, TotalAmount: amt("165.00"), RefundedAmount: amt("166.65")},
			pays:  []paymentdomain.Payment{pay(1, "166.65", "1.65", "customer_surcharge", "")},
			refund: []orderdomain.OrderRefundRecord{{ID: 9, Amount: amt("166.65")}},
			state: "fully_refunded", amount: "0.00",
		},
		{
			name: "partial refund",
			order: orderdomain.Order{OrderNo: "F", Status: constants.OrderStatusPaid, Currency: "CNY", InvoiceRequested: true, TotalAmount: amt("165.00"), RefundedAmount: amt("50.00")},
			pays:  []paymentdomain.Payment{pay(1, "166.65", "1.65", "customer_surcharge", "")},
			refund: []orderdomain.OrderRefundRecord{{ID: 8, Amount: amt("50.00")}},
			state: "partial_refund", amount: "0.00",
		},
		{
			name: "duplicate payment",
			order: orderdomain.Order{OrderNo: "G", Status: constants.OrderStatusPaid, Currency: "CNY", InvoiceRequested: true, TotalAmount: amt("165.00")},
			pays: []paymentdomain.Payment{
				pay(1, "166.65", "1.65", "customer_surcharge", ""),
				pay(2, "166.65", "1.65", "customer_surcharge", "duplicate"),
			},
			state: "ok", amount: "166.65", exc: true,
		},
		{
			name: "inconsistent",
			order: orderdomain.Order{OrderNo: "H", Status: constants.OrderStatusPaid, Currency: "CNY", InvoiceRequested: true, TotalAmount: amt("165.00")},
			pays:  []paymentdomain.Payment{pay(1, "160.00", "0", "none", "")},
			state: "inconsistent", amount: "0.00",
		},
		{
			name:  "usd",
			order: orderdomain.Order{OrderNo: "I", Status: constants.OrderStatusPaid, Currency: "USD", InvoiceRequested: true, TotalAmount: amt("20.00")},
			state: "non_cny", amount: "0.00",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeInvoiceableAmount(&tc.order, nil, tc.pays, tc.refund, now)
			if got.InvoiceableState != tc.state || got.Invoiceable.StringFixed(2) != tc.amount || got.HasPaymentException != tc.exc {
				t.Fatalf("got state=%s amount=%s exc=%v inconsistency=%s", got.InvoiceableState, got.Invoiceable.StringFixed(2), got.HasPaymentException, got.Inconsistency)
			}
		})
	}
}

func TestComputeInvoiceableAmountChildOrder(t *testing.T) {
	parent := uint(3)
	order := &orderdomain.Order{OrderNo: "CHILD", ParentID: &parent, Status: constants.OrderStatusPaid, Currency: "CNY", InvoiceRequested: true}
	got := ComputeInvoiceableAmount(order, nil, nil, nil, time.Now())
	if got.InvoiceableState != "not_root_order" {
		t.Fatalf("got %s", got.InvoiceableState)
	}
}
