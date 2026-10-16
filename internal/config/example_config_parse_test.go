package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestExampleConfigParses 直接用同一个 viper 解析路径校验 config.yml.example，
// 避免示例文件写错缩进或字段名却要到线上才发现。
func TestExampleConfigParses(t *testing.T) {
	v := viper.New()
	v.SetConfigFile(filepath.Join("..", "..", "config.yml.example"))
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("read example config: %v", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal example config: %v", err)
	}

	if len(cfg.CORS.AllowedOrigins) == 0 {
		t.Fatal("cors.allowed_origins parsed empty")
	}
	sh := cfg.Security.SecurityHeaders
	if !sh.Enabled {
		t.Fatal("security.security_headers.enabled should parse as true")
	}
	if sh.ContentTypeOptions != "nosniff" {
		t.Fatalf("content_type_options = %q, want nosniff", sh.ContentTypeOptions)
	}
	if sh.FrameOptions != "SAMEORIGIN" {
		t.Fatalf("frame_options = %q, want SAMEORIGIN", sh.FrameOptions)
	}
	if sh.ReferrerPolicy != "strict-origin-when-cross-origin" {
		t.Fatalf("referrer_policy = %q", sh.ReferrerPolicy)
	}
	if sh.HSTSMaxAgeSeconds != 0 {
		t.Fatalf("hsts_max_age_seconds = %d, want 0 (disabled by default)", sh.HSTSMaxAgeSeconds)
	}
	t.Logf("cors.allow_credentials=%v origins=%v", cfg.CORS.AllowCredentials, cfg.CORS.AllowedOrigins)
	t.Logf("security_headers=%+v", sh)
}
