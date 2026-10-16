package consumer

import (
	"context"
	"errors"

	"github.com/dujiao-next/internal/logger"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"

	"github.com/hibiken/asynq"
)

// handleCDKIssueSweep 把丢失发货任务的实时发码订单重新入队。
func (c *Consumer) handleCDKIssueSweep(_ context.Context, _ *asynq.Task) error {
	if c == nil || c.CDKIssueService == nil {
		return nil
	}
	if _, err := c.CDKIssueService.SweepStuckOrders(); err != nil && !errors.Is(err, cdkissuecontract.ErrQueueUnavailable) {
		logger.Warnw("worker_cdk_issue_sweep_failed", "error", err)
		return err
	}
	return nil
}
