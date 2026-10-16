package orderwiring

import (
	"context"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	cdkissueapp "github.com/dujiao-next/internal/modules/cdkissue/application"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
)

// orderDeliveryStateAdapter 把实时发码进度映射为用户端发货状态。
type orderDeliveryStateAdapter struct {
	issues *cdkissueapp.Service
}

func (a orderDeliveryStateAdapter) ResolveDeliveryState(order *orderdomain.Order, fallbackPaidAt *time.Time) string {
	if a.issues == nil || order == nil {
		return ""
	}
	// 只有已付款、尚未交付的自动发货订单才可能处于生成中。
	if order.Status != constants.OrderStatusPaid || order.Fulfillment != nil || len(order.Items) == 0 {
		return ""
	}
	issueOrder := cdkissuecontract.IssueOrder{OrderID: order.ID, OrderNo: order.OrderNo}
	for _, item := range order.Items {
		if strings.TrimSpace(item.FulfillmentType) != constants.FulfillmentTypeAuto {
			return ""
		}
		issueOrder.Items = append(issueOrder.Items, cdkissuecontract.NewIssueOrderItem(
			item.ID, item.ProductID, item.SKUID, item.Quantity, nil, item.SKUSnapshotJSON,
		))
	}
	paidAt := order.PaidAt
	if paidAt == nil {
		paidAt = fallbackPaidAt
	}
	state, err := a.issues.DeliveryState(issueOrder, paidAt)
	if err != nil {
		logger.Warnw("order_delivery_state_resolve_failed", "order_id", order.ID, "error", err)
		return ""
	}
	return state
}

// cdkRefundTimeout 覆盖退款后调用卡台停码的耗时。
const cdkRefundTimeout = time.Minute

// newCDKRefundHook 全额退款后在后台停用该订单在卡台上尚未兑换的卡密。部分退款不处理，
// 由管理员在订单详情手动停用。
func newCDKRefundHook(issues *cdkissueapp.Service) func(order *orderdomain.Order) {
	return func(order *orderdomain.Order) {
		if issues == nil || order == nil || order.Status != constants.OrderStatusRefunded {
			return
		}
		orderID, orderNo := order.ID, order.OrderNo
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), cdkRefundTimeout)
			defer cancel()
			if err := issues.HandleOrderRefunded(ctx, orderID, orderNo); err != nil {
				logger.Warnw("order_refund_disable_cdk_failed", "order_id", orderID, "error", err)
			}
		}()
	}
}
