package integrationtest

import (
	"testing"
	"time"

	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// 最小化的订单表结构，只包含补发扫描读取的列。
type sweepOrder struct {
	ID        uint
	Status    string
	PaidAt    *time.Time
	DeletedAt *time.Time
}

func (sweepOrder) TableName() string { return "orders" }

type sweepOrderItem struct {
	ID              uint
	OrderID         uint
	FulfillmentType string
	SKUSnapshot     jsonmap.JSON `gorm:"column:sku_snapshot;type:json"`
	DeletedAt       *time.Time
}

func (sweepOrderItem) TableName() string { return "order_items" }

type sweepFulfillment struct {
	ID        uint
	OrderID   uint
	DeletedAt *time.Time
}

func (sweepFulfillment) TableName() string { return "fulfillments" }

func TestSweepStuckOrdersRequeuesOnlyEligibleRealtimeOrders(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	if err := f.db.AutoMigrate(&sweepOrder{}, &sweepOrderItem{}, &sweepFulfillment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	realtime := jsonmap.JSON{}
	cdkissuedomain.ApplyItemSnapshot(realtime, "plus", "PH")
	stale := f.now.Add(-10 * time.Minute)
	fresh := f.now.Add(-time.Minute)
	ancient := f.now.Add(-48 * time.Hour)

	seed := []struct {
		id        uint
		paidAt    time.Time
		snapshot  jsonmap.JSON
		fulfilled bool
		status    string
	}{
		{1, stale, realtime, false, "paid"},       // 应补发
		{2, fresh, realtime, false, "paid"},       // 太新，交给正常任务
		{3, ancient, realtime, false, "paid"},     // 太旧，交给管理员
		{4, stale, jsonmap.JSON{}, false, "paid"}, // 库存卡密商品
		{5, stale, realtime, true, "paid"},        // 已交付
		{6, stale, realtime, false, "completed"},
		{7, stale, realtime, false, "paid"}, // 已有失败记录
	}
	for _, s := range seed {
		paidAt := s.paidAt
		f.db.Create(&sweepOrder{ID: s.id, Status: s.status, PaidAt: &paidAt})
		f.db.Create(&sweepOrderItem{OrderID: s.id, FulfillmentType: "auto", SKUSnapshot: s.snapshot})
		if s.fulfilled {
			f.db.Create(&sweepFulfillment{OrderID: s.id})
		}
	}
	f.db.Create(&cdkissuedomain.IssueRequest{
		RootOrderID: 7, OrderID: 7, OrderNo: "DJ7", OrderItemID: 7, Plan: "plus", Count: 1,
		IdempotencyKey: "dj-DJ7-7-0", Status: cdkissuedomain.RequestStatusFailed,
	})

	result, err := f.service.SweepStuckOrders()
	if err != nil {
		t.Fatalf("SweepStuckOrders: %v", err)
	}
	if result.Enqueued != 1 || len(f.queue.orders) != 1 || f.queue.orders[0] != 1 {
		t.Fatalf("result=%+v queued=%v", result, f.queue.orders)
	}

	f.queue.enabled = false
	if _, err := f.service.SweepStuckOrders(); err != cdkissuecontract.ErrQueueUnavailable {
		t.Fatalf("want queue unavailable, got %v", err)
	}
}
