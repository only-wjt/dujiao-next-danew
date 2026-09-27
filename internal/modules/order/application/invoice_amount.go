package application

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// InvoiceableBreakdown 是订单可开票金额的唯一计算结果。
type InvoiceableBreakdown struct {
	OrderNo             string       `json:"order_no"`
	RootOrderNo         string       `json:"root_order_no,omitempty"`
	OrderStatus         string       `json:"order_status"`
	Currency            string       `json:"currency"`
	InvoiceRequested    bool         `json:"invoice_requested"`
	TotalAmount         money.Amount `json:"total_amount"`
	InvoiceFee          money.Amount `json:"invoice_fee"`
	WalletPaid          money.Amount `json:"wallet_paid"`
	OnlinePaid          money.Amount `json:"online_paid"`
	CustomerPaid        money.Amount `json:"customer_paid"`
	CustomerSurcharge   money.Amount `json:"customer_surcharge"`
	ExpectedPaid        money.Amount `json:"expected_paid"`
	Refunded            money.Amount `json:"refunded"`
	Invoiceable         money.Amount `json:"invoiceable"`
	InvoiceableState    string       `json:"invoiceable_state"`
	Consistent          bool         `json:"consistent"`
	Inconsistency       string       `json:"inconsistency"`
	HasPaymentException bool         `json:"has_payment_exception"`
	Fingerprint         string       `json:"fingerprint"`
	ComputedAt          time.Time    `json:"computed_at"`
}

// ComputeInvoiceableAmount 计算商城订单可开票金额。支付多少开多少；未申请开票、非人民币、任何退款都不开。
func ComputeInvoiceableAmount(root *orderdomain.Order, children []orderdomain.Order, payments []paymentdomain.Payment, refunds []orderdomain.OrderRefundRecord, now time.Time) InvoiceableBreakdown {
	out := InvoiceableBreakdown{
		OrderNo:          root.OrderNo,
		OrderStatus:      root.Status,
		Currency:         root.Currency,
		InvoiceRequested: root.InvoiceRequested,
		TotalAmount:      root.TotalAmount,
		InvoiceFee:       root.InvoiceFeeAmount,
		Consistent:       true,
		ComputedAt:       now,
	}
	if root.ParentID != nil {
		out.InvoiceableState = "not_root_order"
		return out
	}
	if !root.InvoiceRequested {
		out.InvoiceableState = "not_requested"
		return out
	}
	if !strings.EqualFold(strings.TrimSpace(root.Currency), "CNY") {
		out.InvoiceableState = "non_cny"
		return out
	}
	switch root.Status {
	case constants.OrderStatusPendingPayment:
		out.InvoiceableState = "not_paid"
		return out
	case constants.OrderStatusCanceled:
		out.InvoiceableState = "canceled"
		return out
	}

	wallet := root.WalletPaidAmount.Decimal
	online := decimal.Zero
	surcharge := decimal.Zero
	payParts := make([]idAmount, 0, len(payments))
	for _, p := range payments {
		if p.Status != constants.PaymentStatusSuccess || p.ProviderType == constants.PaymentProviderWallet {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(p.Currency), strings.TrimSpace(root.Currency)) && strings.TrimSpace(p.Currency) != "" {
			continue
		}
		if p.ExceptionCode != "" {
			out.HasPaymentException = true
			continue
		}
		online = online.Add(p.Amount.Decimal)
		payParts = append(payParts, idAmount{id: p.ID, amount: p.Amount.Decimal})
		switch p.FeePolicy {
		case "customer_surcharge", "legacy_customer_surcharge", "":
			if p.FeeAmount.Decimal.IsPositive() {
				surcharge = surcharge.Add(p.FeeAmount.Decimal)
			}
		}
	}
	customerPaid := wallet.Add(online)
	expected := root.TotalAmount.Decimal.Add(surcharge)
	out.WalletPaid = money.FromDecimal(wallet)
	out.OnlinePaid = money.FromDecimal(online)
	out.CustomerPaid = money.FromDecimal(customerPaid)
	out.ExpectedPaid = money.FromDecimal(expected)
	out.CustomerSurcharge = money.FromDecimal(surcharge)
	if !customerPaid.Equal(expected) {
		out.Consistent = false
		out.Inconsistency = fmt.Sprintf("实付 %s，应付 %s", customerPaid.StringFixed(2), expected.StringFixed(2))
	}

	recordSum := decimal.Zero
	refundParts := make([]idAmount, 0, len(refunds))
	for _, r := range refunds {
		recordSum = recordSum.Add(r.Amount.Decimal)
		refundParts = append(refundParts, idAmount{id: r.ID, amount: r.Amount.Decimal})
	}
	storedRefunded := root.RefundedAmount.Decimal
	for _, child := range children {
		storedRefunded = storedRefunded.Add(child.RefundedAmount.Decimal)
	}
	if !recordSum.Equal(storedRefunded) {
		out.Consistent = false
		if out.Inconsistency != "" {
			out.Inconsistency += "；"
		}
		out.Inconsistency += fmt.Sprintf("退款记录 %s，订单已退 %s", recordSum.StringFixed(2), storedRefunded.StringFixed(2))
	}
	out.Refunded = money.FromDecimal(recordSum)
	out.Fingerprint = fingerprint(root.OrderNo, root.Status, payParts, refundParts)

	switch {
	case !out.Consistent:
		out.InvoiceableState = "inconsistent"
	case root.Status == constants.OrderStatusPartiallyRefunded || (recordSum.IsPositive() && recordSum.LessThan(customerPaid)):
		out.InvoiceableState = "partial_refund"
	case root.Status == constants.OrderStatusRefunded || (recordSum.IsPositive() && recordSum.GreaterThanOrEqual(customerPaid)):
		out.InvoiceableState = "fully_refunded"
	default:
		out.Invoiceable = money.FromDecimal(customerPaid)
		out.InvoiceableState = "ok"
	}
	return out
}

type idAmount struct {
	id     uint
	amount decimal.Decimal
}

func fingerprint(orderNo, status string, pays, refunds []idAmount) string {
	sort.Slice(pays, func(i, j int) bool { return pays[i].id < pays[j].id })
	sort.Slice(refunds, func(i, j int) bool { return refunds[i].id < refunds[j].id })
	var b strings.Builder
	b.WriteString(orderNo)
	b.WriteByte('|')
	b.WriteString(status)
	b.WriteByte('|')
	for i, p := range pays {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d:%s", p.id, p.amount.StringFixed(2))
	}
	b.WriteByte('|')
	for i, r := range refunds {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%d:%s", r.id, r.amount.StringFixed(2))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
