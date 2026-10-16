package middleware

import (
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/config"

	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware 下发安全响应头。
//
// 审计 F2：此前响应中缺少 CSP、X-Frame-Options、X-Content-Type-Options、
// Referrer-Policy、Permissions-Policy，使 XSS 载荷可内联执行、页面可被 iframe
// 嵌套点击劫持、MIME 嗅探与 Referer 泄露完整 URL 均无缓解。
//
// 默认只下发零副作用的三项；CSP 与 HSTS 需要显式配置后才生效，原因见
// config.SecurityHeadersConfig 的说明——它们误配会直接打断站点功能。
//
// 头在 c.Next() 之前写入：静态文件与 SPA 兜底都走同一个引擎，提前写入可保证
// 所有响应（含 404/错误页）都带上，而事后写入会被已提交的响应头丢掉。
func SecurityHeadersMiddleware(cfg config.SecurityHeadersConfig) gin.HandlerFunc {
	if !cfg.Enabled {
		return func(c *gin.Context) { c.Next() }
	}

	frameOptions := strings.TrimSpace(cfg.FrameOptions)
	contentTypeOptions := strings.TrimSpace(cfg.ContentTypeOptions)
	referrerPolicy := strings.TrimSpace(cfg.ReferrerPolicy)
	permissionsPolicy := strings.TrimSpace(cfg.PermissionsPolicy)
	if permissionsPolicy == "" {
		permissionsPolicy = config.DefaultPermissionsPolicy()
	}
	csp := strings.TrimSpace(cfg.ContentSecurityPolicy)

	hsts := buildHSTSHeader(cfg)

	return func(c *gin.Context) {
		header := c.Writer.Header()
		// X-Frame-Options 已被 CSP 的 frame-ancestors 取代，但旧浏览器仍需前者。
		// 两者同时下发会导致旧浏览器优先采用更宽松的一方，因此只发 X-Frame-Options，
		// 由运维在需要细粒度控制时改用 CSP 的 frame-ancestors。
		if frameOptions != "" {
			header.Set("X-Frame-Options", frameOptions)
		}
		if contentTypeOptions != "" {
			header.Set("X-Content-Type-Options", contentTypeOptions)
		}
		if referrerPolicy != "" {
			header.Set("Referrer-Policy", referrerPolicy)
		}
		if permissionsPolicy != "" {
			header.Set("Permissions-Policy", permissionsPolicy)
		}
		if csp != "" {
			header.Set("Content-Security-Policy", csp)
		}
		if hsts != "" {
			header.Set("Strict-Transport-Security", hsts)
		}

		c.Next()
	}
}

// buildHSTSHeader 组装 Strict-Transport-Security 值；未启用时返回空串。
//
// 注意：本中间件不感知当前连接是否经由 HTTPS。若部署在反向代理之后且代理已
// 终结 TLS，HttpOnly 之外的判断无从进行，HSTS 只在明确配置后才下发，
// 由运维确认对外访问确实是 HTTPS 再开启。
func buildHSTSHeader(cfg config.SecurityHeadersConfig) string {
	if cfg.HSTSMaxAgeSeconds <= 0 {
		return ""
	}
	parts := []string{"max-age=" + strconv.Itoa(cfg.HSTSMaxAgeSeconds)}
	if cfg.HSTSIncludeSubDomains {
		parts = append(parts, "includeSubDomains")
	}
	if cfg.HSTSPreload {
		parts = append(parts, "preload")
	}
	return strings.Join(parts, "; ")
}
