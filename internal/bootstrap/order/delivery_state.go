package orderwiring

import (
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
