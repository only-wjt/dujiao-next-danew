package orderhttp

import (
	"crypto/subtle"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	orderapp "github.com/dujiao-next/internal/modules/order/application"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// FolioHandler 提供给 Folio 的只读金额查询和开票回调。
type FolioHandler struct {
	orders        ordercontract.Store
	payments      paymentcontract.Store
	resolveSecret func() string
	nonces        *nonceStore
}

func NewFolioHandler(orders ordercontract.Store, payments paymentcontract.Store, resolveSecret func() string) *FolioHandler {
	if resolveSecret == nil {
		resolveSecret = func() string { return "" }
	}
	return &FolioHandler{orders: orders, payments: payments, resolveSecret: resolveSecret, nonces: newNonceStore()}
}

func RegisterFolio(group *gin.RouterGroup, orders ordercontract.Store, payments paymentcontract.Store, resolveSecret func() string) {
	h := NewFolioHandler(orders, payments, resolveSecret)
	g := group.Group("/integrations/folio")
	g.GET("/orders/:order_no", h.GetOrder)
	g.GET("/orders", h.ListOrders)
	g.POST("/callback", h.Callback)
}

func (h *FolioHandler) GetOrder(c *gin.Context) {
	if !h.authorize(c) {
		return
	}
	view, err := orderapp.LoadInvoiceable(h.orders, h.payments, c.Param("order_no"), time.Now())
	if err != nil {
		response.Error(c, response.CodeInternal, "订单查询失败")
		return
	}
	if view == nil {
		response.Error(c, response.CodeNotFound, "订单不存在")
		return
	}
	response.Success(c, view)
}

func (h *FolioHandler) ListOrders(c *gin.Context) {
	if !h.authorize(c) {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	var from, to *time.Time
	if raw := c.Query("paid_from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.Error(c, response.CodeBadRequest, "paid_from 格式错误")
			return
		}
		from = &parsed
	}
	if raw := c.Query("paid_to"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.Error(c, response.CodeBadRequest, "paid_to 格式错误")
			return
		}
		to = &parsed
	}
	orders, total, err := h.orders.ListInvoiceRequested(from, to, page, 20)
	if err != nil {
		response.Error(c, response.CodeInternal, "订单查询失败")
		return
	}
	type row struct {
		OrderNo   string     `json:"order_no"`
		Status    string     `json:"status"`
		Currency  string     `json:"currency"`
		PaidAt    *time.Time `json:"paid_at"`
		InvoiceNo string     `json:"invoice_no"`
	}
	out := make([]row, 0, len(orders))
	for _, order := range orders {
		out = append(out, row{OrderNo: order.OrderNo, Status: order.Status, Currency: order.Currency, PaidAt: order.PaidAt, InvoiceNo: order.InvoiceNo})
	}
	response.SuccessWithPage(c, out, response.BuildPagination(page, 20, total))
}

type folioCallback struct {
	EventID      string `json:"event_id"`
	Event        string `json:"event"`
	Version      int    `json:"version"`
	OrderNo      string `json:"order_no"`
	InvoiceID    string `json:"invoice_id"`
	Status       string `json:"status"`
	Fphm         string `json:"fphm"`
	Amount       string `json:"amount"`
	ErrorMessage string `json:"error_message"`
}

func (h *FolioHandler) Callback(c *gin.Context) {
	body, ok := h.readAuthorized(c)
	if !ok {
		return
	}
	var payload folioCallback
	if err := c.ShouldBindJSON(&payload); err != nil {
		_ = body
		response.Error(c, response.CodeBadRequest, "回调格式错误")
		return
	}
	order, err := h.orders.GetByOrderNo(payload.OrderNo)
	if err != nil || order == nil {
		response.Error(c, response.CodeNotFound, "订单不存在")
		return
	}
	if order.ParentID != nil {
		parent, err := h.orders.GetByID(*order.ParentID)
		if err != nil || parent == nil {
			response.Error(c, response.CodeNotFound, "订单不存在")
			return
		}
		order = parent
	}
	if payload.Version > 0 && payload.Version <= order.InvoiceSyncVersion {
		response.Success(c, gin.H{"duplicate": true})
		return
	}
	updates := map[string]interface{}{
		"invoice_status":       payload.Status,
		"invoice_no":           payload.Fphm,
		"invoice_external_id":  payload.InvoiceID,
		"invoice_last_error":   payload.ErrorMessage,
		"invoice_sync_version": payload.Version,
		"updated_at":           time.Now(),
	}
	if payload.Status == "issued" || payload.Event == "invoice.issued" {
		now := time.Now()
		updates["invoice_issued_at"] = &now
	}
	if err := h.orders.UpdateFields(order.ID, updates); err != nil {
		response.Error(c, response.CodeInternal, "回写失败")
		return
	}
	response.Success(c, gin.H{"accepted": true})
}

func (h *FolioHandler) authorize(c *gin.Context) bool {
	_, ok := h.readAuthorized(c)
	return ok
}

func (h *FolioHandler) readAuthorized(c *gin.Context) ([]byte, bool) {
	secret := strings.TrimSpace(h.resolveSecret())
	if secret == "" {
		response.ErrorWithHTTPStatus(c, http.StatusUnauthorized, response.CodeUnauthorized, "Folio 密钥未配置")
		return nil, false
	}
	body, _ := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	c.Request.Body = io.NopCloser(bytesReader(body))
	ts := c.GetHeader("X-Folio-Timestamp")
	nonce := c.GetHeader("X-Folio-Nonce")
	sig := c.GetHeader("X-Folio-Signature")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || abs64(time.Now().Unix()-sec) > 300 || nonce == "" || !h.nonces.remember(nonce) {
		response.ErrorWithHTTPStatus(c, http.StatusUnauthorized, response.CodeUnauthorized, "签名无效")
		return nil, false
	}
	expect := orderapp.SignFolioRequest(c.Request.Method, c.Request.URL.Path, ts, nonce, body, secret)
	if subtle.ConstantTimeCompare([]byte(expect), []byte(sig)) != 1 {
		response.ErrorWithHTTPStatus(c, http.StatusUnauthorized, response.CodeUnauthorized, "签名无效")
		return nil, false
	}
	return body, true
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

type nonceStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newNonceStore() *nonceStore { return &nonceStore{seen: map[string]time.Time{}} }

func (n *nonceStore) remember(nonce string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	for key, exp := range n.seen {
		if exp.Before(now) {
			delete(n.seen, key)
		}
	}
	if _, ok := n.seen[nonce]; ok {
		return false
	}
	n.seen[nonce] = now.Add(10 * time.Minute)
	return true
}

func bytesReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct{ b []byte }

func (s *sliceReader) Read(p []byte) (int, error) {
	if len(s.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.b)
	s.b = s.b[n:]
	return n, nil
}

// 保证回调结构体能编译引用订单字段。
var _ = orderdomain.Order{}
