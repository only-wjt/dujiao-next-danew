// Package zovocard 实现 ZovoCard 卡台 Open API 网关。
package zovocard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"

	"github.com/shopspring/decimal"
)

const (
	issueTimeout      = 60 * time.Second
	defaultTimeout    = 15 * time.Second
	maxResponseBytes  = 4 << 20
	maxDisableBatch   = 100
	maxErrorMsgLength = 300
	userAgent         = "dujiao-next-cdk-issue/1"
)

// Client 是 ZovoCard Open API 客户端。
type Client struct {
	transport http.RoundTripper
}

var _ cdkissuecontract.Gateway = (*Client)(nil)

// New 创建客户端；transport 为空时使用默认传输。
func New(transport http.RoundTripper) *Client {
	return &Client{transport: transport}
}

type envelope struct {
	Code      int             `json:"code"`
	Msg       string          `json:"msg"`
	Message   string          `json:"message"`
	ErrorCode string          `json:"error_code"`
	Data      json.RawMessage `json:"data"`
}

func (e envelope) message(fallback string) string {
	for _, candidate := range []string{e.Msg, e.Message} {
		if text := strings.TrimSpace(candidate); text != "" {
			return truncate(text, maxErrorMsgLength)
		}
	}
	return fallback
}

type response struct {
	data     json.RawMessage
	replayed bool
}

func (c *Client) do(ctx context.Context, credentials cdkissuecontract.Credentials, method, path string, body interface{}, idempotencyKey string, timeout time.Duration) (*response, error) {
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
	request.Header.Set("X-API-Key", strings.TrimSpace(credentials.AppSecret))
	if appID := strings.TrimSpace(credentials.AppID); appID != "" {
		request.Header.Set("X-App-Id", appID)
	}
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
		// 网络错误与超时：请求可能已到达卡台，结果未定。
		return nil, &cdkissuecontract.GatewayError{Message: truncate(err.Error(), maxErrorMsgLength), Transient: true}
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return nil, &cdkissuecontract.GatewayError{HTTPStatus: resp.StatusCode, Message: "read response failed", Transient: true}
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, &cdkissuecontract.GatewayError{
			HTTPStatus: resp.StatusCode,
			Message:    fmt.Sprintf("non-json response (http %d)", resp.StatusCode),
			Transient:  resp.StatusCode < 400 || cdkissuecontract.ClassifyHTTPStatus(resp.StatusCode),
		}
	}
	if resp.StatusCode >= 400 {
		return nil, &cdkissuecontract.GatewayError{
			HTTPStatus: resp.StatusCode,
			ErrorCode:  strings.TrimSpace(env.ErrorCode),
			Message:    env.message(resp.Status),
			Transient:  cdkissuecontract.ClassifyHTTPStatus(resp.StatusCode),
		}
	}
	if env.Code != 0 {
		// HTTP 2xx 但业务码非 0：按业务码归类，无法识别时视为明确拒绝。
		status := env.Code
		if status < 400 {
			status = http.StatusBadRequest
		}
		return nil, &cdkissuecontract.GatewayError{
			HTTPStatus: status,
			ErrorCode:  strings.TrimSpace(env.ErrorCode),
			Message:    env.message("business error"),
			Transient:  cdkissuecontract.ClassifyHTTPStatus(status),
		}
	}
	return &response{
		data:     env.Data,
		replayed: strings.EqualFold(strings.TrimSpace(resp.Header.Get("Idempotent-Replayed")), "true"),
	}, nil
}

type issueRequestBody struct {
	Plan             string `json:"plan"`
	Count            int    `json:"count"`
	FundingConfirmed bool   `json:"funding_confirmed"`
	PaymentCountry   string `json:"payment_country,omitempty"`
}

type issuedItem struct {
	ID             json.Number `json:"id"`
	Code           string      `json:"code"`
	Plan           string      `json:"plan"`
	FeeAmountMinor json.Number `json:"fee_amount_minor"`
}

