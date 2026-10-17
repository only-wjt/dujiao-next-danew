package cdkissuehttp

import (
	"context"
	"errors"
	"strings"
	"time"

	cdkissueapp "github.com/dujiao-next/internal/modules/cdkissue/application"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	ginutil "github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

const gatewayCallTimeout = 30 * time.Second

// AdminService 是后台实时发码用例端口。
type AdminService interface {
	GetConfig(provider string) (cdkissueapp.ConfigView, error)
	ListConfigs() (cdkissueapp.ConfigsView, error)
	UpdateConfig(provider string, patch cdkissueapp.ConfigPatch) (cdkissueapp.ConfigView, error)
	TestConnection(ctx context.Context, provider string, patch cdkissueapp.ConfigPatch) (*cdkissuedomain.Overview, error)
	Overview(ctx context.Context, provider string, force bool) (*cdkissuedomain.Overview, error)
	GetBinding(productID uint) (cdkissueapp.BindingView, error)
	SaveBinding(ctx context.Context, productID uint, input cdkissueapp.BindingInput) (cdkissueapp.BindingView, error)
	ListRequests(filter cdkissuecontract.RequestFilter) ([]cdkissueapp.RequestView, int64, error)
	Stats(filter cdkissuecontract.RequestFilter) (cdkissueapp.RequestStats, error)
	ListOrderRequests(orderID uint) ([]cdkissueapp.RequestView, error)
	Retry(requestID uint, mode cdkissuedomain.RetryMode) (*cdkissueapp.RequestView, error)
	DisableOrderCodes(ctx context.Context, orderID uint) (*cdkissuecontract.DisableResult, error)
}

// AdminHandler 处理后台实时发码请求。
type AdminHandler struct {
	service AdminService
}

// NewAdminHandler 创建处理器。
func NewAdminHandler(service AdminService) *AdminHandler {
	if service == nil {
		panic("cdkissue admin handler: service is nil")
	}
	return &AdminHandler{service: service}
}

// respondServiceError 统一把用例错误映射为响应。卡台原始报文只写日志，不回给浏览器以外的上下文。
func respondServiceError(c *gin.Context, err error) {
	var bindingErr *cdkissuecontract.BindingError
	var configErr *cdkissuecontract.ConfigError
	var gatewayErr *cdkissuecontract.GatewayError
	switch {
	case errors.As(err, &bindingErr):
		ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, bindingErr.Reason, nil)
	case errors.As(err, &configErr):
		ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, configErr.Reason, nil)
	case errors.As(err, &gatewayErr):
		msg := gatewayErr.Message
		if gatewayErr.ErrorCode != "" {
			msg = gatewayErr.ErrorCode + ": " + msg
		}
		label := providerLabel(c.Query("provider"))
		if gatewayErr.HTTPStatus == 401 || gatewayErr.HTTPStatus == 403 {
			ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, label+" 鉴权失败（检查密钥、IP 白名单）："+msg, err)
			return
		}
		ginutil.RespondErrorWithMsg(c, response.CodeBadRequest, label+" 返回错误："+msg, err)
	case errors.Is(err, cdkissuecontract.ErrNotConfigured):
		ginutil.RespondError(c, response.CodeBadRequest, "error.cdk_issue_not_configured", nil)
	case errors.Is(err, cdkissuecontract.ErrNotFound):
		ginutil.RespondError(c, response.CodeNotFound, "error.cdk_issue_not_found", nil)
	case errors.Is(err, cdkissuecontract.ErrRetryNotAllowed):
		ginutil.RespondError(c, response.CodeBadRequest, "error.cdk_issue_retry_not_allowed", nil)
	case errors.Is(err, cdkissuecontract.ErrDisableNotAllowed):
		ginutil.RespondError(c, response.CodeBadRequest, "error.cdk_issue_disable_not_allowed", nil)
	case errors.Is(err, cdkissuecontract.ErrQueueUnavailable):
		ginutil.RespondError(c, response.CodeBadRequest, "error.cdk_issue_queue_unavailable", nil)
	case errors.Is(err, cdkissuecontract.ErrInvalidInput):
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
	default:
		ginutil.RespondError(c, response.CodeInternal, "error.internal_error", err)
	}
}

// providerLabel 是错误提示里的渠道名；未指定渠道（当前启用渠道）时不猜具体名字。
func providerLabel(provider string) string {
	if strings.TrimSpace(provider) == "" {
		return "发码渠道"
	}
	if cdkissuedomain.NormalizeProvider(provider) == cdkissuedomain.ProviderDanewCDK {
		return "danew_cdk"
	}
	return "ZovoCard"
}

