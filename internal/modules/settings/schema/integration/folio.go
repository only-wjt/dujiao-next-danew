package settingsintegration

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

var ErrFolioConfigInvalid = errors.New("folio config invalid")

// FolioSetting Folio 开票对接配置。
type FolioSetting struct {
	BaseURL string `json:"base_url"`
	Secret  string `json:"secret"`
}

// FolioSettingPatch Folio 配置补丁（支持部分更新；secret 空字符串表示保留原值）。
type FolioSettingPatch struct {
	BaseURL *string `json:"base_url"`
	Secret  *string `json:"secret"`
}

// DefaultFolioSetting 根据静态配置生成默认 Folio 设置。
func DefaultFolioSetting(cfg config.FolioConfig) FolioSetting {
	return NormalizeFolioSetting(FolioSetting{
		BaseURL: cfg.BaseURL,
		Secret:  cfg.Secret,
	})
}

// NormalizeFolioSetting 归一化 Folio 配置。
func NormalizeFolioSetting(setting FolioSetting) FolioSetting {
	setting.BaseURL = strings.TrimRight(strings.TrimSpace(setting.BaseURL), "/")
	setting.Secret = strings.TrimSpace(setting.Secret)
	return setting
}

// ValidateFolioSetting 校验 Folio 配置：全空表示关闭；任一有值则两者都必须合法。
func ValidateFolioSetting(setting FolioSetting) error {
	normalized := NormalizeFolioSetting(setting)
	if normalized.BaseURL == "" && normalized.Secret == "" {
		return nil
	}
	if normalized.BaseURL == "" {
		return fmt.Errorf("%w: Folio Base URL 不能为空", ErrFolioConfigInvalid)
	}
	if normalized.Secret == "" {
		return fmt.Errorf("%w: Folio Secret 不能为空", ErrFolioConfigInvalid)
	}
	parsed, err := url.Parse(normalized.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: Folio Base URL 格式无效", ErrFolioConfigInvalid)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: Folio Base URL 仅支持 http/https", ErrFolioConfigInvalid)
	}
	return nil
}

// FolioSettingToConfig 将 Folio 设置转换为运行时配置。
func FolioSettingToConfig(setting FolioSetting) config.FolioConfig {
	normalized := NormalizeFolioSetting(setting)
	return config.FolioConfig{
		BaseURL: normalized.BaseURL,
		Secret:  normalized.Secret,
	}
}

// EncodeFolioSetting 将 Folio 设置编码为 settings 表结构。
func EncodeFolioSetting(setting FolioSetting) jsonmap.JSON {
	normalized := NormalizeFolioSetting(setting)
	return jsonmap.JSON{
		"base_url": normalized.BaseURL,
		"secret":   normalized.Secret,
	}
}

// MaskFolioSettingForAdmin 返回脱敏后的 Folio 设置。
func MaskFolioSettingForAdmin(setting FolioSetting) jsonmap.JSON {
	normalized := NormalizeFolioSetting(setting)
	return jsonmap.JSON{
		"base_url":   normalized.BaseURL,
		"secret":     "",
		"has_secret": normalized.Secret != "",
		"enabled":    config.FolioEnabled(FolioSettingToConfig(normalized)),
	}
}

// DecodeFolioSetting 从持久化 JSON 解码，并对缺失字段使用 fallback。
func DecodeFolioSetting(raw jsonmap.JSON, fallback FolioSetting) FolioSetting {
	next := fallback
	if raw == nil {
		return next
	}
	if value, exists := raw["base_url"]; exists {
		if text, ok := value.(string); ok {
			next.BaseURL = text
		}
	}
	if value, exists := raw["secret"]; exists {
		if text, ok := value.(string); ok {
			next.Secret = text
		}
	}
	return next
}

// ApplyFolioSettingPatch 把补丁应用到当前 Folio 配置并完成校验。
// Base URL 清空时同时清除 Secret，便于在后台一键关闭对接。
func ApplyFolioSettingPatch(current FolioSetting, patch FolioSettingPatch) (FolioSetting, error) {
	next := current
	if patch.BaseURL != nil {
		next.BaseURL = strings.TrimSpace(*patch.BaseURL)
		if next.BaseURL == "" {
			next.Secret = ""
		}
	}
	if patch.Secret != nil {
		secret := strings.TrimSpace(*patch.Secret)
		if secret != "" {
			next.Secret = secret
		}
	}
	normalized := NormalizeFolioSetting(next)
	if err := ValidateFolioSetting(normalized); err != nil {
		return FolioSetting{}, err
	}
	return normalized, nil
}
