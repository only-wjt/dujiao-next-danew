package middleware

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/modules/identity/jwttoken"
	usercontract "github.com/dujiao-next/internal/modules/identity/user/contract"
	userauthapp "github.com/dujiao-next/internal/modules/identity/userauth/application"

	"go.uber.org/zap"

	"github.com/dujiao-next/internal/authz"
	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/i18n"
	"github.com/dujiao-next/internal/logger"
	admincontract "github.com/dujiao-next/internal/modules/identity/admin/contract"
	adminauthapp "github.com/dujiao-next/internal/modules/identity/adminauth/application"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const requestIDKey = "request_id"
const requestIDHeader = "X-Request-ID"
const adminIsSuperContextKey = "admin_is_super"
const authHeaderKey = "Authorization"
const authSchemeBearer = "Bearer"

// CORSMiddleware 跨域中间件
func CORSMiddleware(cfg config.CORSConfig) gin.HandlerFunc {
	allowedOrigins := cfg.AllowedOrigins
	if len(allowedOrigins) == 0 {
		allowedOrigins = config.DefaultCORSAllowedOrigins()
	}
	allowedMethods := cfg.AllowedMethods
	if len(allowedMethods) == 0 {
		allowedMethods = config.DefaultCORSAllowedMethods()
	}
	allowedHeaders := cfg.AllowedHeaders
	if len(allowedHeaders) == 0 {
		allowedHeaders = config.DefaultCORSAllowedHeaders()
	}
	methodsHeader := strings.Join(allowedMethods, ", ")
	headersHeader := strings.Join(allowedHeaders, ", ")
	// 只有「纯通配且不带凭证」时响应才与请求来源无关，可以安全共享缓存。
	// 其余任何配置下回显值都取决于 Origin，必须声明 Vary: Origin，
	// 否则中间缓存会把发给某个来源的响应复用到另一个来源上。
	variesByOrigin := cfg.AllowCredentials || !isPureWildcardAllowlist(allowedOrigins)

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowedOrigin := resolveAllowedOrigin(origin, allowedOrigins, cfg.AllowCredentials)
		if allowedOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		}
		if variesByOrigin {
			c.Writer.Header().Add("Vary", "Origin")
		}
		// 只有确实回显了具体来源时才能声明允许凭证。与 "*" 组合或没有可用来源时
		// 声明该头没有任何浏览器会接受，反而会误导调用方以为凭证请求被放行。
		if cfg.AllowCredentials && allowedOrigin != "" && allowedOrigin != "*" {
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if allowedOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Headers", headersHeader)
			c.Writer.Header().Set("Access-Control-Allow-Methods", methodsHeader)
			if cfg.MaxAge > 0 {
				c.Writer.Header().Set("Access-Control-Max-Age", strconv.Itoa(cfg.MaxAge))
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// isPureWildcardAllowlist 报告白名单是否为「仅一个 *」，即不针对具体来源的配置。
func isPureWildcardAllowlist(allowedOrigins []string) bool {
	if len(allowedOrigins) != 1 {
		return false
	}
	return strings.TrimSpace(allowedOrigins[0]) == "*"
}

// resolveAllowedOrigin 决定本次请求应回显的 Access-Control-Allow-Origin 值。
//
// 规则：
//   - 请求未携带 Origin → 不是跨域请求，不回显任何 CORS 头。
//   - Origin 为 "null"、含逗号的多来源串、或不是 http/https 的绝对来源 → 一律拒绝。
//     这些都是浏览器不会自发产生、或规范仅用于重定向/Service Worker 的形态，
//     原样回显等于给攻击者一个任意来源的跨域读入口。
//   - 命中白名单中的具体来源 → 回显该来源（大小写不敏感）。
//   - 白名单含 "*" → 仅在不携带凭证时返回 "*"；携带凭证时通配不可用，返回空串
//     让浏览器拒绝这次跨域读取，而不是把请求来源原样反射回去。
func resolveAllowedOrigin(origin string, allowedOrigins []string, allowCredentials bool) string {
	origin = strings.TrimSpace(origin)
	if origin == "" || len(allowedOrigins) == 0 {
		return ""
	}
	if !isValidRequestOrigin(origin) {
		return ""
	}
	// 先找精确匹配：白名单里同时写了 "*" 和具体来源时，具体来源必须优先命中，
	// 否则一次通配符判定就会让后面的具体条目永远失效。
	hasWildcard := false
	for _, allowed := range allowedOrigins {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if allowed == "*" {
			hasWildcard = true
			continue
		}
		if strings.EqualFold(allowed, origin) {
			return origin
		}
	}
	// 未精确命中时才考虑通配符，且仅在不需要凭证时可用。
	if hasWildcard && !allowCredentials {
		return "*"
	}
	return ""
}

// isValidRequestOrigin 校验 Origin 头是否为可安全回显的单一来源。
//
// 必须同时满足：是 http/https 绝对来源、不含逗号（拒绝多来源列表）、
// 且能被 net/url 解析出主机名。像 "null"、"https://evil.example, https://evil2.example"
// 这类值都是跨域攻击的典型投递形态，不能进入白名单比较之后还回显出去。
func isValidRequestOrigin(origin string) bool {
	if origin == "null" {
		return false
	}
	if strings.ContainsAny(origin, ",\x00") {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	// Origin 不该带路径、查询或片段；带上了说明不是浏览器生成的合法值。
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return true
}

// RequestIDMiddleware 请求 ID 中间件
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(requestIDHeader))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Set(requestIDKey, requestID)
		c.Writer.Header().Set(requestIDHeader, requestID)
		c.Next()
	}
}

