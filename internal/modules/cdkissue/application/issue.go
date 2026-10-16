package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/crypto"
	"github.com/dujiao-next/internal/logger"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/shared/jsonslice"
)

const (
	errorCodeNotConfigured = "not_configured"
	errorCodeCountMismatch = "count_mismatch"
	errorCodeNetwork       = "network_error"
)

type itemChunkKey struct {
	itemID uint
	chunk  int
}

// RealtimeItems 返回订单中走实时发码的订单项：下单时已固化实时发码快照，或此前已建过发码请求。
// 只看下单快照而不看当前商品绑定，避免付款后修改绑定导致改用库存卡密或换套餐。
func (s *Service) RealtimeItems(order cdkissuecontract.IssueOrder) (map[uint]bool, error) {
	out := make(map[uint]bool)
	if len(order.Items) == 0 {
		return out, nil
	}
	existing, err := s.store.ListRequestsByOrder(order.OrderID)
	if err != nil {
		return nil, err
	}
	requested := make(map[uint]bool, len(existing))
	for _, request := range existing {
		requested[request.OrderItemID] = true
	}
	for _, item := range order.Items {
		if (item.Realtime && item.Plan != "") || requested[item.OrderItemID] {
			out[item.OrderItemID] = true
		}
	}
	return out, nil
}

// EnsureIssued 确保订单里的实时发码订单项都已拿到码，可安全重复调用。
//
// 返回 ErrIssueRetryable 表示应稍后由队列重试（同一幂等键）；
// 返回 ErrIssueHalted 表示需要管理员处理，队列不应继续重试。
func (s *Service) EnsureIssued(ctx context.Context, order cdkissuecontract.IssueOrder) (cdkissuecontract.IssuedItemCodes, error) {
	result := cdkissuecontract.IssuedItemCodes{}
	realtime, err := s.RealtimeItems(order)
	if err != nil {
		return nil, err
	}
	if len(realtime) == 0 {
		return result, nil
	}

	existingRows, err := s.store.ListRequestsByOrder(order.OrderID)
	if err != nil {
		return nil, err
	}
	existing := make(map[itemChunkKey]*cdkissuedomain.IssueRequest, len(existingRows))
	for i := range existingRows {
		row := &existingRows[i]
		existing[itemChunkKey{row.OrderItemID, row.ChunkIndex}] = row
	}

	config, err := s.loadConfig()
	if err != nil {
		return nil, err
	}

	rootOrderID := order.RootOrderID
	if rootOrderID == 0 {
		rootOrderID = order.OrderID
	}

	var credentials *cdkissuecontract.Credentials
	var credentialsErr error
	resolveCredentials := func() (*cdkissuecontract.Credentials, error) {
		if credentials == nil && credentialsErr == nil {
			value, err := s.credentials(config)
			if err != nil {
				credentialsErr = err
			} else {
				credentials = &value
			}
		}
		return credentials, credentialsErr
	}

	retryable, halted := false, false
	for _, item := range order.Items {
		if !realtime[item.OrderItemID] {
			continue
		}
		plan, country := item.Plan, item.PaymentCountry
		// 已建过请求时沿用请求上的套餐，保证同一订单项各分片口径一致。
		if first := existing[itemChunkKey{item.OrderItemID, 0}]; first != nil {
			plan, country = first.Plan, first.PaymentCountry
		}
		country = config.ResolveCountry(country)

		itemCodes := make([]string, 0, item.Quantity)
		itemComplete := true
		for chunkIndex, count := range cdkissuedomain.SplitCount(item.Quantity) {
			request := existing[itemChunkKey{item.OrderItemID, chunkIndex}]
			if request == nil {
				created, err := s.store.CreateRequestIfAbsent(&cdkissuedomain.IssueRequest{
					RootOrderID:    rootOrderID,
					OrderID:        order.OrderID,
					OrderNo:        order.OrderNo,
					OrderItemID:    item.OrderItemID,
					ChunkIndex:     chunkIndex,
					ProductID:      item.ProductID,
					ItemTitle:      item.Title,
					SKUID:          item.SKUID,
					Provider:       cdkissuedomain.ProviderZovoCard,
					Plan:           plan,
					PaymentCountry: country,
					Count:          count,
					IdempotencyKey: cdkissuedomain.BuildIdempotencyKey(order.OrderNo, item.OrderItemID, chunkIndex, 0),
					Status:         cdkissuedomain.RequestStatusPending,
					ProviderCDKIDs: jsonslice.Uints{},
				})
				if err != nil {
					return nil, err
				}
				request = created
				existing[itemChunkKey{item.OrderItemID, chunkIndex}] = created
			}

			codes, outcome := s.advanceRequest(ctx, request, resolveCredentials)
			switch outcome {
			case outcomeDone:
				itemCodes = append(itemCodes, codes...)
			case outcomeRetry:
				retryable = true
				itemComplete = false
			default:
				halted = true
				itemComplete = false
			}
		}
		if itemComplete {
			result[item.OrderItemID] = itemCodes
		}
	}

	switch {
	case retryable:
		return result, cdkissuecontract.ErrIssueRetryable
	case halted:
		return result, cdkissuecontract.ErrIssueHalted
	}
	return result, nil
}

