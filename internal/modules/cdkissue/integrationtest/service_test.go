package integrationtest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	cdkissueapp "github.com/dujiao-next/internal/modules/cdkissue/application"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/modules/cdkissue/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const testAppSecretKey = "test-app-secret-key"

type fakeGateway struct {
	mu        sync.Mutex
	issued    map[string][]cdkissuecontract.IssuedCode
	calls     []cdkissuecontract.IssueCommand
	issueErr  error
	shortBy   int
	overview  *cdkissuedomain.Overview
	overErr   error
	nextID    uint
	disabled  []uint
	disableOK bool
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{
		issued:    map[string][]cdkissuecontract.IssuedCode{},
		disableOK: true,
		overview: &cdkissuedomain.Overview{
			Balance: cdkissuedomain.Balance{SpendableBalance: decimal.NewFromInt(500)},
			Plans: []cdkissuedomain.Plan{
				{Key: "plus", Label: "Plus", Purchasable: true, ServiceFeeUSDMinor: 10},
				{Key: "pro_5x", Label: "Pro 5x", Purchasable: true, ServiceFeeUSDMinor: 15},
				{Key: "pro_50x", Label: "Pro 50x", Purchasable: false},
			},
			PaymentRegions: []cdkissuedomain.PaymentRegion{{Country: "PH"}, {Country: "US"}},
		},
	}
}

// Issue 模拟卡台幂等：同一幂等键永远返回同一批码。
func (g *fakeGateway) Issue(_ context.Context, _ cdkissuecontract.Credentials, command cdkissuecontract.IssueCommand) (*cdkissuecontract.IssueResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, command)
	if g.issueErr != nil {
		return nil, g.issueErr
	}
	if codes, ok := g.issued[command.IdempotencyKey]; ok {
		return &cdkissuecontract.IssueResult{Codes: codes, Replayed: true}, nil
	}
	codes := make([]cdkissuecontract.IssuedCode, 0, command.Count)
	for i := 0; i < command.Count-g.shortBy; i++ {
		g.nextID++
		codes = append(codes, cdkissuecontract.IssuedCode{ID: g.nextID, Code: fmt.Sprintf("ZC-%s-%d", command.Plan, g.nextID), Plan: command.Plan, FeeAmountMinor: 10})
	}
	g.issued[command.IdempotencyKey] = codes
	return &cdkissuecontract.IssueResult{Codes: codes}, nil
}

func (g *fakeGateway) Overview(context.Context, cdkissuecontract.Credentials) (*cdkissuedomain.Overview, error) {
	return g.overview, g.overErr
}

func (g *fakeGateway) DisableCDKs(_ context.Context, _ cdkissuecontract.Credentials, ids []uint) (*cdkissuecontract.DisableResult, error) {
	g.disabled = append(g.disabled, ids...)
	if !g.disableOK {
		return &cdkissuecontract.DisableResult{Failed: ids}, nil
	}
	return &cdkissuecontract.DisableResult{Disabled: ids}, nil
}

type fakeProducts struct {
	products map[uint]*cdkissuecontract.Product
}

func (f fakeProducts) GetProduct(id uint) (*cdkissuecontract.Product, error) {
	return f.products[id], nil
}

type fakeQueue struct {
	enabled bool
	orders  []uint
}

func (q *fakeQueue) Enabled() bool { return q.enabled }

func (q *fakeQueue) EnqueueAutoFulfill(orderID uint) error {
	q.orders = append(q.orders, orderID)
	return nil
}

type fakeAlerts struct {
	issue      []string
	lowBalance int
	refund     []string
}

func (a *fakeAlerts) NotifyIssueAlert(request *cdkissuedomain.IssueRequest, _ string) error {
	a.issue = append(a.issue, request.Status)
	return nil
}

func (a *fakeAlerts) NotifyLowBalance(decimal.Decimal, decimal.Decimal) error {
	a.lowBalance++
	return nil
}

func (a *fakeAlerts) NotifyRefundDisableFailed(orderNo string, _ int, _ string) error {
	a.refund = append(a.refund, orderNo)
	return nil
}