// Issue 调用 POST /gpt-direct/cdks 发码。
func (c *Client) Issue(ctx context.Context, credentials cdkissuecontract.Credentials, command cdkissuecontract.IssueCommand) (*cdkissuecontract.IssueResult, error) {
	if command.Count <= 0 || command.Count > cdkissuedomain.MaxCodesPerRequest || strings.TrimSpace(command.Plan) == "" {
		return nil, cdkissuecontract.ErrInvalidInput
	}
	if strings.TrimSpace(command.IdempotencyKey) == "" {
		return nil, cdkissuecontract.ErrInvalidInput
	}
	resp, err := c.do(ctx, credentials, http.MethodPost, "/gpt-direct/cdks", issueRequestBody{
		Plan:             cdkissuedomain.NormalizePlan(command.Plan),
		Count:            command.Count,
		FundingConfirmed: true,
		PaymentCountry:   cdkissuedomain.NormalizeCountry(command.PaymentCountry),
	}, command.IdempotencyKey, issueTimeout)
	if err != nil {
		return nil, err
	}
	var data struct {
		Issued []issuedItem `json:"issued"`
	}
	decoder := json.NewDecoder(bytes.NewReader(resp.data))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		// 2xx 但解析失败：卡台可能已扣费，交给原键重试取回结果。
		return nil, &cdkissuecontract.GatewayError{HTTPStatus: http.StatusOK, Message: "decode issue response failed", Transient: true}
	}
	result := &cdkissuecontract.IssueResult{Replayed: resp.replayed}
	for _, item := range data.Issued {
		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}
		id, _ := strconv.ParseUint(item.ID.String(), 10, 64)
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

// Overview 读取余额与可售套餐。
func (c *Client) Overview(ctx context.Context, credentials cdkissuecontract.Credentials) (*cdkissuedomain.Overview, error) {
	balanceResp, err := c.do(ctx, credentials, http.MethodGet, "/balance", nil, "", defaultTimeout)
	if err != nil {
		return nil, err
	}
	balance, err := parseBalance(balanceResp.data)
	if err != nil {
		return nil, err
	}
	plansResp, err := c.do(ctx, credentials, http.MethodGet, "/gpt-direct/plans?product=gpt", nil, "", defaultTimeout)
	if err != nil {
		return nil, err
	}
	plans, regions, err := parsePlans(plansResp.data)
	if err != nil {
		return nil, err
	}
	return &cdkissuedomain.Overview{
		Balance:        balance,
		Plans:          plans,
		PaymentRegions: regions,
		CheckedAt:      time.Now(),
	}, nil
}

// DisableCDKs 批量停用未使用的码；单次最多 100 个，超过时分批。
func (c *Client) DisableCDKs(ctx context.Context, credentials cdkissuecontract.Credentials, ids []uint) (*cdkissuecontract.DisableResult, error) {
	result := &cdkissuecontract.DisableResult{Disabled: []uint{}, Failed: []uint{}}
	for start := 0; start < len(ids); start += maxDisableBatch {
		end := start + maxDisableBatch
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		resp, err := c.do(ctx, credentials, http.MethodPost, "/gpt-direct/cdks/batch-disable", map[string]interface{}{"ids": batch}, "", defaultTimeout)
		if err != nil {
			if start == 0 {
				return nil, err
			}
			result.Failed = append(result.Failed, ids[start:]...)
			return result, nil
		}
		var data struct {
			Disabled []json.Number `json:"disabled"`
			Failed   []json.Number `json:"failed"`
		}
		decoder := json.NewDecoder(bytes.NewReader(resp.data))
		decoder.UseNumber()
		if err := decoder.Decode(&data); err != nil {
			result.Failed = append(result.Failed, batch...)
			continue
		}
		result.Disabled = append(result.Disabled, numbersToUints(data.Disabled)...)
		result.Failed = append(result.Failed, numbersToUints(data.Failed)...)
	}
	return result, nil
}

func numbersToUints(values []json.Number) []uint {
	out := make([]uint, 0, len(values))
	for _, value := range values {
		if n, err := strconv.ParseUint(value.String(), 10, 64); err == nil {
			out = append(out, uint(n))
		}
	}
	return out
}

func parseBalance(raw json.RawMessage) (cdkissuedomain.Balance, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return cdkissuedomain.Balance{}, &cdkissuecontract.GatewayError{HTTPStatus: http.StatusOK, Message: "decode balance failed", Transient: true}
	}
	balance := cdkissuedomain.Balance{
		Balance:              decimalField(fields, "balance", "totalBalance"),
		SpendableBalance:     decimalField(fields, "spendable_balance", "spendableBalance"),
		AccountReserveAmount: decimalField(fields, "account_reserve_amount", "accountReserveAmount"),
	}
	if _, ok := firstField(fields, "spendable_balance", "spendableBalance"); !ok {
		balance.SpendableBalance = balance.Balance.Sub(balance.AccountReserveAmount)
	}
	return balance, nil
}

type registryEntry struct {
	Key                 string       `json:"key"`
	Label               string       `json:"label"`
	Purchasable         *bool        `json:"purchasable"`
	Enabled             *bool        `json:"enabled"`
	ServiceFeeUSDMinor  *json.Number `json:"service_fee_usd_minor"`
	SortOrder           int          `json:"sort_order"`
	CheckoutCurrency    string       `json:"checkout_currency"`
	CheckoutAmountMinor json.Number  `json:"checkout_amount_minor"`
}

