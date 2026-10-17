package danewcdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) (*Client, cdkissuecontract.Credentials) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(server.Client().Transport), cdkissuecontract.Credentials{
		BaseURL:   server.URL + "/api/v1/cardnet",
		AppSecret: "cn_live_test",
	}
}

func TestIssueSendsHeadersAndParsesCodes(t *testing.T) {
	client, credentials := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cardnet/cdks/issue" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cn_live_test" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Idempotency-Key") != "dj-A1-1-0" {
			t.Fatalf("idempotency key = %q", r.Header.Get("Idempotency-Key"))
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["plan"] != "pro_5x" || body["payment_country"] != "PH" || body["count"].(float64) != 2 ||
			body["external_order_no"] != "DJA1" || body["external_ref"] != "item:1:chunk:0" {
			t.Fatalf("unexpected body %v", body)
		}
		_, _ = w.Write([]byte(`{"request_id":"CNR1","status":"succeeded","count":2,"replayed":true,"issued":[
			{"id":11,"code":"GPTD-AAAA","plan":"pro_5x","fee_amount_minor":15},
			{"id":12,"code":"GPTD-BBBB","plan":"pro_5x","fee_amount_minor":15}]}`))
	})

	result, err := client.Issue(context.Background(), credentials, cdkissuecontract.IssueCommand{
		Plan: "PRO_5X", Count: 2, PaymentCountry: "ph", IdempotencyKey: "dj-A1-1-0",
		OrderNo: "DJA1", ExternalRef: "item:1:chunk:0",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !result.Replayed || result.ProviderRequestID != "CNR1" || len(result.Codes) != 2 ||
		result.Codes[1].ID != 12 || result.Codes[0].Code != "GPTD-AAAA" || result.Codes[0].FeeAmountMinor != 15 {
		t.Fatalf("unexpected result %+v", result)
	}
}

func TestIssueClassifiesErrors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		transient bool
		code      string
	}{
		{"rejected", 422, `{"error_code":"UPSTREAM_REJECTED","message":"余额不足"}`, false, "UPSTREAM_REJECTED"},
		{"not allowed", 403, `{"error_code":"PLAN_NOT_ALLOWED","message":"x"}`, false, "PLAN_NOT_ALLOWED"},
		{"key reused", 422, `{"error_code":"IDEMPOTENCY_KEY_REUSED","message":"x"}`, false, "IDEMPOTENCY_KEY_REUSED"},
		{"in flight", 409, `{"error_code":"REQUEST_IN_FLIGHT","message":"x"}`, true, "REQUEST_IN_FLIGHT"},
		{"rate limited", 429, `{"error_code":"RATE_LIMITED","message":"x"}`, true, "RATE_LIMITED"},
		{"pending", 503, `{"error_code":"UPSTREAM_PENDING","message":"x"}`, true, "UPSTREAM_PENDING"},
		{"review", 502, `{"error_code":"UPSTREAM_COUNT_MISMATCH","message":"x"}`, true, "UPSTREAM_COUNT_MISMATCH"},
		{"html gateway", 502, `<html>bad gateway</html>`, true, ""},
		{"html 404", 404, `<html>not found</html>`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, credentials := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := client.Issue(context.Background(), credentials, cdkissuecontract.IssueCommand{Plan: "plus", Count: 1, IdempotencyKey: "k"})
			var gatewayErr *cdkissuecontract.GatewayError
			if !errors.As(err, &gatewayErr) {
				t.Fatalf("want GatewayError, got %v", err)
			}
			if gatewayErr.Transient != tc.transient || gatewayErr.ErrorCode != tc.code || gatewayErr.HTTPStatus != tc.status {
				t.Fatalf("unexpected error %+v", gatewayErr)
			}
		})
	}
}

