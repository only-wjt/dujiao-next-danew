package productdomain

import (
	"errors"
	"net/url"
	"strings"
)

// ErrRedeemURLInvalid 表示兑换地址不是可用的 https 模板。
var ErrRedeemURLInvalid = errors.New("redeem url invalid")

const redeemURLPlaceholder = "{code}"
const maxRedeemURLLen = 500

// NormalizeRedeemURL 校验商品兑换地址。空字符串表示不提供跳转。
// 地址必须是 https，且恰好包含一次 {code}，由前台替换成这一行卡密。
func NormalizeRedeemURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > maxRedeemURLLen || strings.Count(raw, redeemURLPlaceholder) != 1 {
		return "", ErrRedeemURLInvalid
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", ErrRedeemURLInvalid
	}
	if strings.Contains(parsed.Host, redeemURLPlaceholder) {
		return "", ErrRedeemURLInvalid
	}
	return raw, nil
}
