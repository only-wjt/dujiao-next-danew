// Package danewcdk 实现 danew_cdk 卡网发码接口网关（/api/v1/cardnet）。
//
// 码由 danew_cdk 的主台发出并落 danew_cdk 库，买家在 cdk 站兑换、查单。
// 错误语义与 ZovoCard 一致：4xx（409/429 除外）是明确拒绝，409/429/5xx 结果待定、
// 必须用同一个 Idempotency-Key 重试，因此复用 cdkissue 现有的重试状态机。
package danewcdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"

	"github.com/shopspring/decimal"
)

const (
	// issueTimeout 需覆盖 danew_cdk 调卡台的最长等待（60s）。
	issueTimeout      = 75 * time.Second
	defaultTimeout    = 15 * time.Second
	revokeTimeout     = 30 * time.Second
	maxResponseBytes  = 4 << 20
	maxRevokeBatch    = 100
	maxErrorMsgLength = 300
	userAgent         = "dujiao-next-cdk-issue/1"
)

// Client 是 danew_cdk 卡网接口客户端。
type Client struct {
	transport http.RoundTripper
}

var _ cdkissuecontract.Gateway = (*Client)(nil)

// New 创建客户端；transport 为空时使用默认传输。
func New(transport http.RoundTripper) *Client {
	return &Client{transport: transport}
}

type errorBody struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func (c *Client) do(ctx context.Context, credentials cdkissuecontract.Credentials, method, path string, body interface{}, idempotencyKey string, timeout time.Duration) ([]byte, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(credentials.BaseURL), "/")
	if baseURL == "" || strings.TrimSpace(credentials.AppSecret) == "" {
		return nil, cdkissuecontract.ErrNotConfigured
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(credentials.AppSecret))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}

	httpClient := &http.Client{Timeout: timeout, Transport: c.transport}
	resp, err := httpClient.Do(request)
	if err != nil {
		// 网络错误与超时：请求可能已到达，结果未定。
		return nil, &cdkissuecontract.GatewayError{Message: truncate(err.Error(), maxErrorMsgLength), Transient: true}
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return nil, &cdkissuecontract.GatewayError{HTTPStatus: resp.StatusCode, Message: "read response failed", Transient: true}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	var failure errorBody
	if err := json.Unmarshal(raw, &failure); err != nil || (failure.ErrorCode == "" && failure.Message == "") {
		// 非 JSON（网关/反代页面）：按状态码归类，消息只给状态码，不回显 HTML。
		return nil, &cdkissuecontract.GatewayError{
			HTTPStatus: resp.StatusCode,
			Message:    fmt.Sprintf("non-json response (http %d)", resp.StatusCode),
			Transient:  cdkissuecontract.ClassifyHTTPStatus(resp.StatusCode),
		}
	}
	return nil, &cdkissuecontract.GatewayError{
		HTTPStatus: resp.StatusCode,
		ErrorCode:  strings.TrimSpace(failure.ErrorCode),
		Message:    truncate(strings.TrimSpace(failure.Message), maxErrorMsgLength),
		Transient:  cdkissuecontract.ClassifyHTTPStatus(resp.StatusCode),
	}
}

type issueRequestBody struct {
	Plan            string `json:"plan"`
	Count           int    `json:"count"`
	PaymentCountry  string `json:"payment_country,omitempty"`
	ExternalOrderNo string `json:"external_order_no,omitempty"`
	ExternalRef     string `json:"external_ref,omitempty"`
}

type issueResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Count     int    `json:"count"`
	Replayed  bool   `json:"replayed"`
	Issued    []struct {
		ID             json.Number `json:"id"`
		Code           string      `json:"code"`
		Plan           string      `json:"plan"`
		FeeAmountMinor json.Number `json:"fee_amount_minor"`
	} `json:"issued"`
}

// Issue 调用 POST /cdks/issue。
func (c *Client) Issue(ctx context.Context, credentials cdkissuecontract.Credentials, command cdkissuecontract.IssueCommand) (*cdkissuecontract.IssueResult, error) {
	if command.Count <= 0 || command.Count > cdkissuedomain.MaxCodesPerRequest || strings.TrimSpace(command.Plan) == "" {
		return nil, cdkissuecontract.ErrInvalidInput
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return nil, cdkissuecontract.ErrInvalidInput
	}
	raw, err := c.do(ctx, credentials, http.MethodPost, "/cdks/issue", issueRequestBody{
		Plan:            cdkissuedomain.NormalizePlan(command.Plan),
		Count:           command.Count,
		PaymentCountry:  cdkissuedomain.NormalizeCountry(command.PaymentCountry),
		ExternalOrderNo: strings.TrimSpace(command.OrderNo),
		ExternalRef:     strings.TrimSpace(command.ExternalRef),
	}, command.IdempotencyKey, issueTimeout)
	if err != nil {
		return nil, err
	}
	var data issueResponse
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil || data.Status != "succeeded" {
		// 2xx 但无法确认成功：可能已发码，交给原键重试取回结果。
		return nil, &cdkissuecontract.GatewayError{HTTPStatus: http.StatusOK, Message: "unexpected issue response", Transient: true}
	}
	result := &cdkissuecontract.IssueResult{Replayed: data.Replayed, ProviderRequestID: strings.TrimSpace(data.RequestID)}
	for _, item := range data.Issued {
		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}
		id, err := item.ID.Int64()
		if err != nil || id <= 0 {
			continue
		}
		fee, _ := item.FeeAmountMinor.Int64()
		result.Codes = append(result.Codes, cdkissuecontract.IssuedCode{
			ID:             uint(id),
			Code:           code,
			Plan:           cdkissuedomain.NormalizePlan(item.Plan),
			FeeAmountMinor: fee,
		})
	}
	return result, nil
}

