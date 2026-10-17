package application

import (
	"context"
	"time"

	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
)

// RequestView 是后台展示的发码记录，附带当前允许的重试方式。
type RequestView struct {
	cdkissuedomain.IssueRequest
	RetryMode       string `json:"retry_mode"`
	CanDisableCodes bool   `json:"can_disable_codes"`
}

// RequestStats 是各状态数量。
type RequestStats struct {
	Total     int64 `json:"total"`
	Pending   int64 `json:"pending"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
	Unknown   int64 `json:"unknown"`
}

func (s *Service) toViews(rows []cdkissuedomain.IssueRequest) []RequestView {
	now := s.now()
	views := make([]RequestView, 0, len(rows))
	for _, row := range rows {
		views = append(views, RequestView{
			IssueRequest:    row,
			RetryMode:       string(row.AllowedRetryMode(now)),
			CanDisableCodes: row.CanDisableCodes(),
		})
	}
	return views
}

// ListRequests 分页查询发码记录。
func (s *Service) ListRequests(filter cdkissuecontract.RequestFilter) ([]RequestView, int64, error) {
	rows, total, err := s.store.ListRequests(filter)
	if err != nil {
		return nil, 0, err
	}
	return s.toViews(rows), total, nil
}

// Stats 返回各状态数量（忽略 status 筛选）。
func (s *Service) Stats(filter cdkissuecontract.RequestFilter) (RequestStats, error) {
	counts, err := s.store.CountRequestsByStatus(filter)
	if err != nil {
		return RequestStats{}, err
	}
	stats := RequestStats{
		Pending:   counts[cdkissuedomain.RequestStatusPending],
		Succeeded: counts[cdkissuedomain.RequestStatusSucceeded],
		Failed:    counts[cdkissuedomain.RequestStatusFailed],
		Unknown:   counts[cdkissuedomain.RequestStatusUnknown],
	}
	for _, value := range counts {
		stats.Total += value
	}
	return stats, nil
}

// ListOrderRequests 返回父订单（含子订单）或单订单的全部发码记录。
func (s *Service) ListOrderRequests(orderID uint) ([]RequestView, error) {
	rows, err := s.store.ListRequestsByRootOrder(orderID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		rows, err = s.store.ListRequestsByOrder(orderID)
		if err != nil {
			return nil, err
		}
	}
	return s.toViews(rows), nil
}

// Retry 由管理员触发重试。mode 必须与当前状态允许的方式一致，防止界面状态过期时误换新键。
func (s *Service) Retry(requestID uint, mode cdkissuedomain.RetryMode) (*RequestView, error) {
	if s.fulfillments == nil || !s.fulfillments.Enabled() {
		return nil, cdkissuecontract.ErrQueueUnavailable
	}
	request, err := s.store.GetRequest(requestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, cdkissuecontract.ErrNotFound
	}
	allowed := request.AllowedRetryMode(s.now())
	if allowed == "" || allowed != mode {
		return nil, cdkissuecontract.ErrRetryNotAllowed
	}
	if mode == cdkissuedomain.RetryNewKey {
		request.KeyVersion++
		request.IdempotencyKey = cdkissuedomain.BuildIdempotencyKey(request.OrderNo, request.OrderItemID, request.ChunkIndex, request.KeyVersion)
	}
	previousStatus := request.Status
	request.Status = cdkissuedomain.RequestStatusPending
	request.Attempts = 0
	request.LastErrorCode = ""
	request.LastErrorMsg = ""
	request.LastHTTPStatus = 0
	request.ClaimedUntil = nil
	request.UpdatedAt = s.now()
	// 仅在状态未被并发改动时生效，避免重复点击或与自动重试交错。
	saved, err := s.store.UpdateRequestIfStatus(request, previousStatus)
	if err != nil {
		return nil, err
	}
	if !saved {
		return nil, cdkissuecontract.ErrRetryNotAllowed
	}
	if err := s.fulfillments.EnqueueAutoFulfill(request.OrderID); err != nil {
		return nil, err
	}
	views := s.toViews([]cdkissuedomain.IssueRequest{*request})
	return &views[0], nil
}

// DisableOrderCodes 在卡台停用订单已发出的码（用于退款），已兑换的码会出现在 failed 中。
func (s *Service) DisableOrderCodes(ctx context.Context, orderID uint) (*cdkissuecontract.DisableResult, error) {
	views, err := s.ListOrderRequests(orderID)
	if err != nil {
		return nil, err
	}
	// 码 id 只在各自渠道内有意义，按请求记录的渠道分组停用。
	targets := map[string][]*cdkissuedomain.IssueRequest{}
	order := make([]string, 0, 2)
	for i := range views {
		request := &views[i].IssueRequest
		if !request.CanDisableCodes() {
			continue
		}
		provider := cdkissuedomain.NormalizeProvider(request.Provider)
		if _, seen := targets[provider]; !seen {
			order = append(order, provider)
		}
		targets[provider] = append(targets[provider], request)
	}
	if len(order) == 0 {
		return nil, cdkissuecontract.ErrDisableNotAllowed
	}
	result := &cdkissuecontract.DisableResult{Disabled: []uint{}, Failed: []uint{}}
	now := s.now()
	for _, provider := range order {
		requests := targets[provider]
		ids := make([]uint, 0)
		for _, request := range requests {
			ids = append(ids, request.ProviderCDKIDs...)
		}
		partial, err := s.disableProviderCodes(ctx, provider, ids)
		if err != nil {
			if len(order) == 1 {
				return nil, err
			}
			// 多渠道时一路失败不影响另一路，失败的码记入 failed 由调用方告警。
			result.Failed = append(result.Failed, ids...)
			continue
		}
		result.Disabled = append(result.Disabled, partial.Disabled...)
		result.Failed = append(result.Failed, partial.Failed...)
		disabled := make(map[uint]bool, len(partial.Disabled))
		for _, id := range partial.Disabled {
			disabled[id] = true
		}
		for _, request := range requests {
			allDisabled := true
			for _, id := range request.ProviderCDKIDs {
				if !disabled[id] {
					allDisabled = false
					break
				}
			}
			if allDisabled {
				if err := s.store.MarkCodesDisabled(request.ID, now); err != nil {
					return result, err
				}
			}
		}
	}
	return result, nil
}

func (s *Service) disableProviderCodes(ctx context.Context, provider string, ids []uint) (*cdkissuecontract.DisableResult, error) {
	gateway, err := s.gatewayFor(provider)
	if err != nil {
		return nil, err
	}
	config, err := s.loadConfig(provider)
	if err != nil {
		return nil, err
	}
	credentials, err := s.credentials(config)
	if err != nil {
		return nil, err
	}
	return gateway.DisableCDKs(ctx, credentials, ids)
}

const (
	// DeliveryStateIssuing 正在生成卡密。
	DeliveryStateIssuing = "issuing"
	// DeliveryStateDelayed 发放延迟，等待人工处理。
	DeliveryStateDelayed = "delayed"
)

// issuingGracePeriod 之后仍未发出视为延迟。
const issuingGracePeriod = 2 * time.Minute

// DeliveryState 返回用户端展示用的发货状态；非实时发码或已交付时返回空。
func (s *Service) DeliveryState(order cdkissuecontract.IssueOrder, paidAt *time.Time) (string, error) {
	realtime, err := s.RealtimeItems(order)
	if err != nil || len(realtime) == 0 {
		return "", err
	}
	rows, err := s.store.ListRequestsByOrder(order.OrderID)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.Status == cdkissuedomain.RequestStatusFailed || row.Status == cdkissuedomain.RequestStatusUnknown {
			return DeliveryStateDelayed, nil
		}
	}
	if paidAt != nil && s.now().Sub(*paidAt) > issuingGracePeriod {
		return DeliveryStateDelayed, nil
	}
	return DeliveryStateIssuing, nil
}
