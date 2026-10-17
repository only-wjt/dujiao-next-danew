package application

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/dujiao-next/internal/crypto"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"

	"github.com/shopspring/decimal"
)

const overviewCacheTTL = 60 * time.Second

// AlertNotifier 推送管理员告警；可为空。
type AlertNotifier interface {
	NotifyIssueAlert(request *cdkissuedomain.IssueRequest, message string) error
	NotifyLowBalance(spendable, threshold decimal.Decimal) error
	// NotifyRefundDisableFailed 退款后卡台卡密停用失败（可能已被兑换）。
	NotifyRefundDisableFailed(orderNo string, failed int, message string) error
}

// Options 汇总实时发码服务依赖。
type Options struct {
	Store cdkissuecontract.Store
	// Gateways 按渠道注册网关，至少需要 zovocard。
	Gateways     map[string]cdkissuecontract.Gateway
	Products     cdkissuecontract.ProductReader
	Fulfillments cdkissuecontract.FulfillmentEnqueuer
	Alerts       AlertNotifier
	AppSecretKey string
	Now          func() time.Time
}

// Service 编排实时发码：配置、商品绑定、发码、重试与停用。
// 同一时刻只有一个渠道启用；在途订单按下单时固化的渠道发码。
type Service struct {
	store        cdkissuecontract.Store
	gateways     map[string]cdkissuecontract.Gateway
	products     cdkissuecontract.ProductReader
	fulfillments cdkissuecontract.FulfillmentEnqueuer
	alerts       AlertNotifier
	encryptKey   []byte
	now          func() time.Time

	overviewMu    sync.Mutex
	overviewCache map[string]cachedOverviewEntry
}

type cachedOverviewEntry struct {
	key      string
	overview *cdkissuedomain.Overview
	expiry   time.Time
}

var (
	_ cdkissuecontract.RealtimeIssuer          = (*Service)(nil)
	_ cdkissuecontract.OrderabilityChecker     = (*Service)(nil)
	_ cdkissuecontract.RealtimeProductResolver = (*Service)(nil)
)

// NewService 创建实时发码服务。
func NewService(options Options) *Service {
	if options.Store == nil {
		panic("cdkissue service: store is nil")
	}
	if options.Gateways[cdkissuedomain.ProviderZovoCard] == nil {
		panic("cdkissue service: zovocard gateway is nil")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	gateways := make(map[string]cdkissuecontract.Gateway, len(options.Gateways))
	for provider, gateway := range options.Gateways {
		if gateway != nil {
			gateways[cdkissuedomain.NormalizeProvider(provider)] = gateway
		}
	}
	return &Service{
		store:         options.Store,
		gateways:      gateways,
		products:      options.Products,
		fulfillments:  options.Fulfillments,
		alerts:        options.Alerts,
		encryptKey:    crypto.DeriveKey(options.AppSecretKey),
		now:           now,
		overviewCache: map[string]cachedOverviewEntry{},
	}
}

// SetAlertNotifier 在通知中心构造后注入。
func (s *Service) SetAlertNotifier(alerts AlertNotifier) {
	s.alerts = alerts
}

// SetFulfillmentEnqueuer 在交付服务构造后注入（避免装配环）。
func (s *Service) SetFulfillmentEnqueuer(enqueuer cdkissuecontract.FulfillmentEnqueuer) {
	s.fulfillments = enqueuer
}

// gatewayFor 取渠道网关；未注册的渠道按未配置处理（没有发出请求，不可能扣费）。
func (s *Service) gatewayFor(provider string) (cdkissuecontract.Gateway, error) {
	gateway := s.gateways[cdkissuedomain.NormalizeProvider(provider)]
	if gateway == nil {
		return nil, cdkissuecontract.ErrNotConfigured
	}
	return gateway, nil
}

func (s *Service) loadConfig(provider string) (cdkissuedomain.Config, error) {
	provider = cdkissuedomain.NormalizeProvider(provider)
	stored, err := s.store.GetConfig(provider)
	if err != nil {
		return cdkissuedomain.Config{}, err
	}
	if stored == nil {
		return cdkissuedomain.DefaultConfig(provider), nil
	}
	stored.Provider = provider
	return *stored, nil
}

// activeConfig 返回当前启用的渠道配置；都没启用时返回 ZovoCard 配置（Active()=false）。
func (s *Service) activeConfig() (cdkissuedomain.Config, error) {
	for _, provider := range cdkissuedomain.Providers {
		config, err := s.loadConfig(provider)
		if err != nil {
			return cdkissuedomain.Config{}, err
		}
		if config.Active() && s.gateways[provider] != nil {
			return config, nil
		}
	}
	return s.loadConfig(cdkissuedomain.ProviderZovoCard)
}

func (s *Service) credentials(config cdkissuedomain.Config) (cdkissuecontract.Credentials, error) {
	if !config.Configured() {
		return cdkissuecontract.Credentials{}, cdkissuecontract.ErrNotConfigured
	}
	secret, err := crypto.Decrypt(s.encryptKey, config.AppSecretEncrypted)
	if err != nil || strings.TrimSpace(secret) == "" {
		return cdkissuecontract.Credentials{}, cdkissuecontract.ErrNotConfigured
	}
	return cdkissuecontract.Credentials{
		BaseURL:   config.BaseURL(),
		AppID:     strings.TrimSpace(config.AppID),
		AppSecret: secret,
	}, nil
}

// cachedOverview 返回缓存的渠道概览；配置变化或过期后重新拉取。
func (s *Service) cachedOverview(ctx context.Context, config cdkissuedomain.Config, force bool) (*cdkissuedomain.Overview, error) {
	provider := cdkissuedomain.NormalizeProvider(config.Provider)
	gateway, err := s.gatewayFor(provider)
	if err != nil {
		return nil, err
	}
	credentials, err := s.credentials(config)
	if err != nil {
		return nil, err
	}
	key := config.BaseURL() + "|" + config.AppID + "|" + config.AppSecretEncrypted
	now := s.now()
	s.overviewMu.Lock()
	if entry, ok := s.overviewCache[provider]; ok && !force && entry.key == key && now.Before(entry.expiry) {
		s.overviewMu.Unlock()
		return entry.overview, nil
	}
	s.overviewMu.Unlock()

	overview, err := gateway.Overview(ctx, credentials)
	if err != nil {
		return nil, err
	}
	s.overviewMu.Lock()
	s.overviewCache[provider] = cachedOverviewEntry{key: key, overview: overview, expiry: now.Add(overviewCacheTTL)}
	s.overviewMu.Unlock()
	return overview, nil
}

func (s *Service) invalidateOverview() {
	s.overviewMu.Lock()
	s.overviewCache = map[string]cachedOverviewEntry{}
	s.overviewMu.Unlock()
}