type fixture struct {
	db      *gorm.DB
	store   *gormstore.Store
	gateway *fakeGateway
	queue   *fakeQueue
	alerts  *fakeAlerts
	service *cdkissueapp.Service
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := fmt.Sprintf("file:cdkissue_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&cdkissuedomain.Config{}, &cdkissuedomain.Binding{}, &cdkissuedomain.IssueRequest{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	f := &fixture{
		db:      db,
		store:   gormstore.New(db),
		gateway: newFakeGateway(),
		queue:   &fakeQueue{enabled: true},
		alerts:  &fakeAlerts{},
		now:     time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
	}
	f.service = cdkissueapp.NewService(cdkissueapp.Options{
		Store:    f.store,
		Gateways: map[string]cdkissuecontract.Gateway{cdkissuedomain.ProviderZovoCard: f.gateway},
		Products: fakeProducts{products: map[uint]*cdkissuecontract.Product{
			1: {ID: 1, FulfillmentType: "auto", SKUs: []cdkissuecontract.ProductSKU{{ID: 10, Code: "PLUS", IsActive: true}, {ID: 11, Code: "PRO", IsActive: true}}},
			2: {ID: 2, FulfillmentType: "manual"},
		}},
		Fulfillments: f.queue,
		Alerts:       f.alerts,
		AppSecretKey: testAppSecretKey,
		Now:          func() time.Time { return f.now },
	})
	return f
}

func ptr[T any](value T) *T { return &value }

