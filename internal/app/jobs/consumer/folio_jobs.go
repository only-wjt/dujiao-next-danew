package consumer

import (
	"context"
	"encoding/json"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	orderapp "github.com/dujiao-next/internal/modules/order/application"
	"github.com/dujiao-next/internal/queue"

	"github.com/hibiken/asynq"
)

func (c *Consumer) resolveFolioConfig() config.FolioConfig {
	if c == nil || c.Container == nil || c.Config == nil {
		return config.FolioConfig{}
	}
	if c.SettingService == nil {
		return c.Config.Folio
	}
	return c.SettingService.ResolveFolioConfig(c.Config.Folio)
}

func (c *Consumer) handleFolioInvoice(ctx context.Context, task *asynq.Task) error {
	_ = ctx
	if c == nil || c.Container == nil || c.Config == nil {
		return nil
	}
	var payload queue.FolioInvoicePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("folio_invoice_payload_invalid", "error", err)
		return nil
	}
	folioCfg := c.resolveFolioConfig()
	if err := orderapp.PushInvoiceRequest(folioCfg, c.OrderStore, c.PaymentStore, payload.OrderID); err != nil {
		logger.Warnw("folio_invoice_push_failed", "order_id", payload.OrderID, "error", err)
		return err
	}
	return nil
}

func (c *Consumer) handleFolioRefund(ctx context.Context, task *asynq.Task) error {
	_ = ctx
	if c == nil || c.Container == nil || c.Config == nil {
		return nil
	}
	var payload queue.FolioRefundPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("folio_refund_payload_invalid", "error", err)
		return nil
	}
	order, err := c.OrderStore.GetByID(payload.OrderID)
	if err != nil || order == nil {
		return err
	}
	record, err := c.OrderStore.GetRefundRecordByID(payload.RecordID)
	if err != nil || record == nil {
		return err
	}
	root, err := orderapp.ResolveFolioRefundRoot(c.OrderStore, order)
	if err != nil {
		return err
	}
	if root == nil {
		return nil
	}
	full := payload.Full || root.Status == constants.OrderStatusRefunded
	folioCfg := c.resolveFolioConfig()
	if err := orderapp.PushRefundEvent(folioCfg, c.OrderStore, root, record, full); err != nil {
		logger.Warnw("folio_refund_push_failed", "order_id", root.ID, "record_id", payload.RecordID, "error", err)
		return err
	}
	return nil
}
