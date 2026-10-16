package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/config"

	"github.com/gin-gonic/gin"
)

func TestResolveAllowedOrigin(t *testing.T) {
	got := resolveAllowedOrigin("https://example.com", []string{"*"}, false)
	if got != "*" {
		t.Fatalf("wildcard without credentials should return *, got %s", got)
	}

	// 通配符 + 凭证：规范禁止该组合，必须拒绝而不是把请求来源原样反射回去。
	got = resolveAllowedOrigin("https://example.com", []string{"*"}, true)
	if got != "" {
		t.Fatalf("wildcard with credentials must not echo origin, got %s", got)
	}

	got = resolveAllowedOrigin("https://a.example.com", []string{"https://a.example.com", "https://b.example.com"}, false)
	if got != "https://a.example.com" {
		t.Fatalf("allow-list should return matched origin, got %s", got)
	}

	got = resolveAllowedOrigin("https://x.example.com", []string{"https://a.example.com"}, false)
	if got != "" {
		t.Fatalf("unmatched origin should be empty, got %s", got)
	}
}

// TestResolveAllowedOriginRejectsAttackShapes 覆盖审计中实测到的几种跨域投递形态。
// 这些值都不是浏览器为普通页面请求生成的合法 Origin，一旦被原样回显，
// 就等于给出一个任意来源的跨域读取入口。注意区分两种拒绝强度：
//   - 所有模式下都必须拒绝的：来源本身不合法（null、多来源、非 http(s)、带路径查询等）。
//   - 仅凭证模式下必须拒绝的：来源形态合法但未被白名单精确命中，此时退回 "*" 是
//     公开可读的声明、并未反射给该来源，属正常行为；但一旦携带凭证就绝不能放行。
func TestResolveAllowedOriginRejectsAttackShapes(t *testing.T) {
	alwaysRejected := []struct {
		name    string
		origin  string
		allowed []string
	}{
		{"null origin", "null", []string{"*"}},
		{"null origin with explicit allowlist", "null", []string{"null", "https://card.example.com"}},
		{"multi-origin list", "https://evil.example, https://evil2.example", []string{"*"}},
		{"scheme-relative origin", "//evil.example", []string{"*"}},
		{"non-http scheme", "file:///etc/passwd", []string{"file:///etc/passwd"}},
		{"origin with path", "https://card.example.com/../admin", []string{"*"}},
		{"origin with query", "https://card.example.com/?x=1", []string{"*"}},
		{"empty origin", "", []string{"*"}},
		{"whitespace origin", "   ", []string{"*"}},
		{"hostless origin", "https://", []string{"*"}},
	}

	for _, tc := range alwaysRejected {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveAllowedOrigin(tc.origin, tc.allowed, true); got != "" {
				t.Fatalf("origin %q must not be echoed with credentials, got %q", tc.origin, got)
			}
			if got := resolveAllowedOrigin(tc.origin, tc.allowed, false); got != "" {
				t.Fatalf("origin %q must not be echoed without credentials either, got %q", tc.origin, got)
			}
		})
	}

	credentialOnly := []struct {
		name    string
		origin  string
		allowed []string
	}{
		{"plain http origin", "http://evil.example", []string{"*"}},
		{"unlisted https origin", "https://evil.example", []string{"*"}},
		{"unlisted origin against explicit allowlist", "https://evil.example", []string{"https://card.example.com"}},
	}

	for _, tc := range credentialOnly {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveAllowedOrigin(tc.origin, tc.allowed, true); got != "" {
				t.Fatalf("origin %q must not be granted credentials, got %q", tc.origin, got)
			}
		})
	}
}

