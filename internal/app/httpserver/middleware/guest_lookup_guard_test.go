package middleware

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	captchacontract "github.com/dujiao-next/internal/modules/captcha/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"

	"github.com/gin-gonic/gin"
)

type stubLookupCaptcha struct {
	enabled bool
	calls   int
}

func (s *stubLookupCaptcha) Verify(_ string, payload captchacontract.VerifyPayload, _ string) error {
	s.calls++
	if !s.enabled {
		return nil
	}
	if payload.TurnstileToken == "" {
		return captchacontract.ErrRequired
	}
	if payload.TurnstileToken != "ok" {
		return captchacontract.ErrInvalid
	}
	return nil
}

type lockEvent struct {
	kind    string
	subject string
}

// newGuardTestRouter 模拟游客查单：密码为 "right" 时凭证正确。
func newGuardTestRouter(guard *GuestLookupGuard) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/guest/orders/:order_no", guard.Middleware(), func(c *gin.Context) {
		_, password, _ := ginutil.GetGuestCredentials(c)
		orderNo := c.Param("order_no")
		if password != "right" {
			ginutil.MarkGuestLookup(c, false, orderNo)
			c.JSON(http.StatusOK, gin.H{"status_code": 404})
			return
		}
		ginutil.MarkGuestLookup(c, true, orderNo)
		c.JSON(http.StatusOK, gin.H{"status_code": 0})
	})
	return router
}

func guestLookup(router *gin.Engine, ip, email, password, orderNo, token string) int {
	request := httptest.NewRequest(http.MethodGet, "/guest/orders/"+orderNo, nil)
	request.RemoteAddr = ip + ":1234"
	credential := base64.RawURLEncoding.EncodeToString([]byte(email + "\n" + password))
	request.Header.Set("Authorization", "Guest "+credential)
	if token != "" {
		request.Header.Set("X-Captcha-Token", token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	var body struct {
		StatusCode int `json:"status_code"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	return body.StatusCode
}

func TestGuestLookupGuardLocksOrderAcrossIPs(t *testing.T) {
	var events []lockEvent
	guard := NewGuestLookupGuard(nil, "test", nil, func(kind, subject string, _ int64) {
		events = append(events, lockEvent{kind, subject})
	})
	router := newGuardTestRouter(guard)

	// 攻击者每次换一个 IP、换一个邮箱猜同一个订单号。
	for i := 0; i < guestLookupOrderLimit; i++ {
		ip := "10.0.0." + string(rune('1'+i))
		if code := guestLookup(router, ip, "attacker"+string(rune('a'+i))+"@x.com", "guess", "DJ1", ""); code != 404 {
			t.Fatalf("attempt %d: want 404, got %d", i, code)
		}
	}
	// 即使密码正确，被锁定的订单号也要等窗口过去。
	if code := guestLookup(router, "10.0.1.1", "owner@x.com", "right", "DJ1", ""); code != 429 {
		t.Fatalf("locked order must be refused, got %d", code)
	}
	if len(events) == 0 || events[0].kind != "order" {
		t.Fatalf("lock alert expected, got %+v", events)
	}
	// 其他订单不受影响。
	if code := guestLookup(router, "10.0.1.1", "owner@x.com", "right", "DJ2", ""); code != 0 {
		t.Fatalf("other order must be allowed, got %d", code)
	}
}

func TestGuestLookupGuardRequiresCaptchaAfterFailure(t *testing.T) {
	captcha := &stubLookupCaptcha{enabled: true}
	router := newGuardTestRouter(NewGuestLookupGuard(nil, "test", captcha, nil))

	// 首次查询不弹验证码。
	if code := guestLookup(router, "10.0.0.9", "a@x.com", "wrong", "DJ1", ""); code != 404 {
		t.Fatalf("first attempt must reach the handler, got %d", code)
	}
	if captcha.calls != 0 {
		t.Fatalf("captcha must not be checked before any failure")
	}
	if code := guestLookup(router, "10.0.0.9", "a@x.com", "right", "DJ1", ""); code != 428 {
		t.Fatalf("captcha must be required after a failure, got %d", code)
	}
	if code := guestLookup(router, "10.0.0.9", "a@x.com", "right", "DJ1", "bad"); code != 428 {
		t.Fatalf("invalid captcha must be refused, got %d", code)
	}
	if code := guestLookup(router, "10.0.0.9", "a@x.com", "right", "DJ1", "ok"); code != 0 {
		t.Fatalf("valid captcha must pass, got %d", code)
	}
	// 通过后同一 IP 查同一邮箱不再重复要求（订单详情自动刷新）。
	if code := guestLookup(router, "10.0.0.9", "a@x.com", "right", "DJ1", ""); code != 0 {
		t.Fatalf("verified session must not require captcha again, got %d", code)
	}
	// 新的失败会撤销已验证状态。
	_ = guestLookup(router, "10.0.0.9", "a@x.com", "wrong", "DJ1", "")
	if code := guestLookup(router, "10.0.0.9", "a@x.com", "right", "DJ1", ""); code != 428 {
		t.Fatalf("failure must revoke verification, got %d", code)
	}
}

func TestGuestLookupGuardSkipsCaptchaWhenSceneDisabled(t *testing.T) {
	captcha := &stubLookupCaptcha{enabled: false}
	router := newGuardTestRouter(NewGuestLookupGuard(nil, "test", captcha, nil))
	_ = guestLookup(router, "10.0.0.7", "a@x.com", "wrong", "DJ1", "")
	if code := guestLookup(router, "10.0.0.7", "a@x.com", "right", "DJ1", ""); code != 0 {
		t.Fatalf("disabled scene must not block, got %d", code)
	}
}

func TestGuestLookupGuardLocksIP(t *testing.T) {
	router := newGuardTestRouter(NewGuestLookupGuard(nil, "test", nil, nil))
	for i := 0; i < guestLookupIPLimit; i++ {
		_ = guestLookup(router, "10.0.0.8", fmt.Sprintf("u%d@example.com", i), "wrong", fmt.Sprintf("DJ%d", i), "")
	}
	if code := guestLookup(router, "10.0.0.8", "other@x.com", "right", "DJZZ", ""); code != 429 {
		t.Fatalf("ip must be locked, got %d", code)
	}
}

func TestMaskEmail(t *testing.T) {
	if got := maskEmail("alice@example.com"); got != "a***@example.com" {
		t.Fatalf("maskEmail = %s", got)
	}
	if got := maskEmail("bad"); got != "***" {
		t.Fatalf("maskEmail = %s", got)
	}
}
