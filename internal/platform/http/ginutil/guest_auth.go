package ginutil

import (
	"encoding/base64"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	guestAuthorizationScheme  = "Guest "
	maxGuestAuthorizationSize = 4096
)

// GetGuestCredentials 从 Authorization: Guest <base64url(email\\npassword)> 读取游客订单凭证。
// 凭证不再接受 URL 查询参数，避免进入代理访问日志、浏览器历史和 Referer。
func GetGuestCredentials(c *gin.Context) (string, string, bool) {
	if c == nil {
		return "", "", false
	}
	value := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(value) <= len(guestAuthorizationScheme) || len(value) > maxGuestAuthorizationSize ||
		!strings.EqualFold(value[:len(guestAuthorizationScheme)], guestAuthorizationScheme) {
		return "", "", false
	}
	encoded := strings.TrimSpace(value[len(guestAuthorizationScheme):])
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(raw), "\n", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	email := strings.TrimSpace(parts[0])
	password := strings.TrimSpace(parts[1])
	if email == "" || password == "" || len(email) > 320 || len(password) > 256 {
		return "", "", false
	}
	return email, password, true
}

const guestLookupResultKey = "dj_guest_lookup_result"

// GuestLookupResult 是一次游客凭证校验的结果，供防爆破中间件计数。
type GuestLookupResult struct {
	Success bool
	OrderNo string
}

// MarkGuestLookup 记录本次请求的游客凭证是否校验通过（邮箱、订单密码、订单号匹配）。
// 只有凭证错误才应记为失败；参数缺失、系统错误等不计入。
func MarkGuestLookup(c *gin.Context, success bool, orderNo string) {
	if c == nil {
		return
	}
	c.Set(guestLookupResultKey, GuestLookupResult{Success: success, OrderNo: strings.TrimSpace(orderNo)})
}

// GuestLookupResultFrom 读取 MarkGuestLookup 写入的结果。
func GuestLookupResultFrom(c *gin.Context) (GuestLookupResult, bool) {
	if c == nil {
		return GuestLookupResult{}, false
	}
	value, ok := c.Get(guestLookupResultKey)
	if !ok {
		return GuestLookupResult{}, false
	}
	result, ok := value.(GuestLookupResult)
	return result, ok
}
