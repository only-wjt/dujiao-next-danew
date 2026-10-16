package orderhttp

import (
	"errors"
	"strings"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	orderpresenter "github.com/dujiao-next/internal/modules/order/transport/presenter"
	reseller "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// GuestOrderQuery 游客订单只读端口。
type GuestOrderQuery interface {
	ListOrdersByGuestForTenant(tenant reseller.TenantContext, email, password string, page, pageSize int) ([]orderdomain.Order, int64, error)
	GetOrderByGuestOrderNoForTenant(tenant reseller.TenantContext, orderNo, email, password string) (*orderdomain.Order, error)
	GetAnyOrderByGuestOrderNoForTenant(tenant reseller.TenantContext, orderNo, email, password string) (*orderdomain.Order, error)
}

// GuestHandler 处理前台游客订单只读 HTTP。
type GuestHandler struct {
	orders         GuestOrderQuery
	payments       PaymentChannelPolicy
	refunds        RefundRecordDirectory
	paymentRecords PaymentDirectory
	deliveryStates DeliveryStateResolver
}

// SetDeliveryStateResolver 注入实时发码发货状态解析。
func (h *GuestHandler) SetDeliveryStateResolver(resolver DeliveryStateResolver) {
	h.deliveryStates = resolver
}

func NewGuestHandler(orders GuestOrderQuery, payments PaymentChannelPolicy, refunds RefundRecordDirectory, paymentRecords PaymentDirectory) *GuestHandler {
	if orders == nil {
		panic("order guest handler: orders is nil")
	}
	return &GuestHandler{orders: orders, payments: payments, refunds: refunds, paymentRecords: paymentRecords}
}

// ListGuestOrders 获取游客订单列表
func (h *GuestHandler) ListGuestOrders(c *gin.Context) {
	email, password, ok := ginutil.GetGuestCredentials(c)
	orderNo := strings.TrimSpace(c.Query("order_no"))
	if !ok || email == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.guest_email_required", nil)
		return
	}
	if password == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.guest_password_required", nil)
		return
	}

	if orderNo != "" {
		order, err := h.orders.GetOrderByGuestOrderNoForTenant(tenantFromRequest(c), orderNo, email, password)
		if err != nil {
			if errors.Is(err, ErrGuestOrderNotFound) {
				ginutil.MarkGuestLookup(c, false, orderNo)
				pagination := response.Pagination{
					Page:      1,
					PageSize:  1,
					Total:     0,
					TotalPage: 1,
				}
				response.SuccessWithPage(c, []orderdomain.Order{}, pagination)
				return
			}
			ginutil.RespondError(c, response.CodeInternal, "error.order_fetch_failed", err)
			return
		}
		ginutil.MarkGuestLookup(c, true, orderNo)
		pagination := response.Pagination{
			Page:      1,
			PageSize:  1,
			Total:     1,
			TotalPage: 1,
		}
		response.SuccessWithPage(c, orderpresenter.NewOrderSummaryList([]orderdomain.Order{*order}), pagination)
		return
	}

	page, pageSize := ginutil.ParsePagination(c)

	orders, total, err := h.orders.ListOrdersByGuestForTenant(tenantFromRequest(c), email, password, page, pageSize)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.order_fetch_failed", err)
		return
	}
	// 邮箱下没有任何订单即视为凭证错误。
	ginutil.MarkGuestLookup(c, total > 0, "")
	pagination := response.BuildPagination(page, pageSize, total)
	response.SuccessWithPage(c, orderpresenter.NewOrderSummaryList(orders), pagination)
}

// GetGuestOrderByOrderNo 按订单号获取游客订单详情
func (h *GuestHandler) GetGuestOrderByOrderNo(c *gin.Context) {
	email, password, ok := ginutil.GetGuestCredentials(c)
	if !ok || email == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.guest_email_required", nil)
		return
	}
	if password == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.guest_password_required", nil)
		return
	}
	orderNo := strings.TrimSpace(c.Param("order_no"))
	if orderNo == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.order_item_invalid", nil)
		return
	}
	order, err := h.orders.GetOrderByGuestOrderNoForTenant(tenantFromRequest(c), orderNo, email, password)
	if err != nil {
		if errors.Is(err, ErrGuestOrderNotFound) {
			ginutil.MarkGuestLookup(c, false, orderNo)
			ginutil.RespondError(c, response.CodeNotFound, "error.guest_order_not_found", nil)
			return
		}
		ginutil.RespondError(c, response.CodeInternal, "error.order_fetch_failed", err)
		return
	}
	ginutil.MarkGuestLookup(c, true, orderNo)
	orderDetail := orderpresenter.NewOrderDetailTruncated(order)
	enrichOrderWithAllowedChannels(h.payments, order, &orderDetail)
	enrichOrderWithRefundRecords(h.refunds, order, &orderDetail)
	enrichOrderWithPaymentFee(h.paymentRecords, order, &orderDetail)
	enrichOrderWithDeliveryState(h.deliveryStates, order, &orderDetail)
	response.Success(c, orderDetail)
}

// DownloadGuestFulfillment 下载订单交付内容（游客）
// 支持父订单或子订单的 order_no
func (h *GuestHandler) DownloadGuestFulfillment(c *gin.Context) {
	email, password, ok := ginutil.GetGuestCredentials(c)
	if !ok || email == "" || password == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.guest_email_required", nil)
		return
	}
	orderNo := strings.TrimSpace(c.Param("order_no"))
	if orderNo == "" {
		ginutil.RespondError(c, response.CodeBadRequest, "error.order_item_invalid", nil)
		return
	}
	order, err := h.orders.GetAnyOrderByGuestOrderNoForTenant(tenantFromRequest(c), orderNo, email, password)
	if err != nil {
		if errors.Is(err, ErrGuestOrderNotFound) {
			ginutil.MarkGuestLookup(c, false, orderNo)
			ginutil.RespondError(c, response.CodeNotFound, "error.guest_order_not_found", nil)
			return
		}
		ginutil.RespondError(c, response.CodeInternal, "error.order_fetch_failed", err)
		return
	}
	if order == nil {
		ginutil.MarkGuestLookup(c, false, orderNo)
		ginutil.RespondError(c, response.CodeNotFound, "error.guest_order_not_found", nil)
		return
	}
	ginutil.MarkGuestLookup(c, true, orderNo)
	respondFulfillmentDownload(c, order)
}
