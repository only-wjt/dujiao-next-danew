package integrationtest

import (
	"context"
	"errors"
	"testing"
	"time"

	cdkissueapp "github.com/dujiao-next/internal/modules/cdkissue/application"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// 渠道切换：danew_cdk 与 ZovoCard 同时注册，同一时刻只启用一个，在途订单按下单快照的渠道发码。

type providerFixture struct {
	*fixture
	danew *fakeGateway
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	f := newFixture(t)
	danew := newFakeGateway()
	f.service = cdkissueapp.NewService(cdkissueapp.Options{
		Store: f.store,
		Gateways: map[string]cdkissuecontract.Gateway{
			cdkissuedomain.ProviderZovoCard: f.gateway,
			cdkissuedomain.ProviderDanewCDK: danew,
		},
		Products: fakeProducts{products: map[uint]*cdkissuecontract.Product{
			1: {ID: 1, FulfillmentType: "auto", SKUs: []cdkissuecontract.ProductSKU{{ID: 10, Code: "PLUS", IsActive: true}, {ID: 11, Code: "PRO", IsActive: true}}},
		}},
		Fulfillments: f.queue,
		Alerts:       f.alerts,
		AppSecretKey: testAppSecretKey,
		Now:          func() time.Time { return f.now },
	})
	return &providerFixture{fixture: f, danew: danew}
}

func (f *providerFixture) configureDanew(t *testing.T, enabled bool) {
	t.Helper()
	if _, err := f.service.UpdateConfig(cdkissuedomain.ProviderDanewCDK, cdkissueapp.ConfigPatch{
		APIBaseURL: ptr("https://cdk.example.com/api/v1/cardnet/"),
		AppSecret:  ptr("cn_live_secret_c3d4"),
		Enabled:    ptr(enabled),
	}); err != nil {
		t.Fatalf("UpdateConfig danew: %v", err)
	}
}

func providerItem(itemID, skuID uint, quantity int, provider, plan, country string) cdkissuecontract.IssueOrderItem {
	snapshot := jsonmap.JSON{"sku_id": skuID}
	cdkissuedomain.ApplyItemSnapshot(snapshot, provider, plan, country)
	return cdkissuecontract.NewIssueOrderItem(itemID, 1, skuID, quantity, jsonmap.JSON{"zh-CN": "测试商品"}, snapshot)
}

func TestDanewConfigValidation(t *testing.T) {
	f := newProviderFixture(t)
	cases := []struct {
		name  string
		patch cdkissueapp.ConfigPatch
	}{
		{"http url", cdkissueapp.ConfigPatch{APIBaseURL: ptr("http://cdk.example.com/api/v1/cardnet")}},
		{"ip url", cdkissueapp.ConfigPatch{APIBaseURL: ptr("https://10.0.0.1/api/v1/cardnet")}},
		{"localhost", cdkissueapp.ConfigPatch{APIBaseURL: ptr("https://localhost/api")}},
		{"query", cdkissueapp.ConfigPatch{APIBaseURL: ptr("https://cdk.example.com/api?x=1")}},
		{"zovocard secret", cdkissueapp.ConfigPatch{AppSecret: ptr("sk_live_x")}},
		{"enable without url", cdkissueapp.ConfigPatch{AppSecret: ptr("cn_live_x"), Enabled: ptr(true)}},
	}
	for _, tc := range cases {
		if _, err := f.service.UpdateConfig(cdkissuedomain.ProviderDanewCDK, tc.patch); !errors.Is(err, cdkissuecontract.ErrConfigInvalid) {
			t.Errorf("%s: want ErrConfigInvalid, got %v", tc.name, err)
		}
	}
	if _, err := f.service.UpdateConfig("unknown", cdkissueapp.ConfigPatch{}); !errors.Is(err, cdkissuecontract.ErrInvalidInput) {
		t.Fatalf("unknown provider: %v", err)
	}

	f.configureDanew(t, true)
	view, err := f.service.GetConfig(cdkissuedomain.ProviderDanewCDK)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !view.Active || view.APIBaseURL != "https://cdk.example.com/api/v1/cardnet" ||
		view.SecretHint != "cn_live_****c3d4" || view.Environment != cdkissuedomain.EnvironmentProduction {
		t.Fatalf("unexpected view %+v", view)
	}
}

func TestEnablingOneProviderDisablesTheOther(t *testing.T) {
	f := newProviderFixture(t)
	f.configure(t) // ZovoCard 启用
	f.configureDanew(t, true)

	configs, err := f.service.ListConfigs()
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if configs.ActiveProvider != cdkissuedomain.ProviderDanewCDK || len(configs.Providers) != 2 {
		t.Fatalf("unexpected configs %+v", configs)
	}
	zovo, _ := f.service.GetConfig(cdkissuedomain.ProviderZovoCard)
	if zovo.Enabled {
		t.Fatalf("enabling danew_cdk must disable zovocard")
	}

	// 切回 ZovoCard：danew_cdk 自动停用，密钥保留。
	if _, err := f.service.UpdateConfig(cdkissuedomain.ProviderZovoCard, cdkissueapp.ConfigPatch{Enabled: ptr(true)}); err != nil {
		t.Fatalf("re-enable zovocard: %v", err)
	}
	danew, _ := f.service.GetConfig(cdkissuedomain.ProviderDanewCDK)
	if danew.Enabled || !danew.HasSecret {
		t.Fatalf("unexpected danew view %+v", danew)
	}
}

func TestCheckOrderableSnapshotsActiveProvider(t *testing.T) {
	f := newProviderFixture(t)
	f.configure(t)
	f.bind(t)

	snapshot, err := f.service.CheckOrderable(1, 10, 1)
	if err != nil || snapshot == nil || snapshot.Provider != cdkissuedomain.ProviderZovoCard {
		t.Fatalf("zovocard snapshot %+v err=%v", snapshot, err)
	}

	f.configureDanew(t, true)
	f.danew.overview.Client = &cdkissuedomain.ClientLimits{AllowedPlans: []string{"plus"}, AllowedCountries: []string{"PH"}}
	snapshot, err = f.service.CheckOrderable(1, 10, 1)
	if err != nil || snapshot == nil || snapshot.Provider != cdkissuedomain.ProviderDanewCDK || snapshot.PaymentCountry != "PH" {
		t.Fatalf("danew snapshot %+v err=%v", snapshot, err)
	}
	// SKU 11 绑定 pro_5x / US，不在 danew_cdk 客户端授权范围内：下单前拦截。
	if _, err := f.service.CheckOrderable(1, 11, 1); !errors.Is(err, cdkissuecontract.ErrNotOrderable) {
		t.Fatalf("unauthorized plan must not be orderable, got %v", err)
	}
	// 主台不可发码：拦截。
	f.danew.overview.Client = nil
	no := false
	f.danew.overview.Issuable = &no
	_, _ = f.service.Overview(context.Background(), "", true) // 刷新缓存
	if _, err := f.service.CheckOrderable(1, 10, 1); !errors.Is(err, cdkissuecontract.ErrNotOrderable) {
		t.Fatalf("not issuable must not be orderable, got %v", err)
	}
}

func TestEnsureIssuedRoutesBySnapshotProvider(t *testing.T) {
	f := newProviderFixture(t)
	f.configure(t)
	f.configureDanew(t, true) // 当前启用 danew_cdk，ZovoCard 已停用但凭据仍在

	o := cdkissuecontract.IssueOrder{
		OrderID: 100,
		OrderNo: "DJ100",
		Items: []cdkissuecontract.IssueOrderItem{
			providerItem(500, 10, 2, cdkissuedomain.ProviderDanewCDK, "plus", "PH"),
			// 旧快照没有渠道：仍按 ZovoCard 发码。
			realtimeItem(501, 11, 1, "pro_5x", "US"),
		},
	}
	codes, err := f.service.EnsureIssued(context.Background(), o)
	if err != nil {
		t.Fatalf("EnsureIssued: %v", err)
	}
	if len(codes[500]) != 2 || len(codes[501]) != 1 {
		t.Fatalf("codes = %v", codes)
	}
	if len(f.danew.calls) != 1 || f.danew.calls[0].Plan != "plus" || f.danew.calls[0].OrderNo != "DJ100" ||
		f.danew.calls[0].ExternalRef != "item:500:chunk:0" {
		t.Fatalf("danew calls = %+v", f.danew.calls)
	}
	if len(f.gateway.calls) != 1 || f.gateway.calls[0].Plan != "pro_5x" {
		t.Fatalf("zovocard calls = %+v", f.gateway.calls)
	}
	var rows []cdkissuedomain.IssueRequest
	f.db.Order("order_item_id").Find(&rows)
	if rows[0].Provider != cdkissuedomain.ProviderDanewCDK || rows[1].Provider != cdkissuedomain.ProviderZovoCard {
		t.Fatalf("request providers = %s, %s", rows[0].Provider, rows[1].Provider)
	}
}

func TestEnsureIssuedFailsWhenSnapshotProviderNotConfigured(t *testing.T) {
	f := newProviderFixture(t)
	f.configure(t) // 只配置了 ZovoCard
	o := cdkissuecontract.IssueOrder{
		OrderID: 101,
		OrderNo: "DJ101",
		Items:   []cdkissuecontract.IssueOrderItem{providerItem(600, 10, 1, cdkissuedomain.ProviderDanewCDK, "plus", "PH")},
	}
	if _, err := f.service.EnsureIssued(context.Background(), o); !errors.Is(err, cdkissuecontract.ErrIssueHalted) {
		t.Fatalf("want halted, got %v", err)
	}
	if len(f.danew.calls) != 0 || len(f.gateway.calls) != 0 {
		t.Fatalf("no gateway may be called: danew=%d zovo=%d", len(f.danew.calls), len(f.gateway.calls))
	}
	var row cdkissuedomain.IssueRequest
	f.db.First(&row)
	if row.Status != cdkissuedomain.RequestStatusFailed || row.LastErrorCode != "not_configured" {
		t.Fatalf("unexpected request %+v", row)
	}
}

func TestDisableOrderCodesUsesEachRequestProvider(t *testing.T) {
	f := newProviderFixture(t)
	f.configure(t)
	f.configureDanew(t, true)
	o := cdkissuecontract.IssueOrder{
		OrderID: 102,
		OrderNo: "DJ102",
		Items: []cdkissuecontract.IssueOrderItem{
			providerItem(700, 10, 2, cdkissuedomain.ProviderDanewCDK, "plus", "PH"),
			realtimeItem(701, 11, 1, "pro_5x", "US"),
		},
	}
	if _, err := f.service.EnsureIssued(context.Background(), o); err != nil {
		t.Fatalf("EnsureIssued: %v", err)
	}
	result, err := f.service.DisableOrderCodes(context.Background(), 102)
	if err != nil {
		t.Fatalf("DisableOrderCodes: %v", err)
	}
	if len(f.danew.disabled) != 2 || len(f.gateway.disabled) != 1 || len(result.Disabled) != 3 {
		t.Fatalf("danew=%v zovo=%v result=%+v", f.danew.disabled, f.gateway.disabled, result)
	}
}

func TestReadItemSnapshotProviderDefaultsToZovoCard(t *testing.T) {
	legacy := jsonmap.JSON{"cdk_issue": map[string]interface{}{"plan": "plus", "payment_country": "PH"}}
	provider, plan, country, ok := cdkissuedomain.ReadItemSnapshotProvider(legacy)
	if !ok || provider != cdkissuedomain.ProviderZovoCard || plan != "plus" || country != "PH" {
		t.Fatalf("legacy snapshot: %s %s %s %v", provider, plan, country, ok)
	}
	modern := jsonmap.JSON{}
	cdkissuedomain.ApplyItemSnapshot(modern, cdkissuedomain.ProviderDanewCDK, "Plus", "us")
	provider, plan, country, ok = cdkissuedomain.ReadItemSnapshotProvider(modern)
	if !ok || provider != cdkissuedomain.ProviderDanewCDK || plan != "plus" || country != "US" {
		t.Fatalf("modern snapshot: %s %s %s %v", provider, plan, country, ok)
	}
}