type requestOutcome int

const (
	outcomeDone requestOutcome = iota
	outcomeRetry
	outcomeHalted
)

// advanceRequest 推进单个请求：已成功则解出码；待处理则调卡台；失败/不明则停下等管理员。
func (s *Service) advanceRequest(
	ctx context.Context,
	request *cdkissuedomain.IssueRequest,
	resolveCredentials func() (*cdkissuecontract.Credentials, error),
) ([]string, requestOutcome) {
	switch request.Status {
	case cdkissuedomain.RequestStatusSucceeded:
		codes, err := s.decryptCodes(request.CodesEncrypted)
		if err != nil || len(codes) != request.Count {
			logger.Errorw("cdk_issue_stored_codes_invalid", "request_id", request.ID, "order_id", request.OrderID, "error", err)
			return nil, outcomeHalted
		}
		return codes, outcomeDone
	case cdkissuedomain.RequestStatusPending:
		return s.callIssue(ctx, request, resolveCredentials)
	default:
		return nil, outcomeHalted
	}
}

func (s *Service) callIssue(
	ctx context.Context,
	request *cdkissuedomain.IssueRequest,
	resolveCredentials func() (*cdkissuecontract.Credentials, error),
) ([]string, requestOutcome) {
	credentials, err := resolveCredentials()
	if err != nil {
		// 没有发出请求，不可能扣费，可放心换新键重试。
		s.finishRequest(request, cdkissuedomain.RequestStatusFailed, 0, errorCodeNotConfigured, "ZovoCard 未配置或密钥无法解密", true)
		return nil, outcomeHalted
	}

	// 先落库尝试次数，进程中途退出也不会无限重试。
	request.Attempts++
	request.UpdatedAt = s.now()
	if err := s.store.UpdateRequest(request); err != nil {
		logger.Warnw("cdk_issue_attempt_save_failed", "request_id", request.ID, "error", err)
		return nil, outcomeRetry
	}

	result, err := s.gateway.Issue(ctx, *credentials, cdkissuecontract.IssueCommand{
		Plan:           request.Plan,
		Count:          request.Count,
		PaymentCountry: request.PaymentCountry,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		var gatewayErr *cdkissuecontract.GatewayError
		if !errors.As(err, &gatewayErr) {
			gatewayErr = &cdkissuecontract.GatewayError{ErrorCode: errorCodeNetwork, Message: err.Error(), Transient: true}
		}
		if gatewayErr.Transient {
			if request.Attempts >= cdkissuedomain.MaxAutoAttempts {
				s.finishRequest(request, cdkissuedomain.RequestStatusUnknown, gatewayErr.HTTPStatus, gatewayErr.ErrorCode, gatewayErr.Message, true)
				return nil, outcomeHalted
			}
			s.finishRequest(request, cdkissuedomain.RequestStatusPending, gatewayErr.HTTPStatus, gatewayErr.ErrorCode, gatewayErr.Message, false)
			return nil, outcomeRetry
		}
		s.finishRequest(request, cdkissuedomain.RequestStatusFailed, gatewayErr.HTTPStatus, gatewayErr.ErrorCode, gatewayErr.Message, true)
		return nil, outcomeHalted
	}

	if len(result.Codes) != request.Count {
		// 数量不符时不交付，保留原键：卡台重放应返回同一批码，交给管理员核实。
		s.finishRequest(request, cdkissuedomain.RequestStatusUnknown, 200, errorCodeCountMismatch,
			fmt.Sprintf("卡台返回 %d 张码，期望 %d 张", len(result.Codes), request.Count), true)
		return nil, outcomeHalted
	}

	codes := make([]string, 0, len(result.Codes))
	ids := make(jsonslice.Uints, 0, len(result.Codes))
	var fee int64
	for _, item := range result.Codes {
		codes = append(codes, item.Code)
		if item.ID > 0 {
			ids = append(ids, item.ID)
		}
		fee += item.FeeAmountMinor
	}
	encrypted, err := s.encryptCodes(codes)
	if err != nil {
		// 码已到手但无法安全落库；保持 pending，下次用同一幂等键重放取回。
		logger.Errorw("cdk_issue_encrypt_codes_failed", "request_id", request.ID, "error", err)
		return nil, outcomeRetry
	}
	request.Status = cdkissuedomain.RequestStatusSucceeded
	request.CodesEncrypted = encrypted
	request.ProviderCDKIDs = ids
	request.IssuedCount = len(codes)
	request.FeeAmountMinor = fee
	request.LastHTTPStatus = 200
	request.LastErrorCode = ""
	request.LastErrorMsg = ""
	request.UpdatedAt = s.now()
	if err := s.store.UpdateRequest(request); err != nil {
		logger.Errorw("cdk_issue_save_codes_failed", "request_id", request.ID, "error", err)
		return nil, outcomeRetry
	}
	logger.Infow("cdk_issue_succeeded",
		"request_id", request.ID,
		"order_no", request.OrderNo,
		"plan", request.Plan,
		"count", request.Count,
		"replayed", result.Replayed,
	)
	return codes, outcomeDone
}

func (s *Service) finishRequest(request *cdkissuedomain.IssueRequest, status string, httpStatus int, errorCode, message string, alert bool) {
	request.Status = status
	request.LastHTTPStatus = httpStatus
	request.LastErrorCode = strings.TrimSpace(errorCode)
	request.LastErrorMsg = truncateRunes(strings.TrimSpace(message), 480)
	request.UpdatedAt = s.now()
	if err := s.store.UpdateRequest(request); err != nil {
		logger.Errorw("cdk_issue_save_status_failed", "request_id", request.ID, "status", status, "error", err)
	}
	logger.Warnw("cdk_issue_not_succeeded",
		"request_id", request.ID,
		"order_no", request.OrderNo,
		"status", status,
		"attempts", request.Attempts,
		"http_status", httpStatus,
		"error_code", errorCode,
	)
	if alert && s.alerts != nil {
		if err := s.alerts.NotifyIssueAlert(request, message); err != nil {
			logger.Warnw("cdk_issue_alert_failed", "request_id", request.ID, "error", err)
		}
	}
}

// MarkDelivered 交付事务成功后清除暂存的密文。
func (s *Service) MarkDelivered(orderID uint) error {
	return s.store.MarkOrderDelivered(orderID, s.now())
}

func (s *Service) encryptCodes(codes []string) (string, error) {
	payload, err := json.Marshal(codes)
	if err != nil {
		return "", err
	}
	return crypto.Encrypt(s.encryptKey, string(payload))
}

func (s *Service) decryptCodes(encrypted string) ([]string, error) {
	if strings.TrimSpace(encrypted) == "" {
		return nil, errors.New("codes already cleared")
	}
	plain, err := crypto.Decrypt(s.encryptKey, encrypted)
	if err != nil {
		return nil, err
	}
	var codes []string
	if err := json.Unmarshal([]byte(plain), &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