func parsePlans(raw json.RawMessage) ([]cdkissuedomain.Plan, []cdkissuedomain.PaymentRegion, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, &cdkissuecontract.GatewayError{HTTPStatus: http.StatusOK, Message: "decode plans failed", Transient: true}
	}

	planConfigs := parsePlanConfigs(fields["plans"])

	var registry []registryEntry
	if value, ok := fields["registry"]; ok {
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		_ = decoder.Decode(&registry)
	}

	plans := make([]cdkissuedomain.Plan, 0)
	seen := make(map[string]bool)
	for _, entry := range registry {
		key := cdkissuedomain.NormalizePlan(entry.Key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		config := planConfigs[key]
		purchasable := config.enabled
		if entry.Purchasable != nil {
			purchasable = purchasable && *entry.Purchasable
		}
		if entry.Enabled != nil {
			purchasable = purchasable && *entry.Enabled
		}
		fee := config.fee
		if entry.ServiceFeeUSDMinor != nil {
			if parsed, err := entry.ServiceFeeUSDMinor.Int64(); err == nil {
				fee = parsed
			}
		}
		label := strings.TrimSpace(entry.Label)
		if label == "" {
			label = config.label
		}
		checkoutAmount, _ := entry.CheckoutAmountMinor.Int64()
		plans = append(plans, cdkissuedomain.Plan{
			Key:                 key,
			Label:               label,
			Purchasable:         purchasable,
			ServiceFeeUSDMinor:  fee,
			SortOrder:           entry.SortOrder,
			CheckoutCurrency:    strings.TrimSpace(entry.CheckoutCurrency),
			CheckoutAmountMinor: checkoutAmount,
		})
	}
	// 老版本卡台没有 registry 时直接使用 plans。
	if len(registry) == 0 {
		for key, config := range planConfigs {
			plans = append(plans, cdkissuedomain.Plan{
				Key:                key,
				Label:              config.label,
				Purchasable:        config.enabled,
				ServiceFeeUSDMinor: config.fee,
			})
		}
	}
	cdkissuedomain.SortPlans(plans)

	regions := make([]cdkissuedomain.PaymentRegion, 0)
	if value, ok := fields["payment_regions"]; ok {
		var items []cdkissuedomain.PaymentRegion
		if err := json.Unmarshal(value, &items); err == nil {
			for _, item := range items {
				country := cdkissuedomain.NormalizeCountry(item.Country)
				if country == "" {
					continue
				}
				regions = append(regions, cdkissuedomain.PaymentRegion{Country: country, Currency: strings.ToUpper(strings.TrimSpace(item.Currency))})
			}
		}
	}
	return plans, regions, nil
}

type planConfig struct {
	label   string
	enabled bool
	fee     int64
}

// parsePlanConfigs 兼容 plans 为对象（键为套餐）或数组两种结构。
func parsePlanConfigs(raw json.RawMessage) map[string]planConfig {
	out := make(map[string]planConfig)
	if len(raw) == 0 {
		return out
	}
	var asMap map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &asMap); err == nil {
		for key, item := range asMap {
			if normalized := cdkissuedomain.NormalizePlan(stringValue(item["key"])); normalized != "" {
				key = normalized
			}
			out[cdkissuedomain.NormalizePlan(key)] = toPlanConfig(item)
		}
		return out
	}
	var asList []map[string]interface{}
	if err := json.Unmarshal(raw, &asList); err == nil {
		for _, item := range asList {
			key := cdkissuedomain.NormalizePlan(stringValue(item["key"]))
			if key == "" {
				continue
			}
			out[key] = toPlanConfig(item)
		}
	}
	return out
}

func toPlanConfig(item map[string]interface{}) planConfig {
	config := planConfig{label: stringValue(item["label"]), enabled: true}
	if enabled, ok := item["enabled"].(bool); ok {
		config.enabled = enabled
	}
	config.fee = int64Value(item, "service_fee_usd_minor", "serviceFeeUsdMinor", "serviceFeeUSDMinor")
	return config
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func int64Value(item map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		switch value := item[key].(type) {
		case float64:
			return int64(value)
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func firstField(fields map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if value, ok := fields[key]; ok && len(value) > 0 && string(value) != "null" {
			return value, true
		}
	}
	return nil, false
}

// decimalField 读取数字或字符串形式的金额。
func decimalField(fields map[string]json.RawMessage, keys ...string) decimal.Decimal {
	value, ok := firstField(fields, keys...)
	if !ok {
		return decimal.Zero
	}
	text := strings.Trim(strings.TrimSpace(string(value)), `"`)
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
