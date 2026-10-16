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
}

// Options 汇总实时发码服务依赖。
type Options struct {
	Store        cdkissuecontract.Store
	Gateway      cdkissuecontract.Gateway
	Products     cdkissuecontract.ProductReader
	Fulfillments cdkissuecontract.FulfillmentEnqueuer
	Alerts       AlertNotifier
	AppSecretKey string
	Now          func() time.Time
}

// Service 编排 ZovoCard 实时发码：配置、商品绑定、发码、重试与停用。
type Service struct {
	store        cdkissuecontract.Store
	gateway      cdkissuecontract.Gateway
	products     cdkissuecontract.ProductReader
	fulfillments cdkissuecontract.FulfillmentEnqueuer
	alerts       AlertNotifier
	encryptKey   []byte
	now          func() time.Time

	overviewMu     sync.Mutex
	overviewCache  *cdkissuedomain.Overview
	overviewKey    string
	overviewExpiry time.Time
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
	if options.Gateway == nil {
		panic("cdkissue service: gateway is nil")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:        options.Store,
		gateway:      options.Gateway,
		products:     options.Products,
		fulfillments: options.Fulfillments,
		alerts:       options.Alerts,
		encryptKey:   crypto.DeriveKey(options.AppSecretKey),
		now:          now,
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

func (s *Service) loadConfig() (cdkissuedomain.Config, error) {
	stored, err := s.store.GetConfig(cdkissuedomain.ProviderZovoCard)
	if err != nil {
		return cdkissuedomain.Config{}, err
	}
	if stored == nil {
		return cdkissuedomain.DefaultConfig(), nil
	}
	return *stored, nil
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

// cachedOverview 返回缓存的卡台概览；配置变化或过期后重新拉取。
func (s *Service) cachedOverview(ctx context.Context, config cdkissuedomain.Config, force bool) (*cdkissuedomain.Overview, error) {
	credentials, err := s.credentials(config)
	if err != nil {
		return nil, err
	}
	key := config.BaseURL() + "|" + config.AppID + "|" + config.AppSecretEncrypted
	now := s.now()
	s.overviewMu.Lock()
	if !force && s.overviewCache != nil && s.overviewKey == key && now.Before(s.overviewExpiry) {
		cached := s.overviewCache
		s.overviewMu.Unlock()
		return cached, nil
	}
	s.overviewMu.Unlock()

	overview, err := s.gateway.Overview(ctx, credentials)
	if err != nil {
		return nil, err
	}
	s.overviewMu.Lock()
	s.overviewCache = overview
	s.overviewKey = key
	s.overviewExpiry = now.Add(overviewCacheTTL)
	s.overviewMu.Unlock()
	return overview, nil
}

func (s *Service) invalidateOverview() {
	s.overviewMu.Lock()
	s.overviewCache = nil
	s.overviewKey = ""
	s.overviewMu.Unlock()
}