// LoggerMiddleware 结构化请求日志中间件
func LoggerMiddleware(logger *zap.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = zap.L()
	}
	sugar := logger.Sugar()
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		log := sugar.With(
			"request_id", getRequestID(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
		if len(c.Errors) > 0 {
			log.Errorw("request", "errors", c.Errors.String())
			return
		}
		log.Infow("request")
	}
}

func getRequestID(c *gin.Context) string {
	value, ok := c.Get(requestIDKey)
	if !ok {
		return ""
	}
	if requestID, ok := value.(string); ok {
		return requestID
	}
	return ""
}

// JWTAuthMiddleware JWT 鉴权中间件
func JWTAuthMiddleware(secretKey string, adminRepo admincontract.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if secretKey == "" {
			msg := i18n.T(i18n.ResolveLocale(c), "error.jwt_secret_missing")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		if adminRepo == nil {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		authHeader := c.GetHeader(authHeaderKey)
		if authHeader == "" {
			msg := i18n.T(i18n.ResolveLocale(c), "error.auth_header_missing")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == authSchemeBearer) {
			msg := i18n.T(i18n.ResolveLocale(c), "error.auth_header_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		tokenString := parts[1]
		parser := newHS256JWTParser()
		claims := &adminauthapp.JWTClaims{}
		token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(secretKey), nil
		})
		if err != nil || !token.Valid || claims.AdminID == 0 {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		if !jwttoken.IsAccessType(claims.Typ) {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		if cached, hit, cacheErr := cache.GetAdminAuthState(c.Request.Context(), claims.AdminID); cacheErr == nil && hit && cached != nil {
			if claims.TokenVersion != cached.TokenVersion || !isIssuedAfterInvalidBeforeUnix(claims.IssuedAt, cached.TokenInvalidBefore) {
				msg := i18n.T(i18n.ResolveLocale(c), "error.token_revoked")
				response.Unauthorized(c, msg)
				c.Abort()
				return
			}
			c.Set("admin_id", claims.AdminID)
			c.Set("username", claims.Username)
			c.Set(adminIsSuperContextKey, cached.IsSuper)
			c.Next()
			return
		}

		admin, err := adminRepo.GetByID(claims.AdminID)
		if err != nil || admin == nil {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		if claims.TokenVersion != admin.TokenVersion || !isIssuedAfterInvalidBefore(claims.IssuedAt, admin.TokenInvalidBefore) {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_revoked")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		_ = cache.SetAdminAuthState(c.Request.Context(), cache.BuildAdminAuthState(admin))

		c.Set("admin_id", claims.AdminID)
		c.Set("username", claims.Username)
		c.Set(adminIsSuperContextKey, admin.IsSuper)
		c.Next()
	}
}

// AdminRBACMiddleware 管理端 RBAC 鉴权中间件
func AdminRBACMiddleware(authzService *authz.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authzService == nil {
			logger.Errorw("admin_rbac_service_unavailable")
			msg := i18n.T(i18n.ResolveLocale(c), "error.unauthorized")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		if isSuper, ok := c.Get(adminIsSuperContextKey); ok {
			if superValue, typeOK := isSuper.(bool); typeOK && superValue {
				c.Next()
				return
			}
		}

		adminIDRaw, exists := c.Get("admin_id")
		if !exists {
			msg := i18n.T(i18n.ResolveLocale(c), "error.unauthorized")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		var adminID uint
		switch value := adminIDRaw.(type) {
		case uint:
			adminID = value
		case int:
			if value > 0 {
				adminID = uint(value)
			}
		case float64:
			if value > 0 {
				adminID = uint(value)
			}
		}
		if adminID == 0 {
			msg := i18n.T(i18n.ResolveLocale(c), "error.unauthorized")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		resource := c.FullPath()
		if strings.TrimSpace(resource) == "" {
			resource = c.Request.URL.Path
		}

		allowed, err := authzService.EnforceAdmin(adminID, resource, c.Request.Method)
		if err != nil {
			logger.Errorw("admin_rbac_enforce_failed",
				"admin_id", adminID,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"error", err,
			)
			msg := i18n.T(i18n.ResolveLocale(c), "error.unauthorized")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		if !allowed {
			logger.Warnw("admin_rbac_permission_denied",
				"admin_id", adminID,
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"resource", authz.NormalizeObject(resource),
			)
			msg := i18n.T(i18n.ResolveLocale(c), "error.forbidden")
			response.Forbidden(c, msg)
			c.Abort()
			return
		}

		c.Next()
	}
}

// UserJWTAuthMiddleware 用户 JWT 鉴权中间件
func UserJWTAuthMiddleware(secretKey string, userRepo usercontract.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if secretKey == "" {
			msg := i18n.T(i18n.ResolveLocale(c), "error.jwt_secret_missing")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		if userRepo == nil {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		authHeader := c.GetHeader(authHeaderKey)
		if authHeader == "" {
			msg := i18n.T(i18n.ResolveLocale(c), "error.auth_header_missing")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == authSchemeBearer) {
			msg := i18n.T(i18n.ResolveLocale(c), "error.auth_header_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		tokenString := parts[1]
		parser := newHS256JWTParser()
		claims := &userauthapp.UserJWTClaims{}
		token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(secretKey), nil
		})
		if err != nil || !token.Valid || claims.UserID == 0 {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		if !jwttoken.IsAccessType(claims.Typ) {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}

		if cached, hit, cacheErr := cache.GetUserAuthState(c.Request.Context(), claims.UserID); cacheErr == nil && hit && cached != nil {
			if !isActiveUserStatus(cached.Status) {
				msg := i18n.T(i18n.ResolveLocale(c), "error.user_disabled")
				response.Unauthorized(c, msg)
				c.Abort()
				return
			}
			if claims.TokenVersion != cached.TokenVersion || !isIssuedAfterInvalidBeforeUnix(claims.IssuedAt, cached.TokenInvalidBefore) {
				msg := i18n.T(i18n.ResolveLocale(c), "error.token_revoked")
				response.Unauthorized(c, msg)
				c.Abort()
				return
			}
			c.Set("user_id", claims.UserID)
			c.Set("user_email", claims.Email)
			c.Next()
			return
		}

		user, err := userRepo.GetByID(claims.UserID)
		if err != nil || user == nil {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_invalid")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		if !isActiveUserStatus(user.Status) {
			msg := i18n.T(i18n.ResolveLocale(c), "error.user_disabled")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		if claims.TokenVersion != user.TokenVersion || !isIssuedAfterInvalidBefore(claims.IssuedAt, user.TokenInvalidBefore) {
			msg := i18n.T(i18n.ResolveLocale(c), "error.token_revoked")
			response.Unauthorized(c, msg)
			c.Abort()
			return
		}
		_ = cache.SetUserAuthState(c.Request.Context(), cache.BuildUserAuthState(user))

		c.Set("user_id", claims.UserID)
		c.Set("user_email", claims.Email)
		c.Next()
	}
}

func isIssuedAfterInvalidBefore(issuedAt *jwt.NumericDate, invalidBefore *time.Time) bool {
	if invalidBefore == nil {
		return true
	}
	if issuedAt == nil {
		return false
	}
	return issuedAt.Time.Unix() >= invalidBefore.Unix()
}

func isIssuedAfterInvalidBeforeUnix(issuedAt *jwt.NumericDate, invalidBeforeUnix int64) bool {
	if invalidBeforeUnix <= 0 {
		return true
	}
	if issuedAt == nil {
		return false
	}
	return issuedAt.Time.Unix() >= invalidBeforeUnix
}

func newHS256JWTParser() *jwt.Parser {
	return jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
}

func isActiveUserStatus(status string) bool {
	return strings.ToLower(strings.TrimSpace(status)) == constants.UserStatusActive
}

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