// GetConfig GET /admin/cdk-issue/config?provider=zovocard|danew_cdk
func (h *AdminHandler) GetConfig(c *gin.Context) {
	view, err := h.service.GetConfig(c.Query("provider"))
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// ListConfigs GET /admin/cdk-issue/configs — 全部渠道配置与当前启用渠道。
func (h *AdminHandler) ListConfigs(c *gin.Context) {
	views, err := h.service.ListConfigs()
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, views)
}

// UpdateConfig PUT /admin/cdk-issue/config?provider=
func (h *AdminHandler) UpdateConfig(c *gin.Context) {
	var patch cdkissueapp.ConfigPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	view, err := h.service.UpdateConfig(c.Query("provider"), patch)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// TestConnection POST /admin/cdk-issue/config/test?provider=
func (h *AdminHandler) TestConnection(c *gin.Context) {
	var patch cdkissueapp.ConfigPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), gatewayCallTimeout)
	defer cancel()
	overview, err := h.service.TestConnection(ctx, c.Query("provider"), patch)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, overview)
}

// GetOverview GET /admin/cdk-issue/overview?refresh=1&provider=（空 = 当前启用渠道）
func (h *AdminHandler) GetOverview(c *gin.Context) {
	force, _ := ginutil.ParseQueryBool(c, "refresh")
	ctx, cancel := context.WithTimeout(c.Request.Context(), gatewayCallTimeout)
	defer cancel()
	overview, err := h.service.Overview(ctx, c.Query("provider"), force)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, overview)
}

// GetBinding GET /admin/cdk-issue/products/:id/binding
func (h *AdminHandler) GetBinding(c *gin.Context) {
	productID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	view, err := h.service.GetBinding(productID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// SaveBinding PUT /admin/cdk-issue/products/:id/binding
func (h *AdminHandler) SaveBinding(c *gin.Context) {
	productID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	var input cdkissueapp.BindingInput
	if err := c.ShouldBindJSON(&input); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), gatewayCallTimeout)
	defer cancel()
	view, err := h.service.SaveBinding(ctx, productID, input)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, view)
}

func parseRequestFilter(c *gin.Context) (cdkissuecontract.RequestFilter, error) {
	page, pageSize := ginutil.ParsePagination(c)
	from, to, err := ginutil.ParseQueryTimeRange(c, "created_from", "created_to")
	if err != nil {
		return cdkissuecontract.RequestFilter{}, err
	}
	return cdkissuecontract.RequestFilter{
		Status:      strings.TrimSpace(c.Query("status")),
		Plan:        strings.TrimSpace(c.Query("plan")),
		OrderNo:     strings.TrimSpace(c.Query("order_no")),
		CreatedFrom: from,
		CreatedTo:   to,
		Page:        page,
		PageSize:    pageSize,
	}, nil
}

// ListRequests GET /admin/cdk-issue/requests
func (h *AdminHandler) ListRequests(c *gin.Context) {
	filter, err := parseRequestFilter(c)
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	rows, total, err := h.service.ListRequests(filter)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.SuccessWithPage(c, rows, response.BuildPagination(filter.Page, filter.PageSize, total))
}

// GetStats GET /admin/cdk-issue/requests/stats
func (h *AdminHandler) GetStats(c *gin.Context) {
	filter, err := parseRequestFilter(c)
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	stats, err := h.service.Stats(filter)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, stats)
}

type retryRequest struct {
	Mode string `json:"mode" binding:"required"`
}

// RetryRequest POST /admin/cdk-issue/requests/:id/retry
func (h *AdminHandler) RetryRequest(c *gin.Context) {
	requestID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	var req retryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ginutil.RespondBindError(c, err)
		return
	}
	mode := cdkissuedomain.RetryMode(strings.TrimSpace(req.Mode))
	if mode != cdkissuedomain.RetrySameKey && mode != cdkissuedomain.RetryNewKey {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	view, err := h.service.Retry(requestID, mode)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, view)
}

// ListOrderRequests GET /admin/cdk-issue/orders/:id/requests
func (h *AdminHandler) ListOrderRequests(c *gin.Context) {
	orderID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	rows, err := h.service.ListOrderRequests(orderID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, rows)
}

// DisableOrderCodes POST /admin/cdk-issue/orders/:id/disable-codes
func (h *AdminHandler) DisableOrderCodes(c *gin.Context) {
	orderID, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), gatewayCallTimeout)
	defer cancel()
	result, err := h.service.DisableOrderCodes(ctx, orderID)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	response.Success(c, result)
}