func (f *fixture) configure(t *testing.T) {
	t.Helper()
	if _, err := f.service.UpdateConfig("", cdkissueapp.ConfigPatch{
		AppID:     ptr("ak_test"),
		AppSecret: ptr("sk_live_secret_a1b2"),
		Enabled:   ptr(true),
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
}

func (f *fixture) bind(t *testing.T) {
	t.Helper()
	if _, err := f.service.SaveBinding(context.Background(), 1, cdkissueapp.BindingInput{
		Enabled: true,
		Plan:    "plus",
		SKUs:    []cdkissueapp.SKUBindingView{{SKUID: 11, Plan: "pro_5x", PaymentCountry: "US"}},
	}); err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}
}

// realtimeItem 模拟下单时固化了实时发码快照的订单项。
func realtimeItem(itemID, skuID uint, quantity int, plan, country string) cdkissuecontract.IssueOrderItem {
	snapshot := jsonmap.JSON{"sku_id": skuID}
	cdkissuedomain.ApplyItemSnapshot(snapshot, "", plan, country)
	return cdkissuecontract.NewIssueOrderItem(itemID, 1, skuID, quantity, jsonmap.JSON{"zh-CN": "测试商品"}, snapshot)
}

func order(quantity int) cdkissuecontract.IssueOrder {
	return cdkissuecontract.IssueOrder{
		OrderID: 100,
		OrderNo: "DJ100",
		Items:   []cdkissuecontract.IssueOrderItem{realtimeItem(500, 10, quantity, "plus", "PH")},
	}
}

func TestConfigMasksSecretAndRequiresCredentialsToEnable(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.UpdateConfig("", cdkissueapp.ConfigPatch{Enabled: ptr(true)}); !errors.Is(err, cdkissuecontract.ErrConfigInvalid) {
		t.Fatalf("enable without credentials should fail, got %v", err)
	}
	f.configure(t)
	view, err := f.service.GetConfig("")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !view.Active || !view.HasSecret || view.SecretHint != "sk_****a1b2" || view.Environment != cdkissuedomain.EnvironmentSandbox {
		t.Fatalf("unexpected view %+v", view)
	}
	var stored cdkissuedomain.Config
	f.db.First(&stored)
	if stored.AppSecretEncrypted == "" || stored.AppSecretEncrypted == "sk_live_secret_a1b2" {
		t.Fatalf("secret must be stored encrypted")
	}
	// 留空 secret 保存不应清掉原值。
	if _, err := f.service.UpdateConfig("", cdkissueapp.ConfigPatch{AppSecret: ptr("")}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	view, _ = f.service.GetConfig("")
	if !view.HasSecret {
		t.Fatalf("empty secret must keep the stored one")
	}
}

func TestSaveBindingValidatesAgainstCardPlatform(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.service.SaveBinding(ctx, 1, cdkissueapp.BindingInput{Enabled: true, Plan: "plus"}); !errors.Is(err, cdkissuecontract.ErrBindingInvalid) {
		t.Fatalf("binding before config should fail, got %v", err)
	}
	f.configure(t)
	if _, err := f.service.SaveBinding(ctx, 2, cdkissueapp.BindingInput{Enabled: true, Plan: "plus"}); !errors.Is(err, cdkissuecontract.ErrBindingInvalid) {
		t.Fatalf("manual product must be rejected, got %v", err)
	}
	if _, err := f.service.SaveBinding(ctx, 1, cdkissueapp.BindingInput{Enabled: true, Plan: "pro_50x"}); !errors.Is(err, cdkissuecontract.ErrBindingInvalid) {
		t.Fatalf("unsellable plan must be rejected, got %v", err)
	}
	if _, err := f.service.SaveBinding(ctx, 1, cdkissueapp.BindingInput{Enabled: true, Plan: "plus", PaymentCountry: "JP"}); !errors.Is(err, cdkissuecontract.ErrBindingInvalid) {
		t.Fatalf("unsupported region must be rejected, got %v", err)
	}
	if _, err := f.service.SaveBinding(ctx, 1, cdkissueapp.BindingInput{Enabled: true, SKUs: []cdkissueapp.SKUBindingView{{SKUID: 10, Plan: "plus"}}}); !errors.Is(err, cdkissuecontract.ErrBindingInvalid) {
		t.Fatalf("sku without any plan must be rejected, got %v", err)
	}
	f.bind(t)
	view, err := f.service.GetBinding(1)
	if err != nil || !view.Enabled || view.Plan != "plus" || len(view.SKUs) != 1 || view.SKUs[0].PaymentCountry != "US" {
		t.Fatalf("unexpected binding %+v err=%v", view, err)
	}
	stock, _ := f.service.RealtimeStock([]uint{1, 2})
	if stock[1] != -1 {
		t.Fatalf("realtime product should show unlimited, got %v", stock)
	}
	if _, ok := stock[2]; ok {
		t.Fatalf("unbound product must not be overridden")
	}
}

func TestEnsureIssuedIsIdempotentAndUsesSKUPlan(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	ctx := context.Background()
	o := order(2)
	o.Items = append(o.Items, realtimeItem(501, 11, 1, "pro_5x", "US"))

	codes, err := f.service.EnsureIssued(ctx, o)
	if err != nil {
		t.Fatalf("EnsureIssued: %v", err)
	}
	if len(codes[500]) != 2 || len(codes[501]) != 1 {
		t.Fatalf("codes = %v", codes)
	}
	if f.gateway.calls[1].Plan != "pro_5x" || f.gateway.calls[1].PaymentCountry != "US" || f.gateway.calls[0].PaymentCountry != "PH" {
		t.Fatalf("calls = %+v", f.gateway.calls)
	}
	if f.gateway.calls[0].IdempotencyKey != "dj-DJ100-500-0" {
		t.Fatalf("idempotency key = %s", f.gateway.calls[0].IdempotencyKey)
	}

	again, err := f.service.EnsureIssued(ctx, o)
	if err != nil {
		t.Fatalf("second EnsureIssued: %v", err)
	}
	if len(f.gateway.calls) != 2 {
		t.Fatalf("succeeded requests must not call the gateway again, calls=%d", len(f.gateway.calls))
	}
	if again[500][0] != codes[500][0] {
		t.Fatalf("stored codes differ")
	}

	if err := f.service.MarkDelivered(o.OrderID); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	var rows []cdkissuedomain.IssueRequest
	f.db.Find(&rows)
	for _, row := range rows {
		if row.CodesEncrypted != "" || row.DeliveredAt == nil {
			t.Fatalf("delivered request must drop encrypted codes: %+v", row)
		}
	}
}

func TestEnsureIssuedSplitsLargeQuantities(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	codes, err := f.service.EnsureIssued(context.Background(), order(250))
	if err != nil {
		t.Fatalf("EnsureIssued: %v", err)
	}
	if len(codes[500]) != 250 || len(f.gateway.calls) != 2 || f.gateway.calls[0].Count != 200 || f.gateway.calls[1].Count != 50 {
		t.Fatalf("unexpected split: codes=%d calls=%+v", len(codes[500]), f.gateway.calls)
	}
}

func TestTransientErrorsRetryWithSameKeyThenBecomeUnknown(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	f.gateway.issueErr = &cdkissuecontract.GatewayError{HTTPStatus: 503, ErrorCode: "channel_unavailable", Transient: true}
	ctx := context.Background()

	for i := 1; i < cdkissuedomain.MaxAutoAttempts; i++ {
		if _, err := f.service.EnsureIssued(ctx, order(1)); !errors.Is(err, cdkissuecontract.ErrIssueRetryable) {
			t.Fatalf("attempt %d: want retryable, got %v", i, err)
		}
	}
	if _, err := f.service.EnsureIssued(ctx, order(1)); !errors.Is(err, cdkissuecontract.ErrIssueHalted) {
		t.Fatalf("final attempt: want halted, got %v", err)
	}
	for _, call := range f.gateway.calls {
		if call.IdempotencyKey != "dj-DJ100-500-0" {
			t.Fatalf("transient retries must reuse the key, got %s", call.IdempotencyKey)
		}
	}
	views, _ := f.service.ListOrderRequests(100)
	if views[0].Status != cdkissuedomain.RequestStatusUnknown || views[0].RetryMode != string(cdkissuedomain.RetrySameKey) {
		t.Fatalf("unexpected view %+v", views[0])
	}
	if len(f.alerts.issue) != 1 || f.alerts.issue[0] != cdkissuedomain.RequestStatusUnknown {
		t.Fatalf("alerts = %v", f.alerts.issue)
	}
	// halted 后再次调用不会再请求卡台。
	calls := len(f.gateway.calls)
	_, _ = f.service.EnsureIssued(ctx, order(1))
	if len(f.gateway.calls) != calls {
		t.Fatalf("unknown request must not auto-retry")
	}

	// 不明状态只允许原键重试。
	if _, err := f.service.Retry(views[0].ID, cdkissuedomain.RetryNewKey); !errors.Is(err, cdkissuecontract.ErrRetryNotAllowed) {
		t.Fatalf("new key retry on unknown must be refused, got %v", err)
	}
	f.gateway.issueErr = nil
	view, err := f.service.Retry(views[0].ID, cdkissuedomain.RetrySameKey)
	if err != nil || view.IdempotencyKey != "dj-DJ100-500-0" || len(f.queue.orders) != 1 {
		t.Fatalf("same key retry: view=%+v err=%v queued=%v", view, err, f.queue.orders)
	}
	if _, err := f.service.EnsureIssued(ctx, order(1)); err != nil {
		t.Fatalf("EnsureIssued after retry: %v", err)
	}
}

func TestFinalErrorFailsAndAllowsNewKeyRetry(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	f.gateway.issueErr = &cdkissuecontract.GatewayError{HTTPStatus: 400, ErrorCode: "insufficient_balance", Transient: false}
	if _, err := f.service.EnsureIssued(context.Background(), order(1)); !errors.Is(err, cdkissuecontract.ErrIssueHalted) {
		t.Fatalf("want halted, got %v", err)
	}
	views, _ := f.service.ListOrderRequests(100)
	if views[0].Status != cdkissuedomain.RequestStatusFailed || views[0].LastErrorCode != "insufficient_balance" || views[0].RetryMode != string(cdkissuedomain.RetryNewKey) {
		t.Fatalf("unexpected view %+v", views[0])
	}
	if _, err := f.service.Retry(views[0].ID, cdkissuedomain.RetrySameKey); !errors.Is(err, cdkissuecontract.ErrRetryNotAllowed) {
		t.Fatalf("same key retry on failed must be refused, got %v", err)
	}
	f.gateway.issueErr = nil
	view, err := f.service.Retry(views[0].ID, cdkissuedomain.RetryNewKey)
	if err != nil || view.IdempotencyKey != "dj-DJ100-500-0-r1" || view.KeyVersion != 1 {
		t.Fatalf("new key retry: view=%+v err=%v", view, err)
	}
	if _, err := f.service.EnsureIssued(context.Background(), order(1)); err != nil {
		t.Fatalf("EnsureIssued after retry: %v", err)
	}
	if last := f.gateway.calls[len(f.gateway.calls)-1]; last.IdempotencyKey != "dj-DJ100-500-0-r1" {
		t.Fatalf("retry must use the new key, got %s", last.IdempotencyKey)
	}
}

func TestRetryRequiresQueue(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	f.gateway.issueErr = &cdkissuecontract.GatewayError{HTTPStatus: 400, Transient: false}
	_, _ = f.service.EnsureIssued(context.Background(), order(1))
	f.queue.enabled = false
	views, _ := f.service.ListOrderRequests(100)
	if _, err := f.service.Retry(views[0].ID, cdkissuedomain.RetryNewKey); !errors.Is(err, cdkissuecontract.ErrQueueUnavailable) {
		t.Fatalf("want queue unavailable, got %v", err)
	}
}

func TestCountMismatchIsUnknown(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	f.gateway.shortBy = 1
	if _, err := f.service.EnsureIssued(context.Background(), order(3)); !errors.Is(err, cdkissuecontract.ErrIssueHalted) {
		t.Fatalf("want halted, got %v", err)
	}
	views, _ := f.service.ListOrderRequests(100)
	if views[0].Status != cdkissuedomain.RequestStatusUnknown || views[0].LastErrorCode != "count_mismatch" {
		t.Fatalf("unexpected view %+v", views[0])
	}
}

func TestMissingConfigFailsWithoutCallingGateway(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	f.db.Model(&cdkissuedomain.Config{}).Where("1 = 1").Update("app_secret_encrypted", "")
	if _, err := f.service.EnsureIssued(context.Background(), order(1)); !errors.Is(err, cdkissuecontract.ErrIssueHalted) {
		t.Fatalf("want halted, got %v", err)
	}
	if len(f.gateway.calls) != 0 {
		t.Fatalf("gateway must not be called without credentials")
	}
	views, _ := f.service.ListOrderRequests(100)
	if views[0].Status != cdkissuedomain.RequestStatusFailed {
		t.Fatalf("status = %s", views[0].Status)
	}
}

func TestCheckOrderable(t *testing.T) {
	f := newFixture(t)
	if snapshot, err := f.service.CheckOrderable(1, 10, 1); snapshot != nil || err != nil {
		t.Fatalf("unbound product: snapshot=%v err=%v", snapshot, err)
	}
	f.configure(t)
	f.bind(t)
	if snapshot, err := f.service.CheckOrderable(1, 10, 1); snapshot == nil || err != nil || snapshot.Plan != "plus" || snapshot.PaymentCountry != "PH" {
		t.Fatalf("bound product: snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot, _ := f.service.CheckOrderable(1, 11, 1); snapshot == nil || snapshot.Plan != "pro_5x" || snapshot.PaymentCountry != "US" {
		t.Fatalf("sku override: snapshot=%+v", snapshot)
	}

	f.gateway.overview.Balance.SpendableBalance = decimal.NewFromInt(10)
	f.now = f.now.Add(2 * time.Minute) // 让概览缓存过期
	if _, err := f.service.CheckOrderable(1, 10, 1); !errors.Is(err, cdkissuecontract.ErrNotOrderable) {
		t.Fatalf("low balance must block orders, got %v", err)
	}
	if f.alerts.lowBalance != 1 {
		t.Fatalf("low balance alert not sent")
	}

	f.gateway.overErr = &cdkissuecontract.GatewayError{HTTPStatus: 503, Transient: true}
	f.now = f.now.Add(2 * time.Minute)
	if snapshot, err := f.service.CheckOrderable(1, 10, 1); snapshot == nil || err != nil {
		t.Fatalf("unreachable card platform should not block orders: snapshot=%v err=%v", snapshot, err)
	}

	if _, err := f.service.UpdateConfig("", cdkissueapp.ConfigPatch{Enabled: ptr(false)}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if _, err := f.service.CheckOrderable(1, 10, 1); !errors.Is(err, cdkissuecontract.ErrNotOrderable) {
		t.Fatalf("disabled config must block orders, got %v", err)
	}
	stock, _ := f.service.RealtimeStock([]uint{1})
	if stock[1] != 0 {
		t.Fatalf("disabled config should show sold out, got %v", stock)
	}
}

func TestDeliveryState(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	paidAt := f.now
	if state, _ := f.service.DeliveryState(order(1), &paidAt); state != cdkissueapp.DeliveryStateIssuing {
		t.Fatalf("fresh order state = %q", state)
	}
	f.now = f.now.Add(3 * time.Minute)
	if state, _ := f.service.DeliveryState(order(1), &paidAt); state != cdkissueapp.DeliveryStateDelayed {
		t.Fatalf("slow order state = %q", state)
	}
	f.now = paidAt
	f.gateway.issueErr = &cdkissuecontract.GatewayError{HTTPStatus: 400, Transient: false}
	_, _ = f.service.EnsureIssued(context.Background(), order(1))
	if state, _ := f.service.DeliveryState(order(1), &paidAt); state != cdkissueapp.DeliveryStateDelayed {
		t.Fatalf("failed order state = %q", state)
	}
	unbound := cdkissuecontract.IssueOrder{OrderID: 200, Items: []cdkissuecontract.IssueOrderItem{cdkissuecontract.NewIssueOrderItem(900, 2, 0, 1, nil, nil)}}
	if state, _ := f.service.DeliveryState(unbound, &paidAt); state != "" {
		t.Fatalf("non realtime order state = %q", state)
	}
}

func TestDisableOrderCodes(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	if _, err := f.service.EnsureIssued(context.Background(), order(2)); err != nil {
		t.Fatalf("EnsureIssued: %v", err)
	}
	result, err := f.service.DisableOrderCodes(context.Background(), 100)
	if err != nil || len(result.Disabled) != 2 {
		t.Fatalf("disable: result=%+v err=%v", result, err)
	}
	views, _ := f.service.ListOrderRequests(100)
	if views[0].CodesDisabledAt == nil || views[0].CanDisableCodes {
		t.Fatalf("request should be marked disabled: %+v", views[0])
	}
	if _, err := f.service.DisableOrderCodes(context.Background(), 100); !errors.Is(err, cdkissuecontract.ErrDisableNotAllowed) {
		t.Fatalf("second disable should be refused, got %v", err)
	}
}

func TestListRequestsAndStats(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	_, _ = f.service.EnsureIssued(context.Background(), order(1))
	f.gateway.issueErr = &cdkissuecontract.GatewayError{HTTPStatus: 400, Transient: false}
	other := order(1)
	other.OrderID, other.OrderNo = 101, "DJ101"
	_, _ = f.service.EnsureIssued(context.Background(), other)

	stats, err := f.service.Stats(cdkissuecontract.RequestFilter{Status: "failed"})
	if err != nil || stats.Total != 2 || stats.Succeeded != 1 || stats.Failed != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	rows, total, err := f.service.ListRequests(cdkissuecontract.RequestFilter{Status: "failed", Page: 1, PageSize: 20})
	if err != nil || total != 1 || rows[0].OrderNo != "DJ101" {
		t.Fatalf("rows=%+v total=%d err=%v", rows, total, err)
	}
	rows, total, _ = f.service.ListRequests(cdkissuecontract.RequestFilter{OrderNo: "DJ100", Page: 1, PageSize: 20})
	if total != 1 || rows[0].OrderID != 100 {
		t.Fatalf("order filter rows=%+v", rows)
	}
}

// 付款后改动或关闭商品绑定，不影响已下单订单：仍按下单时的套餐发码，也不会回落到库存。
func TestIssuingUsesOrderSnapshotNotCurrentBinding(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	if _, err := f.service.SaveBinding(context.Background(), 1, cdkissueapp.BindingInput{Enabled: false, Plan: "pro_5x"}); err != nil {
		t.Fatalf("disable binding: %v", err)
	}
	codes, err := f.service.EnsureIssued(context.Background(), order(1))
	if err != nil || len(codes[500]) != 1 {
		t.Fatalf("snapshot order must still be issued: codes=%v err=%v", codes, err)
	}
	if f.gateway.calls[0].Plan != "plus" {
		t.Fatalf("must use the plan captured at checkout, got %s", f.gateway.calls[0].Plan)
	}

	plain := cdkissuecontract.IssueOrder{OrderID: 300, OrderNo: "DJ300", Items: []cdkissuecontract.IssueOrderItem{
		cdkissuecontract.NewIssueOrderItem(700, 1, 10, 1, nil, jsonmap.JSON{"sku_id": 10}),
	}}
	f.bind(t) // 重新启用绑定后，下单时未走实时发码的订单仍然不走
	items, err := f.service.RealtimeItems(plain)
	if err != nil || len(items) != 0 {
		t.Fatalf("order without snapshot must not be realtime: %v %v", items, err)
	}
}

// 另一个任务持有占用时，不得再次调用卡台；占用结束后由持有方写入结果。
func TestClaimPreventsConcurrentGatewayCalls(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	f.gateway.issueErr = &cdkissuecontract.GatewayError{HTTPStatus: 503, Transient: true}
	if _, err := f.service.EnsureIssued(context.Background(), order(1)); !errors.Is(err, cdkissuecontract.ErrIssueRetryable) {
		t.Fatalf("want retryable, got %v", err)
	}
	views, _ := f.service.ListOrderRequests(100)
	// 模拟另一个任务正在调用卡台。
	until := f.now.Add(time.Minute)
	f.db.Model(&cdkissuedomain.IssueRequest{}).Where("id = ?", views[0].ID).Update("claimed_until", until)

	f.gateway.issueErr = nil
	calls := len(f.gateway.calls)
	if _, err := f.service.EnsureIssued(context.Background(), order(1)); !errors.Is(err, cdkissuecontract.ErrIssueRetryable) {
		t.Fatalf("claimed request must be retried later, got %v", err)
	}
	if len(f.gateway.calls) != calls {
		t.Fatalf("gateway must not be called while another worker holds the claim")
	}

	f.now = f.now.Add(2 * time.Minute) // 占用过期
	codes, err := f.service.EnsureIssued(context.Background(), order(1))
	if err != nil || len(codes[500]) != 1 {
		t.Fatalf("expired claim must be taken over: codes=%v err=%v", codes, err)
	}
}

// 已成功的请求不会被迟到的失败结果覆盖。
func TestSucceededRequestIsNotOverwritten(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	if _, err := f.service.EnsureIssued(context.Background(), order(1)); err != nil {
		t.Fatalf("EnsureIssued: %v", err)
	}
	var stored cdkissuedomain.IssueRequest
	f.db.First(&stored)
	stale := stored
	stale.Status = cdkissuedomain.RequestStatusFailed
	saved, err := f.store.UpdateRequestIfStatus(&stale, cdkissuedomain.RequestStatusPending)
	if err != nil || saved {
		t.Fatalf("stale pending write must not apply: saved=%v err=%v", saved, err)
	}
	f.db.First(&stored, stored.ID)
	if stored.Status != cdkissuedomain.RequestStatusSucceeded {
		t.Fatalf("status = %s", stored.Status)
	}
}

func TestRetryRejectsConcurrentDoubleClick(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	f.gateway.issueErr = &cdkissuecontract.GatewayError{HTTPStatus: 400, Transient: false}
	_, _ = f.service.EnsureIssued(context.Background(), order(1))
	views, _ := f.service.ListOrderRequests(100)
	if _, err := f.service.Retry(views[0].ID, cdkissuedomain.RetryNewKey); err != nil {
		t.Fatalf("first retry: %v", err)
	}
	if _, err := f.service.Retry(views[0].ID, cdkissuedomain.RetryNewKey); !errors.Is(err, cdkissuecontract.ErrRetryNotAllowed) {
		t.Fatalf("second retry must be refused, got %v", err)
	}
	if len(f.queue.orders) != 1 {
		t.Fatalf("only one fulfillment must be enqueued, got %v", f.queue.orders)
	}
}

func TestHandleOrderRefundedDisablesCodesAndAlertsOnRedeemed(t *testing.T) {
	f := newFixture(t)
	f.configure(t)
	f.bind(t)
	if _, err := f.service.EnsureIssued(context.Background(), order(2)); err != nil {
		t.Fatalf("EnsureIssued: %v", err)
	}
	f.gateway.disableOK = false
	if err := f.service.HandleOrderRefunded(context.Background(), 100, "DJ100"); err != nil {
		t.Fatalf("HandleOrderRefunded: %v", err)
	}
	if len(f.alerts.refund) != 1 || f.alerts.refund[0] != "DJ100" {
		t.Fatalf("redeemed codes after refund must alert, got %v", f.alerts.refund)
	}
	f.gateway.disableOK = true
	if err := f.service.HandleOrderRefunded(context.Background(), 100, "DJ100"); err != nil {
		t.Fatalf("HandleOrderRefunded: %v", err)
	}
	views, _ := f.service.ListOrderRequests(100)
	if views[0].CodesDisabledAt == nil {
		t.Fatalf("codes must be marked disabled")
	}
	// 没有可停用的码时静默返回。
	if err := f.service.HandleOrderRefunded(context.Background(), 999, "DJ999"); err != nil {
		t.Fatalf("order without codes: %v", err)
	}
}
