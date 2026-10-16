package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dujiao-next/internal/config"

	"github.com/gin-gonic/gin"
)

// serveWithSecurityHeaders 跑一个最小的 gin 引擎，返回响应记录器。
func serveWithSecurityHeaders(t *testing.T, cfg config.SecurityHeadersConfig, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(SecurityHeadersMiddleware(cfg))
	r.GET("/ping", func(c *gin.Context) {
		if handler != nil {
			handler(c)
			return
		}
		c.String(http.StatusOK, "pong")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	return w
}

func TestSecurityHeadersMiddlewareDefaults(t *testing.T) {
	cfg := config.SecurityHeadersConfig{
		Enabled:            true,
		FrameOptions:       "SAMEORIGIN",
		ContentTypeOptions: "nosniff",
		ReferrerPolicy:     "strict-origin-when-cross-origin",
	}
	w := serveWithSecurityHeaders(t, cfg, nil)

	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	// 未配置时下发的默认权限策略应当关掉用不到的敏感能力。
	if got := w.Header().Get("Permissions-Policy"); got != config.DefaultPermissionsPolicy() {
		t.Fatalf("Permissions-Policy = %q, want the default policy", got)
	}
	// CSP 与 HSTS 未显式配置，不应下发。
	if got := w.Header().Get("Content-Security-Policy"); got != "" {
		t.Fatalf("CSP should not be sent unless configured, got %q", got)
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS should not be sent unless configured, got %q", got)
	}
}

func TestSecurityHeadersMiddlewareDisabled(t *testing.T) {
	cfg := config.SecurityHeadersConfig{
		Enabled:            false,
		FrameOptions:       "DENY",
		ContentTypeOptions: "nosniff",
		ReferrerPolicy:     "strict-origin-when-cross-origin",
	}
	w := serveWithSecurityHeaders(t, cfg, nil)

	for _, name := range []string{
		"X-Frame-Options",
		"X-Content-Type-Options",
		"Referrer-Policy",
		"Permissions-Policy",
		"Content-Security-Policy",
		"Strict-Transport-Security",
	} {
		if got := w.Header().Get(name); got != "" {
			t.Fatalf("%s should be absent when disabled, got %q", name, got)
		}
	}
}

func TestSecurityHeadersMiddlewareExplicitCSPAndHSTS(t *testing.T) {
	cfg := config.SecurityHeadersConfig{
		Enabled:               true,
		FrameOptions:          "DENY",
		ContentTypeOptions:    "nosniff",
		ReferrerPolicy:        "no-referrer",
		PermissionsPolicy:     "geolocation=()",
		ContentSecurityPolicy: "default-src 'self'",
		HSTSMaxAgeSeconds:     31536000,
		HSTSIncludeSubDomains: true,
	}
	w := serveWithSecurityHeaders(t, cfg, nil)

	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("CSP = %q", got)
	}
	if got := w.Header().Get("Permissions-Policy"); got != "geolocation=()" {
		t.Fatalf("explicit Permissions-Policy should win over the default, got %q", got)
	}
	want := "max-age=31536000; includeSubDomains"
	if got := w.Header().Get("Strict-Transport-Security"); got != want {
		t.Fatalf("HSTS = %q, want %q", got, want)
	}
}

// TestBuildHSTSHeader 覆盖 preload 组合与未启用时的空串。
func TestBuildHSTSHeader(t *testing.T) {
	if got := buildHSTSHeader(config.SecurityHeadersConfig{}); got != "" {
		t.Fatalf("zero max-age should disable HSTS, got %q", got)
	}
	if got := buildHSTSHeader(config.SecurityHeadersConfig{HSTSMaxAgeSeconds: -1}); got != "" {
		t.Fatalf("negative max-age should disable HSTS, got %q", got)
	}

	got := buildHSTSHeader(config.SecurityHeadersConfig{
		HSTSMaxAgeSeconds:     63072000,
		HSTSIncludeSubDomains: true,
		HSTSPreload:           true,
	})
	want := "max-age=63072000; includeSubDomains; preload"
	if got != want {
		t.Fatalf("buildHSTSHeader = %q, want %q", got, want)
	}
}

// TestSecurityHeadersPresentOnErrorResponses 校验头在非 200 响应上同样下发——
// 错误页与 SPA 兜底最容易漏掉安全头。
func TestSecurityHeadersPresentOnErrorResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(SecurityHeadersMiddleware(config.SecurityHeadersConfig{
		Enabled:            true,
		FrameOptions:       "SAMEORIGIN",
		ContentTypeOptions: "nosniff",
		ReferrerPolicy:     "strict-origin-when-cross-origin",
	}))
	r.NoRoute(func(c *gin.Context) { c.String(http.StatusNotFound, "404 page not found") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("error responses must still carry security headers, got %q", got)
	}
	if got := w.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("error responses must still carry X-Frame-Options, got %q", got)
	}
}
