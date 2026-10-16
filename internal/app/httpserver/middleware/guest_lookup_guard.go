package middleware

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	captchacontract "github.com/dujiao-next/internal/modules/captcha/contract"
	"github.com/dujiao-next/internal/platform/attemptguard"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// 游客查单防爆破策略。计数按失败次数，窗口从第一次失败开始。
const (
	guestLookupWindow = 30 * time.Minute
	// 同一订单号连续失败达到此次数后锁定（不论换多少个 IP）。
	guestLookupOrderLimit = 5
	// 同一邮箱失败达到此次数后锁定。
	guestLookupEmailLimit = 10
	// 同一 IP 失败达到此次数后锁定。
	guestLookupIPLimit = 30
	// 同一 IP 失败达到此次数后要求人机验证（后台开启了该验证码场景时）。
	guestLookupCaptchaAfter = 1
)

var (
	errGuestLookupLocked          = errors.New("guest lookup locked")
	errGuestLookupCaptchaRequired = errors.New("guest lookup captcha required")
	errGuestLookupCaptchaInvalid  = errors.New("guest lookup captcha invalid")
)

// GuestLookupCaptchaVerifier 校验游客查单场景的验证码；场景未开启时应直接返回 nil。
type GuestLookupCaptchaVerifier interface {
	Verify(scene string, payload captchacontract.VerifyPayload, clientIP string) error
}

// GuestLookupLockNotifier 在某个维度刚被锁定时调用，用于告警；可为空。
type GuestLookupLockNotifier func(kind, subject string, failures int64)

// GuestLookupGuard 统计游客凭证校验失败，按订单号、邮箱、IP 三个维度锁定，
// 并在 IP 出现失败后要求人机验证。
type GuestLookupGuard struct {
	failures *attemptguard.Counter
	verified *attemptguard.Counter
	captcha  GuestLookupCaptchaVerifier
	onLock   GuestLookupLockNotifier
}

// NewGuestLookupGuard 创建防爆破守卫；redisClient 为空时使用进程内计数。
func NewGuestLookupGuard(redisClient *redis.Client, prefix string, captcha GuestLookupCaptchaVerifier, onLock GuestLookupLockNotifier) *GuestLookupGuard {
	return &GuestLookupGuard{
		failures: attemptguard.New(redisClient, prefix+":guest_lookup:fail", guestLookupWindow),
		verified: attemptguard.New(redisClient, prefix+":guest_lookup:verified", guestLookupWindow),
		captcha:  captcha,
		onLock:   onLock,
	}
}

func orderKey(orderNo string) string { return "order:" + strings.ToLower(strings.TrimSpace(orderNo)) }
func emailKey(email string) string   { return "email:" + strings.ToLower(strings.TrimSpace(email)) }
func ipKey(ip string) string         { return "ip:" + strings.TrimSpace(ip) }
func verifiedKey(ip, email string) string {
	return strings.TrimSpace(ip) + "|" + strings.ToLower(strings.TrimSpace(email))
}

func (g *GuestLookupGuard) allow(ctx context.Context, ip, email, orderNo string, payload captchacontract.VerifyPayload) error {
	if orderNo != "" && g.failures.Count(ctx, orderKey(orderNo)) >= guestLookupOrderLimit {
		return errGuestLookupLocked
	}
	if g.failures.Count(ctx, emailKey(email)) >= guestLookupEmailLimit {
		return errGuestLookupLocked
	}
	ipFailures := g.failures.Count(ctx, ipKey(ip))
	if ipFailures >= guestLookupIPLimit {
		return errGuestLookupLocked
	}
	if ipFailures < guestLookupCaptchaAfter || g.captcha == nil {
		return nil
	}
	if g.verified.Count(ctx, verifiedKey(ip, email)) > 0 {
		return nil
	}
	if err := g.captcha.Verify(constants.CaptchaSceneGuestOrderLookup, payload, ip); err != nil {
		if errors.Is(err, captchacontract.ErrRequired) {
			return errGuestLookupCaptchaRequired
		}
		return errGuestLookupCaptchaInvalid
	}
	// 通过一次验证后，同一 IP 查同一邮箱在窗口内不再重复要求（订单详情会自动轮询刷新）。
	g.verified.Set(ctx, verifiedKey(ip, email))
	return nil
}

func (g *GuestLookupGuard) recordFailure(ctx context.Context, ip, email, orderNo string) {
	check := func(kind, subject, key string, limit int64) {
		count := g.failures.Increment(ctx, key)
		if count == limit {
			logger.Warnw("guest_lookup_locked", "kind", kind, "failures", count)
			if g.onLock != nil {
				g.onLock(kind, subject, count)
			}
		}
	}
	if orderNo != "" {
		check("order", orderNo, orderKey(orderNo), guestLookupOrderLimit)
	}
	check("email", maskEmail(email), emailKey(email), guestLookupEmailLimit)
	check("ip", ip, ipKey(ip), guestLookupIPLimit)
	g.verified.Reset(ctx, verifiedKey(ip, email))
}

func (g *GuestLookupGuard) recordSuccess(ctx context.Context, email, orderNo string) {
	// 凭证正确说明不是在猜这个订单/邮箱，清掉对应计数；IP 计数保留，防止穿插正确查询绕过验证。
	if orderNo != "" {
		g.failures.Reset(ctx, orderKey(orderNo))
	}
	g.failures.Reset(ctx, emailKey(email))
}

// Middleware 应用于所有携带游客凭证的接口。处理器通过 ginutil.MarkGuestLookup 报告凭证是否正确。
func (g *GuestLookupGuard) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if g == nil {
			c.Next()
			return
		}
		email, _, ok := ginutil.GetGuestCredentials(c)
		if !ok {
			c.Next()
			return
		}
		ip := c.ClientIP()
		orderNo := strings.TrimSpace(c.Param("order_no"))
		if orderNo == "" {
			orderNo = strings.TrimSpace(c.Query("order_no"))
		}
		payload := captchacontract.VerifyPayload{
			CaptchaID:      strings.TrimSpace(c.GetHeader("X-Captcha-Id")),
			CaptchaCode:    strings.TrimSpace(c.GetHeader("X-Captcha-Code")),
			TurnstileToken: strings.TrimSpace(c.GetHeader("X-Captcha-Token")),
		}
		ctx := c.Request.Context()
		if err := g.allow(ctx, ip, email, orderNo, payload); err != nil {
			switch {
			case errors.Is(err, errGuestLookupLocked):
				ginutil.RespondError(c, response.CodeTooManyRequests, "error.guest_lookup_locked", nil)
			case errors.Is(err, errGuestLookupCaptchaRequired):
				ginutil.RespondError(c, response.CodeCaptchaRequired, "error.captcha_required", nil)
			default:
				ginutil.RespondError(c, response.CodeCaptchaRequired, "error.captcha_invalid", nil)
			}
			c.Abort()
			return
		}

		c.Next()

		result, marked := ginutil.GuestLookupResultFrom(c)
		if !marked {
			return
		}
		if result.OrderNo != "" {
			orderNo = result.OrderNo
		}
		if result.Success {
			g.recordSuccess(ctx, email, orderNo)
			return
		}
		g.recordFailure(ctx, ip, email, orderNo)
	}
}

// maskEmail 告警中只保留邮箱首字符和域名。
func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}
