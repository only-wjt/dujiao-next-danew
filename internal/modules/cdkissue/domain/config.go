package domain

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	EnvironmentSandbox    = "sandbox"
	EnvironmentProduction = "production"

	SandboxBaseURL    = "https://sandbox.zovocard.com/openapi/v1"
	ProductionBaseURL = "https://zovocard.com/openapi/v1"

	// DanewCDKSecretPrefix 是 danew_cdk 卡网客户端 Key 的前缀。
	DanewCDKSecretPrefix = "cn_live_"

	DefaultPaymentCountry = "PH"
)

// DefaultMinSpendableBalance 是默认余额预警线（USD）。
var DefaultMinSpendableBalance = decimal.NewFromInt(50)

// Config 是卡台对接配置，每个 provider 一行。App Secret 以密文存储。
// APIBaseURL 仅 danew_cdk 使用（例如 https://cdk.danew.cc/api/v1/cardnet）；ZovoCard 地址由环境决定。
type Config struct {
	ID                    uint            `gorm:"primarykey" json:"id"`
	Provider              string          `gorm:"type:varchar(32);not null;uniqueIndex" json:"provider"`
	Environment           string          `gorm:"type:varchar(16);not null;default:'sandbox'" json:"environment"`
	AppID                 string          `gorm:"type:varchar(128);not null;default:''" json:"app_id"`
	AppSecretEncrypted    string          `gorm:"type:text" json:"-"`
	AppSecretHint         string          `gorm:"type:varchar(32);not null;default:''" json:"app_secret_hint"`
	APIBaseURL            string          `gorm:"column:api_base_url;type:varchar(255);not null;default:''" json:"api_base_url"`
	DefaultPaymentCountry string          `gorm:"type:varchar(8);not null;default:'PH'" json:"default_payment_country"`
	MinSpendableBalance   decimal.Decimal `gorm:"type:decimal(20,2);not null;default:50" json:"min_spendable_balance"`
	Enabled               bool            `gorm:"not null;default:false" json:"enabled"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

// TableName 指定表名。
func (Config) TableName() string {
	return "cdk_issue_configs"
}

// DefaultConfig 返回尚未保存时的默认配置。
func DefaultConfig(provider string) Config {
	config := Config{
		Provider:              NormalizeProvider(provider),
		Environment:           EnvironmentSandbox,
		DefaultPaymentCountry: DefaultPaymentCountry,
		MinSpendableBalance:   DefaultMinSpendableBalance,
	}
	if config.Provider == ProviderDanewCDK {
		// danew_cdk 没有沙盒，地址由管理员填写。
		config.Environment = EnvironmentProduction
	}
	return config
}

// NormalizeEnvironment 未知取值一律回落为沙盒，避免误连生产。
func NormalizeEnvironment(env string) string {
	if strings.EqualFold(strings.TrimSpace(env), EnvironmentProduction) {
		return EnvironmentProduction
	}
	return EnvironmentSandbox
}

// BaseURL ZovoCard 由环境决定，不允许任意填写，防止把密钥发到第三方地址；
// danew_cdk 用管理员填写并经 NormalizeAPIBaseURL 校验过的地址。
func (c Config) BaseURL() string {
	if NormalizeProvider(c.Provider) == ProviderDanewCDK {
		return strings.TrimSpace(c.APIBaseURL)
	}
	if NormalizeEnvironment(c.Environment) == EnvironmentProduction {
		return ProductionBaseURL
	}
	return SandboxBaseURL
}

// HasSecret 表示是否已保存 App Secret。
func (c Config) HasSecret() bool {
	return strings.TrimSpace(c.AppSecretEncrypted) != ""
}

// Configured 表示凭据齐全。
func (c Config) Configured() bool {
	if NormalizeProvider(c.Provider) == ProviderDanewCDK {
		return strings.TrimSpace(c.APIBaseURL) != "" && c.HasSecret()
	}
	return strings.TrimSpace(c.AppID) != "" && c.HasSecret()
}

// Active 表示实时发码可以对外生效。
func (c Config) Active() bool {
	return c.Enabled && c.Configured()
}

// ResolveCountry 返回实际使用的付款地区。
func (c Config) ResolveCountry(country string) string {
	if normalized := NormalizeCountry(country); normalized != "" {
		return normalized
	}
	if normalized := NormalizeCountry(c.DefaultPaymentCountry); normalized != "" {
		return normalized
	}
	return DefaultPaymentCountry
}

// SecretHint 生成脱敏提示，例如 sk_****a1b2。
func SecretHint(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	tail := secret
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	prefix := ""
	for _, known := range []string{DanewCDKSecretPrefix, "sk_"} {
		if strings.HasPrefix(secret, known) {
			prefix = known
			break
		}
	}
	return prefix + "****" + tail
}

// NormalizeAPIBaseURL 校验 danew_cdk 接口地址：只允许 https 域名，不带查询串，
// 拒绝 IP 字面量与 localhost，避免把密钥误配到内网或第三方。
func NormalizeAPIBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("接口地址必须是 https:// 开头的完整地址")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", fmt.Errorf("接口地址不能包含查询参数、锚点或账号信息")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(host) != nil {
		return "", fmt.Errorf("接口地址必须是公网域名，不能是 IP 或 localhost")
	}
	if !strings.Contains(host, ".") {
		return "", fmt.Errorf("接口地址必须是公网域名")
	}
	return parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.EscapedPath(), "/"), nil
}
