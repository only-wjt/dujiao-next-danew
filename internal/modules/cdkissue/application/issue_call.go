package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/logger"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/shared/jsonslice"
)

// advanceRequest 推进单个请求：已成功则解出码；待处理则调卡台；失败/不明则停下等管理员。
func (s *Service) advanceRequest(
	ctx context.Context,
	request *cdkissuedomain.IssueRequest,
	resolveCredentials func(provider string) (*cdkissuecontract.Credentials, error),
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

// reloadAndAdvance 在占用或保存失败（被其他任务抢先）时重新读取请求，按最新状态继续。
func (s *Service) reloadAndAdvance(request *cdkissuedomain.IssueRequest) ([]string, requestOutcome) {
	latest, err := s.store.GetRequest(request.ID)
	if err != nil || latest == nil {
		return nil, outcomeRetry
	}
	*request = *latest
	switch latest.Status {
	case cdkissuedomain.RequestStatusSucceeded:
		codes, err := s.decryptCodes(latest.CodesEncrypted)
		if err != nil || len(codes) != latest.Count {
			return nil, outcomeRetry
		}
		return codes, outcomeDone
	case cdkissuedomain.RequestStatusPending:
		// 另一个任务正在调用卡台，稍后再看结果。
		return nil, outcomeRetry
	default:
		return nil, outcomeHalted
	}
}

func (s *Service) callIssue(
	ctx context.Context,
	request *cdkissuedomain.IssueRequest,
	resolveCredentials func(provider string) (*cdkissuecontract.Credentials, error),
) ([]string, requestOutcome) {
	gateway, gatewayErr := s.gatewayFor(request.Provider)
	credentials, err := resolveCredentials(request.Provider)
	if gatewayErr != nil || err != nil {
		// 没有发出请求，不可能扣费，可放心换新键重试。
		return s.finishRequest(request, cdkissuedomain.RequestStatusFailed, 0, errorCodeNotConfigured,
			providerLabel(request.Provider)+" 未配置或密钥无法解密", true)
	}

	// 原子占用：同一请求同一时刻只允许一个任务调用卡台；尝试次数在占用时落库。
	now := s.now()
	until := now.Add(cdkissuedomain.ClaimLease)
	claimed, err := s.store.ClaimRequest(request.ID, now, until)
	if err != nil {
		logger.Warnw("cdk_issue_claim_failed", "request_id", request.ID, "error", err)
		return nil, outcomeRetry
	}
	if !claimed {
		return s.reloadAndAdvance(request)
	}
	request.Attempts++
	request.ClaimedUntil = &until
	request.UpdatedAt = now

	result, err := gateway.Issue(ctx, *credentials, cdkissuecontract.IssueCommand{
		Plan:           request.Plan,
		Count:          request.Count,
		PaymentCountry: request.PaymentCountry,
		IdempotencyKey: request.IdempotencyKey,
		OrderNo:        request.OrderNo,
		ExternalRef:    fmt.Sprintf("item:%d:chunk:%d", request.OrderItemID, request.ChunkIndex),
	})
	if err != nil {
		var gatewayErr *cdkissuecontract.GatewayError
		if !errors.As(err, &gatewayErr) {
			gatewayErr = &cdkissuecontract.GatewayError{ErrorCode: errorCodeNetwork, Message: err.Error(), Transient: true}
		}
		if gatewayErr.Transient {
			if request.Attempts >= cdkissuedomain.MaxAutoAttempts {
				return s.finishRequest(request, cdkissuedomain.RequestStatusUnknown, gatewayErr.HTTPStatus, gatewayErr.ErrorCode, gatewayErr.Message, true)
			}
			return s.finishRequest(request, cdkissuedomain.RequestStatusPending, gatewayErr.HTTPStatus, gatewayErr.ErrorCode, gatewayErr.Message, false)
		}
		return s.finishRequest(request, cdkissuedomain.RequestStatusFailed, gatewayErr.HTTPStatus, gatewayErr.ErrorCode, gatewayErr.Message, true)
	}

	if len(result.Codes) != request.Count {
		// 数量不符时不交付，保留原键：卡台重放应返回同一批码，交给管理员核实。
		return s.finishRequest(request, cdkissuedomain.RequestStatusUnknown, 200, errorCodeCountMismatch,
			fmt.Sprintf("卡台返回 %d 张码，期望 %d 张", len(result.Codes), request.Count), true)
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
		// 码已到手但无法安全落库；释放占用，下次用同一幂等键重放取回。
		logger.Errorw("cdk_issue_encrypt_codes_failed", "request_id", request.ID, "error", err)
		s.releaseClaim(request)
		return nil, outcomeRetry
	}
	request.Status = cdkissuedomain.RequestStatusSucceeded
	request.CodesEncrypted = encrypted
	request.ProviderCDKIDs = ids
	request.ProviderRequestID = truncateRunes(strings.TrimSpace(result.ProviderRequestID), 64)
	request.IssuedCount = len(codes)
	request.FeeAmountMinor = fee
	request.LastHTTPStatus = 200
	request.LastErrorCode = ""
	request.LastErrorMsg = ""
	request.ClaimedUntil = nil
	request.UpdatedAt = s.now()
	saved, err := s.store.UpdateRequestIfStatus(request, cdkissuedomain.RequestStatusPending)
	if err != nil {
		logger.Errorw("cdk_issue_save_codes_failed", "request_id", request.ID, "error", err)
		return nil, outcomeRetry
	}
	if !saved {
		// 已被其他任务写入：以库里的结果为准，保证只有一份码。
		return s.reloadAndAdvance(request)
	}
	logger.Infow("cdk_issue_succeeded",
		"request_id", request.ID,
		"provider", request.Provider,
		"provider_request_id", request.ProviderRequestID,
		"order_no", request.OrderNo,
		"plan", request.Plan,
		"count", request.Count,
		"replayed", result.Replayed,
	)
	return codes, outcomeDone
}

// providerLabel 是告警与后台展示用的渠道名。
func providerLabel(provider string) string {
	if cdkissuedomain.NormalizeProvider(provider) == cdkissuedomain.ProviderDanewCDK {
		return "danew_cdk"
	}
	return "ZovoCard"
}

// releaseClaim 释放占用，让下一次重试能立即接手。
func (s *Service) releaseClaim(request *cdkissuedomain.IssueRequest) {
	request.ClaimedUntil = nil
	request.UpdatedAt = s.now()
	if _, err := s.store.UpdateRequestIfStatus(request, cdkissuedomain.RequestStatusPending); err != nil {
		logger.Warnw("cdk_issue_release_claim_failed", "request_id", request.ID, "error", err)
	}
}

// finishRequest 写入非成功结果并返回对应的推进结果。只在请求仍为 pending 时写入，
// 避免覆盖其他任务已经保存的成功结果。
func (s *Service) finishRequest(request *cdkissuedomain.IssueRequest, status string, httpStatus int, errorCode, message string, alert bool) ([]string, requestOutcome) {
	request.Status = status
	request.LastHTTPStatus = httpStatus
	request.LastErrorCode = strings.TrimSpace(errorCode)
	request.LastErrorMsg = truncateRunes(strings.TrimSpace(message), 480)
	request.ClaimedUntil = nil
	request.UpdatedAt = s.now()
	saved, err := s.store.UpdateRequestIfStatus(request, cdkissuedomain.RequestStatusPending)
	if err != nil {
		logger.Errorw("cdk_issue_save_status_failed", "request_id", request.ID, "status", status, "error", err)
		return nil, outcomeRetry
	}
	if !saved {
		return s.reloadAndAdvance(request)
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
	if status == cdkissuedomain.RequestStatusPending {
		return nil, outcomeRetry
	}
	return nil, outcomeHalted
}