func TestIssueUnconfirmedSuccessIsTransient(t *testing.T) {
	for _, body := range []string{`not json`, `{"status":"pending","issued":[]}`} {
		client, credentials := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		})
		_, err := client.Issue(context.Background(), credentials, cdkissuecontract.IssueCommand{Plan: "plus", Count: 1, IdempotencyKey: "k"})
		var gatewayErr *cdkissuecontract.GatewayError
		if !errors.As(err, &gatewayErr) || !gatewayErr.Transient {
			t.Fatalf("%q: want transient error, got %v", body, err)
		}
	}
}

func TestIssueRequiresConfig(t *testing.T) {
	client := New(nil)
	_, err := client.Issue(context.Background(), cdkissuecontract.Credentials{}, cdkissuecontract.IssueCommand{Plan: "plus", Count: 1, IdempotencyKey: "k"})
	if !errors.Is(err, cdkissuecontract.ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestOverviewParsesClientLimits(t *testing.T) {
	client, credentials := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cardnet/overview" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"issuable":true,
			"balance":{"balance":"100.00","spendable_balance":"80.00","account_reserve_amount":"20.00"},
			"plans":[{"key":"pro_5x","label":"Pro 5x","purchasable":true,"service_fee_usd_minor":15,"sort_order":2},
			         {"key":"plus","label":"Plus","purchasable":true,"service_fee_usd_minor":10,"sort_order":1}],
			"payment_regions":[{"country":"PH","currency":"PHP"}],
			"client":{"allowed_plans":["plus","pro_5x"],"allowed_countries":["PH"],"daily_code_limit":200,"issued_today":3}}`))
	})
	overview, err := client.Overview(context.Background(), credentials)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if !overview.CanIssue() || overview.Balance.SpendableBalance.String() != "80" || len(overview.Plans) != 2 ||
		overview.Plans[0].Key != "plus" || overview.Client == nil || overview.Client.IssuedToday != 3 {
		t.Fatalf("unexpected overview %+v", overview)
	}
	if !overview.ClientAllows("plus", "PH") || !overview.ClientAllows("plus", "") ||
		overview.ClientAllows("plus", "US") || overview.ClientAllows("go", "") {
		t.Fatalf("client limits not applied")
	}
	// 授权地区为空：与 danew_cdk 同口径，只允许默认地区 PH（显式或省略）。
	overview.Client.AllowedCountries = nil
	if !overview.ClientAllows("plus", "PH") || !overview.ClientAllows("plus", "") || overview.ClientAllows("plus", "US") {
		t.Fatalf("empty country grant must mean PH only")
	}
}

func TestOverviewNotIssuable(t *testing.T) {
	client, credentials := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuable":false,"message":"主台熔断中","plans":[],"payment_regions":[],"client":{"allowed_plans":[]}}`))
	})
	overview, err := client.Overview(context.Background(), credentials)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if overview.CanIssue() || overview.Unavailable != "主台熔断中" {
		t.Fatalf("unexpected overview %+v", overview)
	}
}

func TestDisableCDKsMapsRevokeResult(t *testing.T) {
	calls := 0
	client, credentials := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/cardnet/cdks/revoke" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			IDs []uint `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.IDs) > maxRevokeBatch {
			t.Fatalf("batch too large: %d", len(body.IDs))
		}
		out := map[string]interface{}{"revoked": []uint{}, "failed": []map[string]interface{}{}}
		revoked := []uint{}
		failed := []map[string]interface{}{}
		for _, id := range body.IDs {
			if id%2 == 0 {
				failed = append(failed, map[string]interface{}{"id": id, "reason": "CONSUMED"})
			} else {
				revoked = append(revoked, id)
			}
		}
		out["revoked"], out["failed"] = revoked, failed
		_ = json.NewEncoder(w).Encode(out)
	})
	ids := make([]uint, 0, 150)
	for i := uint(1); i <= 150; i++ {
		ids = append(ids, i)
	}
	result, err := client.DisableCDKs(context.Background(), credentials, ids)
	if err != nil {
		t.Fatalf("DisableCDKs: %v", err)
	}
	if calls != 2 || len(result.Disabled) != 75 || len(result.Failed) != 75 {
		t.Fatalf("calls=%d disabled=%d failed=%d", calls, len(result.Disabled), len(result.Failed))
	}
}
