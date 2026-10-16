package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dujiao-next/internal/logger"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
)

const (
	// sweepMinAge 付款后至少等这么久再兜底，避免和正常的发货任务抢跑。
	sweepMinAge = 3 * time.Minute
	// sweepMaxAge 只兜底最近的订单，更早的交给管理员。
	sweepMaxAge = 24 * time.Hour
	sweepLimit  = 200
)

// SweepResult 是一次补发扫描的结果。
type SweepResult struct {
	Scanned  int `json:"scanned"`
	Enqueued int `json:"enqueued"`
}

// SweepStuckOrders 把已付款但未交付、也没有等待人工处理的实时发码订单重新加入发货队列。
// 用于队列任务丢失（Redis 重启/清空）后的兜底；CreateAuto 幂等，重复入队安全。
func (s *Service) SweepStuckOrders() (SweepResult, error) {
	result := SweepResult{}
	if s.fulfillments == nil || !s.fulfillments.Enabled() {
		return result, cdkissuecontract.ErrQueueUnavailable
	}
	now := s.now()
	items, err := s.store.ListAwaitingFulfillment(now.Add(-sweepMaxAge), now.Add(-sweepMinAge), sweepLimit)
	if err != nil {
		return result, err
	}
	realtimeOrders := make(map[uint]bool)
	for _, item := range items {
		if _, _, ok := cdkissuedomain.ReadItemSnapshot(item.SKUSnapshot); ok {
			realtimeOrders[item.OrderID] = true
		}
	}
	for orderID := range realtimeOrders {
		result.Scanned++
		requests, err := s.store.ListRequestsByOrder(orderID)
		if err != nil {
			return result, err
		}
		if needsAdmin(requests, now) {
			continue
		}
		if err := s.fulfillments.EnqueueAutoFulfill(orderID); err != nil {
			logger.Warnw("cdk_issue_sweep_enqueue_failed", "order_id", orderID, "error", err)
			continue
		}
		result.Enqueued++
	}
	if result.Enqueued > 0 {
		logger.Infow("cdk_issue_sweep_enqueued", "scanned", result.Scanned, "enqueued", result.Enqueued)
	}
	return result, nil
}

// needsAdmin 表示订单已有失败/不明的请求，或仍有任务持有占用，不需要兜底入队。
func needsAdmin(requests []cdkissuedomain.IssueRequest, now time.Time) bool {
	for _, request := range requests {
		switch request.Status {
		case cdkissuedomain.RequestStatusFailed, cdkissuedomain.RequestStatusUnknown:
			return true
		case cdkissuedomain.RequestStatusPending:
			if request.ClaimedUntil != nil && request.ClaimedUntil.After(now) {
				return true
			}
		}
	}
	return false
}

// HandleOrderRefunded 在订单全额退款后停用卡台上尚未兑换的卡密；已兑换的码会告警。
func (s *Service) HandleOrderRefunded(ctx context.Context, orderID uint, orderNo string) error {
	result, err := s.DisableOrderCodes(ctx, orderID)
	if err != nil {
		if errors.Is(err, cdkissuecontract.ErrDisableNotAllowed) {
			return nil
		}
		if s.alerts != nil {
			_ = s.alerts.NotifyRefundDisableFailed(orderNo, 0, err.Error())
		}
		return err
	}
	logger.Infow("cdk_issue_refund_codes_disabled", "order_id", orderID, "disabled", len(result.Disabled), "failed", len(result.Failed))
	if len(result.Failed) > 0 && s.alerts != nil {
		msg := fmt.Sprintf("%d 张卡密无法停用，可能已被兑换，请到卡台核对", len(result.Failed))
		if alertErr := s.alerts.NotifyRefundDisableFailed(orderNo, len(result.Failed), msg); alertErr != nil {
			logger.Warnw("cdk_issue_refund_alert_failed", "order_id", orderID, "error", alertErr)
		}
	}
	return nil
}
