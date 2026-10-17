package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/dujiao-next/internal/modules/fulfillment/application"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	fulfillmentgormstore "github.com/dujiao-next/internal/modules/fulfillment/infrastructure/gormstore"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"

	"github.com/dujiao-next/internal/constants"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type stubRealtimeIssuer struct {
	realtimeProduct uint
	codes           []string
	err             error
	delivered       []uint
	issued          int
}

func (s *stubRealtimeIssuer) RealtimeItems(order cdkissuecontract.IssueOrder) (map[uint]bool, error) {
	out := map[uint]bool{}
	for _, item := range order.Items {
		// 交付服务必须把下单快照透传给发码模块。
		if item.Realtime && item.Plan == "plus" && item.ProductID == s.realtimeProduct {
			out[item.OrderItemID] = true
		}
	}
	return out, nil
}

func (s *stubRealtimeIssuer) EnsureIssued(_ context.Context, order cdkissuecontract.IssueOrder) (cdkissuecontract.IssuedItemCodes, error) {
	s.issued++
	if s.err != nil {
		return nil, s.err
	}
	out := cdkissuecontract.IssuedItemCodes{}
	for _, item := range order.Items {
		if item.ProductID == s.realtimeProduct {
			out[item.OrderItemID] = s.codes
		}
	}
	return out, nil
}

func (s *stubRealtimeIssuer) MarkDelivered(orderID uint) error {
	s.delivered = append(s.delivered, orderID)
	return nil
}

func createPaidAutoOrder(t *testing.T, db *gorm.DB, orderNo string, productID uint, quantity int) *orderdomain.Order {
	t.Helper()
	now := time.Now()
	order := &orderdomain.Order{
		OrderNo:                 orderNo,
		UserID:                  1,
		Status:                  constants.OrderStatusPaid,
		Currency:                "CNY",
		OriginalAmount:          money.FromDecimal(decimal.NewFromInt(10)),
		DiscountAmount:          money.FromDecimal(decimal.Zero),
		PromotionDiscountAmount: money.FromDecimal(decimal.Zero),
		TotalAmount:             money.FromDecimal(decimal.NewFromInt(10)),
		WalletPaidAmount:        money.FromDecimal(decimal.Zero),
		OnlinePaidAmount:        money.FromDecimal(decimal.NewFromInt(10)),
		RefundedAmount:          money.FromDecimal(decimal.Zero),
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("create order failed: %v", err)
	}
	skuSnapshot := jsonmap.JSON{"sku_id": 0}
	cdkissuedomain.ApplyItemSnapshot(skuSnapshot, "", "plus", "PH")
	item := &orderdomain.OrderItem{
		OrderID:         order.ID,
		ProductID:       productID,
		SKUSnapshotJSON: skuSnapshot,
		TitleJSON:       jsonmap.JSON{"zh-CN": "实时发码商品"},
		UnitPrice:       money.FromDecimal(decimal.NewFromInt(10)),
		Quantity:        quantity,
		TotalPrice:      money.FromDecimal(decimal.NewFromInt(10)),
		FulfillmentType: constants.FulfillmentTypeAuto,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("create order item failed: %v", err)
	}
	return order
}

func TestCreateAutoDeliversRealtimeCodesWithoutTouchingStock(t *testing.T) {
	db := setupFulfillmentServiceTestDB(t)
	order := createPaidAutoOrder(t, db, "RT-001", 300, 2)
	// 同商品的库存卡密不得被发出。
	stock := &cardsecretdomain.Secret{ProductID: 300, Secret: "LEAKED-STOCK", Status: cardsecretdomain.StatusAvailable, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(stock).Error; err != nil {
		t.Fatalf("create stock failed: %v", err)
	}

	issuer := &stubRealtimeIssuer{realtimeProduct: 300, codes: []string{"ZC-A", "ZC-B"}}
	svc := New(Options{
		OrderStore:       ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes"),
		FulfillmentStore: fulfillmentgormstore.New(db),
	})
	svc.SetRealtimeIssuer(issuer)

	result, err := svc.CreateAuto(order.ID)
	if err != nil {
		t.Fatalf("CreateAuto: %v", err)
	}
	if result.Payload != "ZC-A\nZC-B" {
		t.Fatalf("payload = %q", result.Payload)
	}
	var secrets []cardsecretdomain.Secret
	db.Where("order_id = ?", order.ID).Find(&secrets)
	if len(secrets) != 2 || secrets[0].Status != cardsecretdomain.StatusUsed {
		t.Fatalf("issued codes must be recorded as used secrets: %+v", secrets)
	}
	var untouched cardsecretdomain.Secret
	db.First(&untouched, stock.ID)
	if untouched.Status != cardsecretdomain.StatusAvailable {
		t.Fatalf("stock secret must stay available, got %s", untouched.Status)
	}
	var saved orderdomain.Order
	db.First(&saved, order.ID)
	if saved.Status != constants.OrderStatusCompleted {
		t.Fatalf("order status = %s", saved.Status)
	}
	if len(issuer.delivered) != 1 || issuer.delivered[0] != order.ID {
		t.Fatalf("MarkDelivered not called: %v", issuer.delivered)
	}
}

func TestCreateAutoKeepsOrderPaidWhenRealtimeIssueFails(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"retryable", cdkissuecontract.ErrIssueRetryable, ErrRealtimeIssueRetryable},
		{"halted", cdkissuecontract.ErrIssueHalted, ErrRealtimeIssueHalted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupFulfillmentServiceTestDB(t)
			order := createPaidAutoOrder(t, db, "RT-FAIL-"+tc.name, 300, 1)
			svc := New(Options{
				OrderStore:       ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes"),
				FulfillmentStore: fulfillmentgormstore.New(db),
			})
			svc.SetRealtimeIssuer(&stubRealtimeIssuer{realtimeProduct: 300, err: tc.err})

			if _, err := svc.CreateAuto(order.ID); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			var saved orderdomain.Order
			db.First(&saved, order.ID)
			if saved.Status != constants.OrderStatusPaid {
				t.Fatalf("order must stay paid, got %s", saved.Status)
			}
			var count int64
			db.Model(&fulfillmentdomain.Fulfillment{}).Count(&count)
			if count != 0 {
				t.Fatalf("no fulfillment should be created")
			}
		})
	}
}

func TestCreateAutoRejectsRealtimeCodeCountMismatch(t *testing.T) {
	db := setupFulfillmentServiceTestDB(t)
	order := createPaidAutoOrder(t, db, "RT-SHORT", 300, 3)
	svc := New(Options{
		OrderStore:       ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes"),
		FulfillmentStore: fulfillmentgormstore.New(db),
	})
	svc.SetRealtimeIssuer(&stubRealtimeIssuer{realtimeProduct: 300, codes: []string{"ZC-A"}})
	if _, err := svc.CreateAuto(order.ID); !errors.Is(err, ErrCardSecretInsufficient) {
		t.Fatalf("want insufficient, got %v", err)
	}
}
