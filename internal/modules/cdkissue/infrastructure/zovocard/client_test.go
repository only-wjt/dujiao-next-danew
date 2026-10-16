package zovocard

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
		BaseURL:   server.URL + "/openapi/v1",
		AppID:     "ak_test",
		AppSecret: "sk_test",
	}
}

func TestIssueSendsHeadersAndParsesCodes(t *testing.T) {
	client, credentials := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v1/gpt-direct/cdks" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "sk_test" || r.Header.Get("X-App-Id") != "ak_test" {
			t.Fatalf("missing auth headers")
		}
		if r.Header.Get("Idempotency-Key") != "dj-A1-1-0" {
			t.Fatalf("idempotency key = %q", r.Header.Get("Idempotency-Key"))
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["plan"] != "pro_5x" || body["funding_confirmed"] != true || body["payment_country"] != "PH" || body["count"].(float64) != 2 {
			t.Fatalf("unexpected body %v", body)
		}
		w.Header().Set("Idempotent-Replayed", "true")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"requested":2,"issued":[
			{"id":11,"code":"ZC5X-AAAA","plan":"pro_5x","fee_amount_minor":15},
			{"id":12,"code":"ZC5X-BBBB","plan":"pro_5x","fee_amount_minor":15}]}}`))
	})

	result, err := client.Issue(context.Background(), credentials, cdkissuecontract.IssueCommand{
		Plan: "PRO_5X", Count: 2, PaymentCountry: "ph", IdempotencyKey: "dj-A1-1-0",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !result.Replayed || len(result.Codes) != 2 || result.Codes[1].ID != 12 || result.Codes[0].FeeAmountMinor != 15 {
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
		{"insufficient balance is final", 400, `{"code":400,"error_code":"insufficient_balance","msg":"余额不足"}`, false, "insufficient_balance"},
		{"auth failure is final", 401, `{"code":401,"msg":"unauthorized"}`, false, ""},
		{"idempotency in progress retries", 409, `{"code":409,"msg":"processing"}`, true, ""},
		{"rate limit retries", 429, `{"code":429,"msg":"slow down"}`, true, ""},
		{"channel unavailable retries", 503, `{"code":503,"error_code":"channel_unavailable","msg":"x"}`, true, "channel_unavailable"},
		{"html gateway page retries", 502, `<html>bad gateway</html>`, true, ""},
		{"business code in 200 is final", 200, `{"code":400,"error_code":"plan_not_open","msg":"x"}`, false, "plan_not_open"},
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
				t.Fatalf("expected GatewayError, got %v", err)
			}
			if gatewayErr.Transient != tc.transient || gatewayErr.ErrorCode != tc.code {
				t.Fatalf("got transient=%v code=%q", gatewayErr.Transient, gatewayErr.ErrorCode)
			}
		})
	}
}

func TestIssueNetworkErrorIsTransient(t *testing.T) {
	client := New(nil)
	_, err := client.Issue(context.Background(), cdkissuecontract.Credentials{BaseURL: "http://127.0.0.1:1/openapi/v1", AppSecret: "sk_x"},
		cdkissuecontract.IssueCommand{Plan: "plus", Count: 1, IdempotencyKey: "k"})
	var gatewayErr *cdkissuecontract.GatewayError
	if !errors.As(err, &gatewayErr) || !gatewayErr.Transient {
		t.Fatalf("expected transient gateway error, got %v", err)
	}
}

func TestOverviewParsesRegistryAndBalance(t *testing.T) {
	client, credentials := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi/v1/balance":
			_, _ = w.Write([]byte(`{"code":0,"data":{"balance":"312.50","spendable_balance":292.5,"account_reserve_amount":"20"}}`))
		case "/openapi/v1/gpt-direct/plans":
			_, _ = w.Write([]byte(`{"code":0,"data":{
				"plans":{"plus":{"label":"Plus","enabled":true,"service_fee_usd_minor":10},"pro_50x":{"enabled":false}},
				"registry":[
					{"key":"pro_50x","label":"Pro 50x","sort_order":4,"purchasable":true},
					{"key":"plus","label":"Plus","sort_order":1,"purchasable":true}],
				"payment_regions":[{"country":"ph","currency":"php"},{"country":"US","currency":"USD"}]}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	overview, err := client.Overview(context.Background(), credentials)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if overview.Balance.SpendableBalance.String() != "292.5" {
		t.Fatalf("spendable = %s", overview.Balance.SpendableBalance)
	}
	if len(overview.Plans) != 2 || overview.Plans[0].Key != "plus" || overview.Plans[0].ServiceFeeUSDMinor != 10 {
		t.Fatalf("plans = %+v", overview.Plans)
	}
	if overview.Plans[1].Purchasable {
		t.Fatalf("pro_50x disabled in plans must not be purchasable")
	}
	if !overview.SupportsCountry("US") || overview.SupportsCountry("JP") {
		t.Fatalf("regions = %+v", overview.PaymentRegions)
	}
}

func TestDisableCDKsBatches(t *testing.T) {
	calls := 0
	client, credentials := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			IDs []uint `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.IDs) > maxDisableBatch {
			t.Fatalf("batch too large: %d", len(body.IDs))
		}
		payload, _ := json.Marshal(map[string]interface{}{"code": 0, "data": map[string]interface{}{"disabled": body.IDs, "failed": []uint{}}})
		_, _ = w.Write(payload)
	})
	ids := make([]uint, 150)
	for i := range ids {
		ids[i] = uint(i + 1)
	}
	result, err := client.DisableCDKs(context.Background(), credentials, ids)
	if err != nil {
		t.Fatalf("DisableCDKs: %v", err)
	}
	if calls != 2 || len(result.Disabled) != 150 {
		t.Fatalf("calls=%d disabled=%d", calls, len(result.Disabled))
	}
}
