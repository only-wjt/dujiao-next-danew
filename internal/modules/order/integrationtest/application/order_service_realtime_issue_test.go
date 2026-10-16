package application_test

import (
	"errors"
	"testing"

	"github.com/dujiao-next/internal/constants"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	. "github.com/dujiao-next/internal/modules/order/application"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
)

type stubOrderabilityChecker struct {
	snapshot *cdkissuecontract.RealtimeIssueSnapshot
	err      error
}

func (s stubOrderabilityChecker) CheckOrderable(uint, uint, int) (*cdkissuecontract.RealtimeIssueSnapshot, error) {
	return s.snapshot, s.err
}

func newRealtimeOrderFixture(t *testing.T, name string) (couponLimitRaceFixture, *OrderService) {
	t.Helper()
	f := newCouponLimitRaceFixture(t, name, 0, 0)
	if err := f.db.AutoMigrate(&cardsecretdomain.Secret{}, &cardsecretdomain.Batch{}); err != nil {
		t.Fatalf("migrate card secrets: %v", err)
	}
	if err := f.db.Model(&f.product).Update("fulfillment_type", constants.FulfillmentTypeAuto).Error; err != nil {
		t.Fatalf("switch product to auto: %v", err)
	}
	return f, f.service(f.couponRepo, f.usageRepo)
}

func TestRealtimeIssueOrderSkipsStockAndSnapshotsPlan(t *testing.T) {
	f, svc := newRealtimeOrderFixture(t, "realtime_issue_order")
	svc.SetRealtimeIssueChecker(stubOrderabilityChecker{snapshot: &cdkissuecontract.RealtimeIssueSnapshot{Plan: "pro_5x", PaymentCountry: "PH"}})

	input := f.input(f.users[0].ID)
	input.CouponCode = ""
	order, err := svc.CreateOrder(input)
	if err != nil {
		t.Fatalf("realtime product must be orderable without stock codes: %v", err)
	}

	var items []orderdomain.OrderItem
	f.db.Where("order_id = ?", order.ID).Find(&items)
	if len(items) == 0 {
		var children []orderdomain.Order
		f.db.Where("parent_id = ?", order.ID).Find(&children)
		for _, child := range children {
			f.db.Where("order_id = ?", child.ID).Find(&items)
		}
	}
	if len(items) != 1 {
		t.Fatalf("expected one order item, got %d", len(items))
	}
	plan, country, ok := cdkissuedomain.ReadItemSnapshot(items[0].SKUSnapshotJSON)
	if !ok || plan != "pro_5x" || country != "PH" {
		t.Fatalf("order item must carry the realtime snapshot, got %v", items[0].SKUSnapshotJSON)
	}
	var reserved int64
	f.db.Model(&cardsecretdomain.Secret{}).Where("status = ?", cardsecretdomain.StatusReserved).Count(&reserved)
	if reserved != 0 {
		t.Fatalf("realtime order must not reserve stock codes")
	}
}

func TestRealtimeIssueOrderBlockedWhenNotOrderable(t *testing.T) {
	f, svc := newRealtimeOrderFixture(t, "realtime_issue_blocked")
	svc.SetRealtimeIssueChecker(stubOrderabilityChecker{err: cdkissuecontract.ErrNotOrderable})

	input := f.input(f.users[0].ID)
	input.CouponCode = ""
	if _, err := svc.CreateOrder(input); !errors.Is(err, ErrProductNotAvailable) {
		t.Fatalf("want ErrProductNotAvailable, got %v", err)
	}
}

func TestStockAutoOrderStillRequiresCodes(t *testing.T) {
	f, svc := newRealtimeOrderFixture(t, "stock_auto_order")
	svc.SetRealtimeIssueChecker(stubOrderabilityChecker{})

	input := f.input(f.users[0].ID)
	input.CouponCode = ""
	if _, err := svc.CreateOrder(input); !errors.Is(err, ErrCardSecretInsufficient) {
		t.Fatalf("non realtime auto product without codes must fail, got %v", err)
	}
}