type overviewResponse struct {
	Issuable bool   `json:"issuable"`
	Message  string `json:"message"`
	Balance  *struct {
		Balance              json.RawMessage `json:"balance"`
		SpendableBalance     json.RawMessage `json:"spendable_balance"`
		AccountReserveAmount json.RawMessage `json:"account_reserve_amount"`
	} `json:"balance"`
	Plans []struct {
		Key                 string      `json:"key"`
		Label               string      `json:"label"`
		Purchasable         bool        `json:"purchasable"`
		ServiceFeeUSDMinor  json.Number `json:"service_fee_usd_minor"`
		SortOrder           int         `json:"sort_order"`
		CheckoutCurrency    string      `json:"checkout_currency"`
		CheckoutAmountMinor json.Number `json:"checkout_amount_minor"`
	} `json:"plans"`
	PaymentRegions []cdkissuedomain.PaymentRegion `json:"payment_regions"`
	Client         *cdkissuedomain.ClientLimits   `json:"client"`
}

// Overview 读取 GET /overview：主台余额、本客户端可发的套餐、付款地区与授权范围。
func (c *Client) Overview(ctx context.Context, credentials cdkissuecontract.Credentials) (*cdkissuedomain.Overview, error) {
	raw, err := c.do(ctx, credentials, http.MethodGet, "/overview", nil, "", defaultTimeout)
	if err != nil {
		return nil, err
	}
	var data overviewResponse
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return nil, &cdkissuecontract.GatewayError{HTTPStatus: http.StatusOK, Message: "decode overview failed", Transient: true}
	}
	issuable := data.Issuable
	overview := &cdkissuedomain.Overview{
		Plans:          make([]cdkissuedomain.Plan, 0, len(data.Plans)),
		PaymentRegions: data.PaymentRegions,
		CheckedAt:      time.Now(),
		Issuable:       &issuable,
		Unavailable:    strings.TrimSpace(data.Message),
		Client:         data.Client,
	}
	if overview.PaymentRegions == nil {
		overview.PaymentRegions = []cdkissuedomain.PaymentRegion{}
	}
	if data.Balance != nil {
		overview.Balance = cdkissuedomain.Balance{
			Balance:              rawDecimal(data.Balance.Balance),
			SpendableBalance:     rawDecimal(data.Balance.SpendableBalance),
			AccountReserveAmount: rawDecimal(data.Balance.AccountReserveAmount),
		}
	}
	for _, plan := range data.Plans {
		key := cdkissuedomain.NormalizePlan(plan.Key)
		if key == "" {
			continue
		}
		fee, _ := plan.ServiceFeeUSDMinor.Int64()
		checkout, _ := plan.CheckoutAmountMinor.Int64()
		overview.Plans = append(overview.Plans, cdkissuedomain.Plan{
			Key:                 key,
			Label:               strings.TrimSpace(plan.Label),
			Purchasable:         plan.Purchasable,
			ServiceFeeUSDMinor:  fee,
			SortOrder:           plan.SortOrder,
			CheckoutCurrency:    strings.TrimSpace(plan.CheckoutCurrency),
			CheckoutAmountMinor: checkout,
		})
	}
	cdkissuedomain.SortPlans(overview.Plans)
	return overview, nil
}

type revokeResponse struct {
	Revoked []json.Number `json:"revoked"`
	Failed  []struct {
		ID     json.Number `json:"id"`
		Reason string      `json:"reason"`
	} `json:"failed"`
}

// DisableCDKs 调用 POST /cdks/revoke 作废未兑换的码；单次最多 100 个，超过时分批。
// danew_cdk 优先删除退费，失败再停用，已兑换的码出现在 Failed 中。
func (c *Client) DisableCDKs(ctx context.Context, credentials cdkissuecontract.Credentials, ids []uint) (*cdkissuecontract.DisableResult, error) {
	result := &cdkissuecontract.DisableResult{Disabled: []uint{}, Failed: []uint{}}
	for start := 0; start < len(ids); start += maxRevokeBatch {
		end := start + maxRevokeBatch
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		raw, err := c.do(ctx, credentials, http.MethodPost, "/cdks/revoke", map[string]interface{}{
			"ids": batch, "reason": "order_refunded",
		}, "", revokeTimeout)
		if err != nil {
			if start == 0 {
				return nil, err
			}
			result.Failed = append(result.Failed, ids[start:]...)
			return result, nil
		}
		var data revokeResponse
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			result.Failed = append(result.Failed, batch...)
			continue
		}
		for _, id := range data.Revoked {
			if n, err := id.Int64(); err == nil && n > 0 {
				result.Disabled = append(result.Disabled, uint(n))
			}
		}
		for _, item := range data.Failed {
			if n, err := item.ID.Int64(); err == nil && n > 0 {
				result.Failed = append(result.Failed, uint(n))
			}
		}
	}
	return result, nil
}

func rawDecimal(raw json.RawMessage) decimal.Decimal {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return decimal.Zero
	}
	parsed, err := decimal.NewFromString(text)
	if err != nil {
		return decimal.Zero
	}
	return parsed
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
