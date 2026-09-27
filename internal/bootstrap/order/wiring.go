package orderwiring

import (
	"github.com/dujiao-next/internal/app/container"
	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordertransport "github.com/dujiao-next/internal/modules/order/transport/http"
	"github.com/dujiao-next/internal/queue"
)

// Handlers contains every order HTTP entrypoint required by the router.
type Handlers struct {
	Admin       *ordertransport.AdminHandler
	AdminRefund *ordertransport.AdminRefundHandler
	User        *ordertransport.UserHandler
	Guest       *ordertransport.GuestHandler
	Preview     *ordertransport.PreviewHandler
	Create      *ordertransport.CreateHandler
}

// New assembles order transports and their composition-boundary adapters.
func New(c *container.Container) Handlers {
	return Handlers{
		Admin: ordertransport.NewAdminHandler(
			orderAdminQueryAdapter{orders: c.OrderService},
			orderAdminUserAdapter{users: c.UserStore},
			orderAdminCouponAdapter{coupons: c.CouponRepo},
			orderAdminPromotionAdapter{promotions: c.PromotionRepo},
			orderAdminPaymentAdapter{payments: c.PaymentStore},
			orderAdminPaymentChannelAdapter{channels: c.PaymentChannelStore},
		),
		AdminRefund: NewAdminRefundHandler(c),
		User: ordertransport.NewUserHandler(
			orderUserQueryAdapter{orders: c.OrderService},
			orderUserPaymentChannelAdapter{payments: c.PaymentService},
			orderUserRefundRecordAdapter{records: c.OrderStore},
			orderUserLookupAdapter{users: c.UserStore},
			orderAdminPaymentAdapter{payments: c.PaymentStore},
		),
		Guest: ordertransport.NewGuestHandler(
			orderGuestQueryAdapter{orders: c.OrderService},
			orderUserPaymentChannelAdapter{payments: c.PaymentService},
			orderUserRefundRecordAdapter{records: c.OrderStore},
			orderAdminPaymentAdapter{payments: c.PaymentStore},
		),
		Preview: ordertransport.NewPreviewHandler(
			orderPreviewAdapter{orders: c.OrderService},
		),
		Create: ordertransport.NewCreateHandler(
			orderCreateAdapter{orders: c.OrderService},
			orderUserPaymentChannelAdapter{payments: c.PaymentService},
			orderGuestCreateCaptchaAdapter{captcha: c.CaptchaService},
			orderPaymentCreatorAdapter{payments: c.PaymentService},
		),
	}
}

// NewAdminRefundHandler exposes the focused refund composition for integration
// tests and command surfaces that do not need the complete order handler set.
func NewAdminRefundHandler(c *container.Container) *ordertransport.AdminRefundHandler {
	refunds := orderAdminRefundAdapter{refunds: c.OrderRefundService}
	handler := ordertransport.NewAdminRefundHandler(
		refunds,
		refunds,
		orderAdminWalletRefundAdapter{refunds: c.OrderRefundService},
		orderAdminOrderLookupAdapter{orders: c.OrderStore},
		orderAdminStatusEmailAdapter{queue: c.QueueClient},
	)
	if c != nil && c.Config != nil && c.QueueClient != nil {
		qc := c.QueueClient
		orders := c.OrderStore
		settings := c.SettingService
		cfg := c.Config
		handler.FolioRefund = func(order *orderdomain.Order, record *orderdomain.OrderRefundRecord) {
			folioCfg := cfg.Folio
			if settings != nil {
				folioCfg = settings.ResolveFolioConfig(cfg.Folio)
			}
			if !config.FolioEnabled(folioCfg) {
				return
			}
			if order == nil || record == nil {
				return
			}
			root := order
			if order.ParentID != nil && orders != nil {
				if parent, err := orders.GetByID(*order.ParentID); err == nil && parent != nil {
					root = parent
				}
			}
			if !root.InvoiceRequested {
				return
			}
			_ = qc.EnqueueFolioRefund(queue.FolioRefundPayload{
				OrderID:  root.ID,
				RecordID: record.ID,
				Full:     root.Status == constants.OrderStatusRefunded || order.Status == constants.OrderStatusRefunded,
			})
		}
	}
	return handler
}
