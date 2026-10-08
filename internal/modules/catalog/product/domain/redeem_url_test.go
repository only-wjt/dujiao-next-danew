package productdomain

import "testing"

func TestNormalizeRedeemURL(t *testing.T) {
	ok, err := NormalizeRedeemURL("  https://card.example.com/recharge?code={code}  ")
	if err != nil || ok != "https://card.example.com/recharge?code={code}" {
		t.Fatalf("got %q err=%v", ok, err)
	}
	if got, err := NormalizeRedeemURL(""); err != nil || got != "" {
		t.Fatalf("empty got %q err=%v", got, err)
	}
	for _, raw := range []string{
		"http://card.example.com/recharge?code={code}",
		"https://card.example.com/recharge",
		"https://card.example.com/recharge?code={code}&again={code}",
		"https://user:pass@card.example.com/recharge?code={code}",
		"javascript:alert({code})",
		"https://{code}.example.com/recharge",
	} {
		if _, err := NormalizeRedeemURL(raw); err == nil {
			t.Fatalf("expected reject %s", raw)
		}
	}
}
