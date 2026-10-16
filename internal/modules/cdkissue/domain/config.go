package domain

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	EnvironmentSandbox    = "sandbox"
	EnvironmentProduction = "production"

	SandboxBaseURL    = "https://sandbox.zovocard.com/openapi/v1"
	ProductionBaseURL = "https://zovocard.com/openapi/v1"

	DefaultPaymentCountry = "PH"
)

// DefaultMinSpendableBalance 是默认余额预警线（USD）。
var DefaultMinSpendableBalance = decimal.NewFromInt(50)

// Config 是卡台对接配置，每个 provider 一行。App Secret 以密文存储。
type Config struct {
	ID                    uint            `gorm:"primarykey" json:"id"`
	Provider              string          `gorm:"type:varchar(32);not null;uniqueIndex" json:"provider"`
	Environment           string          `gorm:"type:varchar(16);not null;default:'sandbox'" json:"environment"`
	AppID                 string          `gorm:"type:varchar(128);not null;default:''" json:"app_id"`
	AppSecretEncrypted    string          `gorm:"type:text" json:"-"`
	AppSecretHint         string          `gorm:"type:varchar(32);not null;default:''" json:"app_secret_hint"`
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
func DefaultConfig() Config {
	return Config{
		Provider:              ProviderZovoCard,
		Environment:           EnvironmentSandbox,
		DefaultPaymentCountry: DefaultPaymentCountry,
		MinSpendableBalance:   DefaultMinSpendableBalance,
	}
}

// NormalizeEnvironment 未知取值一律回落为沙盒，避免误连生产。
func NormalizeEnvironment(env string) string {
	if strings.EqualFold(strings.TrimSpace(env), EnvironmentProduction) {
		return EnvironmentProduction
	}
	return EnvironmentSandbox
}

// BaseURL 由环境决定，不允许任意填写，防止把密钥发到第三方地址。
func (c Config) BaseURL() string {
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
	if strings.HasPrefix(secret, "sk_") {
		prefix = "sk_"
	}
	return prefix + "****" + tail
}