// TestResolveAllowedOriginWildcardDoesNotShortCircuit 回归用例：白名单里同时出现 "*"
// 和具体来源时，旧实现在遍历到 "*" 时立即返回，后面的具体来源永远得不到检查。
func TestResolveAllowedOriginWildcardDoesNotShortCircuit(t *testing.T) {
	allowed := []string{"*", "https://card.example.com"}

	if got := resolveAllowedOrigin("https://card.example.com", allowed, false); got != "https://card.example.com" {
		t.Fatalf("allowlisted origin should be echoed, got %s", got)
	}
	if got := resolveAllowedOrigin("https://evil.example", allowed, false); got != "*" {
		t.Fatalf("unlisted origin should fall back to *, got %s", got)
	}
	if got := resolveAllowedOrigin("https://evil.example", allowed, true); got != "" {
		t.Fatalf("unlisted origin must be rejected when credentials are allowed, got %s", got)
	}
}

// TestCORSMiddlewareCredentialHeaders 校验中间件层的响应头组合：
// 凭证头绝不能与 "*"、空来源或未命中的来源一起发出。
func TestCORSMiddlewareCredentialHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	serve := func(t *testing.T, cfg config.CORSConfig, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := gin.New()
		r.Use(CORSMiddleware(cfg))
		r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("wildcard with credentials emits neither ACAO reflection nor credential header", func(t *testing.T) {
		w := serve(t, config.CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true}, "https://evil.example")
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO should be empty, got %q", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Fatalf("ACAC must not be sent alongside an unusable ACAO, got %q", got)
		}
	})

	t.Run("allowlisted origin gets scoped credential header", func(t *testing.T) {
		w := serve(t, config.CORSConfig{
			AllowedOrigins:   []string{"https://card.example.com"},
			AllowCredentials: true,
		}, "https://card.example.com")
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://card.example.com" {
			t.Fatalf("ACAO should echo the allowlisted origin, got %q", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("ACAC should be true for an allowlisted origin, got %q", got)
		}
		if got := w.Header().Values("Vary"); !slices.Contains(got, "Origin") {
			t.Fatalf("Vary should contain Origin for an origin-dependent response, got %v", got)
		}
	})

	t.Run("unlisted origin is refused", func(t *testing.T) {
		w := serve(t, config.CORSConfig{
			AllowedOrigins:   []string{"https://card.example.com"},
			AllowCredentials: true,
		}, "https://evil.example")
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO should be empty for an unlisted origin, got %q", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Fatalf("ACAC must not be sent for an unlisted origin, got %q", got)
		}
	})

	t.Run("request without Origin gets no CORS headers", func(t *testing.T) {
		w := serve(t, config.CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true}, "")
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO should be empty without an Origin header, got %q", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Fatalf("ACAC should be empty without an Origin header, got %q", got)
		}
	})

	t.Run("preflight from an unlisted origin is still 204 but grants nothing", func(t *testing.T) {
		r := gin.New()
		r.Use(CORSMiddleware(config.CORSConfig{
			AllowedOrigins:   []string{"https://card.example.com"},
			AllowCredentials: true,
		}))
		r.PATCH("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

		req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Method", "PATCH")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("preflight should still return 204, got %d", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("preflight must not grant an unlisted origin, got %q", got)
		}
	})
}

func TestRequestIDMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"request_id": getRequestID(c)})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(requestIDHeader, "req-123")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status want 200 got %d", w.Code)
	}
	if w.Header().Get(requestIDHeader) != "req-123" {
		t.Fatalf("response request id want req-123 got %s", w.Header().Get(requestIDHeader))
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp["request_id"] != "req-123" {
		t.Fatalf("context request id want req-123 got %s", resp["request_id"])
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w2, req2)
	generated := w2.Header().Get(requestIDHeader)
	if generated == "" {
		t.Fatalf("generated request id should not be empty")
	}
	if resp := strings.TrimSpace(generated); resp == "" {
		t.Fatalf("generated request id should not be blank")
	}
}

func TestJWTAuthMiddlewareMissingSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(JWTAuthMiddleware("", nil))
	r.GET("/admin/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status want 200 got %d", w.Code)
	}
	var resp struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("status_code want 401 got %d", resp.StatusCode)
	}
}
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
