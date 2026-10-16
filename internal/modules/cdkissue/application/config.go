package application

import (
	"context"
	"strings"

	"github.com/dujiao-next/internal/crypto"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"

	"github.com/shopspring/decimal"
)

// ConfigView 是返回给后台的脱敏配置。
type ConfigView struct {
	Enabled               bool            `json:"enabled"`
	Active                bool            `json:"active"`
	Environment           string          `json:"environment"`
	BaseURL               string          `json:"base_url"`
	AppID                 string          `json:"app_id"`
	HasSecret             bool            `json:"has_secret"`
	SecretHint            string          `json:"secret_hint"`
	DefaultPaymentCountry string          `json:"default_payment_country"`
	MinSpendableBalance   decimal.Decimal `json:"min_spendable_balance"`
	// QueueEnabled 为假时付款后不会触发自动发货，实时发码也不会执行。
	QueueEnabled bool `json:"queue_enabled"`
}

// ConfigPatch 是后台提交的配置补丁；AppSecret 为空表示保留原值。
type ConfigPatch struct {
	Enabled               *bool    `json:"enabled"`
	Environment           *string  `json:"environment"`
	AppID                 *string  `json:"app_id"`
	AppSecret             *string  `json:"app_secret"`
	DefaultPaymentCountry *string  `json:"default_payment_country"`
	MinSpendableBalance   *float64 `json:"min_spendable_balance"`
}

func (s *Service) toConfigView(config cdkissuedomain.Config) ConfigView {
	return ConfigView{
		QueueEnabled:          s.fulfillments != nil && s.fulfillments.Enabled(),
		Enabled:               config.Enabled,
		Active:                config.Active(),
		Environment:           cdkissuedomain.NormalizeEnvironment(config.Environment),
		BaseURL:               config.BaseURL(),
		AppID:                 strings.TrimSpace(config.AppID),
		HasSecret:             config.HasSecret(),
		SecretHint:            config.AppSecretHint,
		DefaultPaymentCountry: config.ResolveCountry(""),
		MinSpendableBalance:   config.MinSpendableBalance,
	}
}

// GetConfig 返回脱敏配置。
func (s *Service) GetConfig() (ConfigView, error) {
	config, err := s.loadConfig()
	if err != nil {
		return ConfigView{}, err
	}
	return s.toConfigView(config), nil
}

// applyPatch 在内存中应用补丁并校验，不落库。
func (s *Service) applyPatch(current cdkissuedomain.Config, patch ConfigPatch) (cdkissuedomain.Config, error) {
	next := current
	if patch.Environment != nil {
		next.Environment = cdkissuedomain.NormalizeEnvironment(*patch.Environment)
	}
	if patch.AppID != nil {
		appID := strings.TrimSpace(*patch.AppID)
		if appID != "" && !strings.HasPrefix(appID, "ak_") {
			return next, &cdkissuecontract.ConfigError{Reason: "App ID 应以 ak_ 开头"}
		}
		next.AppID = appID
	}
	if patch.AppSecret != nil {
		secret := strings.TrimSpace(*patch.AppSecret)
		if secret != "" {
			if !strings.HasPrefix(secret, "sk_") {
				return next, &cdkissuecontract.ConfigError{Reason: "App Secret 应以 sk_ 开头"}
			}
			encrypted, err := crypto.Encrypt(s.encryptKey, secret)
			if err != nil {
				return next, err
			}
			next.AppSecretEncrypted = encrypted
			next.AppSecretHint = cdkissuedomain.SecretHint(secret)
		}
	}
	if patch.DefaultPaymentCountry != nil {
		country := cdkissuedomain.NormalizeCountry(*patch.DefaultPaymentCountry)
		if country == "" {
			country = cdkissuedomain.DefaultPaymentCountry
		}
		if len(country) != 2 {
			return next, &cdkissuecontract.ConfigError{Reason: "付款地区应为两位国家代码，例如 PH"}
		}
		next.DefaultPaymentCountry = country
	}
	if patch.MinSpendableBalance != nil {
		value := decimal.NewFromFloat(*patch.MinSpendableBalance).Round(2)
		if value.IsNegative() {
			return next, &cdkissuecontract.ConfigError{Reason: "余额预警线不能小于 0"}
		}
		next.MinSpendableBalance = value
	}
	if patch.Enabled != nil {
		next.Enabled = *patch.Enabled
	}
	if next.Enabled && !next.Configured() {
		return next, &cdkissuecontract.ConfigError{Reason: "启用前请先填写 App ID 和 App Secret"}
	}
	return next, nil
}

// UpdateConfig 保存配置。
func (s *Service) UpdateConfig(patch ConfigPatch) (ConfigView, error) {
	current, err := s.loadConfig()
	if err != nil {
		return ConfigView{}, err
	}
	next, err := s.applyPatch(current, patch)
	if err != nil {
		return ConfigView{}, err
	}
	next.Provider = cdkissuedomain.ProviderZovoCard
	if err := s.store.SaveConfig(&next); err != nil {
		return ConfigView{}, err
	}
	s.invalidateOverview()
	return s.toConfigView(next), nil
}

// TestConnection 用当前表单值（未保存）查询卡台余额与套餐。
func (s *Service) TestConnection(ctx context.Context, patch ConfigPatch) (*cdkissuedomain.Overview, error) {
	current, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	// 测试不受启用开关约束。
	patch.Enabled = nil
	candidate, err := s.applyPatch(current, patch)
	if err != nil {
		return nil, err
	}
	credentials, err := s.credentials(candidate)
	if err != nil {
		return nil, err
	}
	return s.gateway.Overview(ctx, credentials)
}

// Overview 返回已保存配置下的卡台概览。
func (s *Service) Overview(ctx context.Context, force bool) (*cdkissuedomain.Overview, error) {
	config, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	return s.cachedOverview(ctx, config, force)
}
