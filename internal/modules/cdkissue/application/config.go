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
	Provider              string          `json:"provider"`
	Enabled               bool            `json:"enabled"`
	Active                bool            `json:"active"`
	Environment           string          `json:"environment"`
	BaseURL               string          `json:"base_url"`
	APIBaseURL            string          `json:"api_base_url"`
	AppID                 string          `json:"app_id"`
	HasSecret             bool            `json:"has_secret"`
	SecretHint            string          `json:"secret_hint"`
	DefaultPaymentCountry string          `json:"default_payment_country"`
	MinSpendableBalance   decimal.Decimal `json:"min_spendable_balance"`
	// QueueEnabled 为假时付款后不会触发自动发货，实时发码也不会执行。
	QueueEnabled bool `json:"queue_enabled"`
}

// ConfigsView 是全部渠道配置与当前启用渠道。
type ConfigsView struct {
	ActiveProvider string       `json:"active_provider"`
	Providers      []ConfigView `json:"providers"`
}

// ConfigPatch 是后台提交的配置补丁；AppSecret 为空表示保留原值。
type ConfigPatch struct {
	Enabled               *bool    `json:"enabled"`
	Environment           *string  `json:"environment"`
	APIBaseURL            *string  `json:"api_base_url"`
	AppID                 *string  `json:"app_id"`
	AppSecret             *string  `json:"app_secret"`
	DefaultPaymentCountry *string  `json:"default_payment_country"`
	MinSpendableBalance   *float64 `json:"min_spendable_balance"`
}

func (s *Service) toConfigView(config cdkissuedomain.Config) ConfigView {
	provider := cdkissuedomain.NormalizeProvider(config.Provider)
	environment := cdkissuedomain.NormalizeEnvironment(config.Environment)
	if provider == cdkissuedomain.ProviderDanewCDK {
		environment = cdkissuedomain.EnvironmentProduction
	}
	return ConfigView{
		Provider:              provider,
		QueueEnabled:          s.fulfillments != nil && s.fulfillments.Enabled(),
		Enabled:               config.Enabled,
		Active:                config.Active() && s.gateways[provider] != nil,
		Environment:           environment,
		BaseURL:               config.BaseURL(),
		APIBaseURL:            strings.TrimSpace(config.APIBaseURL),
		AppID:                 strings.TrimSpace(config.AppID),
		HasSecret:             config.HasSecret(),
		SecretHint:            config.AppSecretHint,
		DefaultPaymentCountry: config.ResolveCountry(""),
		MinSpendableBalance:   config.MinSpendableBalance,
	}
}

func normalizeProviderParam(provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return cdkissuedomain.ProviderZovoCard, nil
	}
	if !cdkissuedomain.IsKnownProvider(provider) {
		return "", cdkissuecontract.ErrInvalidInput
	}
	return provider, nil
}

// GetConfig 返回某渠道的脱敏配置；provider 为空表示 ZovoCard（兼容旧接口）。
func (s *Service) GetConfig(provider string) (ConfigView, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return ConfigView{}, err
	}
	config, err := s.loadConfig(provider)
	if err != nil {
		return ConfigView{}, err
	}
	return s.toConfigView(config), nil
}

// ListConfigs 返回全部渠道配置与当前启用渠道。
func (s *Service) ListConfigs() (ConfigsView, error) {
	out := ConfigsView{Providers: []ConfigView{}}
	for _, provider := range cdkissuedomain.Providers {
		if s.gateways[provider] == nil {
			continue
		}
		config, err := s.loadConfig(provider)
		if err != nil {
			return ConfigsView{}, err
		}
		view := s.toConfigView(config)
		if view.Active && out.ActiveProvider == "" {
			out.ActiveProvider = provider
		}
		out.Providers = append(out.Providers, view)
	}
	return out, nil
}

// applyPatch 在内存中应用补丁并校验，不落库。
func (s *Service) applyPatch(current cdkissuedomain.Config, patch ConfigPatch) (cdkissuedomain.Config, error) {
	next := current
	danew := cdkissuedomain.NormalizeProvider(next.Provider) == cdkissuedomain.ProviderDanewCDK
	if patch.Environment != nil && !danew {
		next.Environment = cdkissuedomain.NormalizeEnvironment(*patch.Environment)
	}
	if patch.APIBaseURL != nil && danew {
		base, err := cdkissuedomain.NormalizeAPIBaseURL(*patch.APIBaseURL)
		if err != nil {
			return next, &cdkissuecontract.ConfigError{Reason: err.Error()}
		}
		next.APIBaseURL = base
	}
	if patch.AppID != nil && !danew {
		appID := strings.TrimSpace(*patch.AppID)
		if appID != "" && !strings.HasPrefix(appID, "ak_") {
			return next, &cdkissuecontract.ConfigError{Reason: "App ID 应以 ak_ 开头"}
		}
		next.AppID = appID
	}
	if patch.AppSecret != nil {
		secret := strings.TrimSpace(*patch.AppSecret)
		if secret != "" {
			if danew && !strings.HasPrefix(secret, cdkissuedomain.DanewCDKSecretPrefix) {
				return next, &cdkissuecontract.ConfigError{Reason: "danew_cdk API Key 应以 cn_live_ 开头"}
			}
			if !danew && !strings.HasPrefix(secret, "sk_") {
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
		if danew {
			return next, &cdkissuecontract.ConfigError{Reason: "启用前请先填写 danew_cdk 接口地址和 API Key"}
		}
		return next, &cdkissuecontract.ConfigError{Reason: "启用前请先填写 App ID 和 App Secret"}
	}
	return next, nil
}

// UpdateConfig 保存某渠道配置。启用一个渠道时同一事务停用其它渠道，保证同时只有一个启用。
func (s *Service) UpdateConfig(provider string, patch ConfigPatch) (ConfigView, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return ConfigView{}, err
	}
	if s.gateways[provider] == nil {
		return ConfigView{}, cdkissuecontract.ErrNotConfigured
	}
	current, err := s.loadConfig(provider)
	if err != nil {
		return ConfigView{}, err
	}
	next, err := s.applyPatch(current, patch)
	if err != nil {
		return ConfigView{}, err
	}
	next.Provider = provider
	if next.Enabled {
		err = s.store.SaveConfigExclusive(&next)
	} else {
		err = s.store.SaveConfig(&next)
	}
	if err != nil {
		return ConfigView{}, err
	}
	s.invalidateOverview()
	return s.toConfigView(next), nil
}

// TestConnection 用当前表单值（未保存）查询渠道余额与套餐。
func (s *Service) TestConnection(ctx context.Context, provider string, patch ConfigPatch) (*cdkissuedomain.Overview, error) {
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return nil, err
	}
	gateway, err := s.gatewayFor(provider)
	if err != nil {
		return nil, err
	}
	current, err := s.loadConfig(provider)
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
	return gateway.Overview(ctx, credentials)
}

// Overview 返回已保存配置下的渠道概览；provider 为空表示当前启用渠道（都未启用时为 ZovoCard）。
func (s *Service) Overview(ctx context.Context, provider string, force bool) (*cdkissuedomain.Overview, error) {
	var config cdkissuedomain.Config
	var err error
	if strings.TrimSpace(provider) == "" {
		config, err = s.activeConfig()
	} else {
		var normalized string
		if normalized, err = normalizeProviderParam(provider); err == nil {
			config, err = s.loadConfig(normalized)
		}
	}
	if err != nil {
		return nil, err
	}
	return s.cachedOverview(ctx, config, force)
}
