package application

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/logger"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"
)

// SignFolioRequest 计算 Folio 服务间签名。body 为原始请求体，GET 传空。
func SignFolioRequest(method, path, timestamp, nonce string, body []byte, secret string) string {
	sum := sha256.Sum256(body)
	content := method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(sum[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(content))
	return hex.EncodeToString(mac.Sum(nil))
}

const maxInvoicePDFURLLen = 500

// NormalizeInvoicePDFURL 只接受税局域名下的 https 链接，其它一律返回空串。
// 即使对接密钥泄露，也无法借回调往用户订单里塞第三方链接。
func NormalizeInvoicePDFURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxInvoicePDFURLLen {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "chinatax.gov.cn" && !strings.HasSuffix(host, ".chinatax.gov.cn") {
		return ""
	}
	return parsed.String()
}

// InvoiceableView 是给 Folio 的订单金额查询结果。
type InvoiceableView struct {
	InvoiceableBreakdown
	Buyer map[string]string `json:"buyer"`
	Items []InvoiceableItem `json:"items"`
}

// InvoiceableItem 订单商品摘要，只用于展示，不参与金额。
type InvoiceableItem struct {
	ProductID string `json:"product_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	NetAmount string `json:"net_amount"`
}

// LoadInvoiceable 读取订单并计算可开票金额。订单不存在返回 nil, nil。
func LoadInvoiceable(orders ordercontract.Store, payments paymentcontract.Store, orderNo string, now time.Time) (*InvoiceableView, error) {
	order, err := orders.GetByOrderNo(strings.TrimSpace(orderNo))
	if err != nil || order == nil {
		return nil, err
	}
	if order.ParentID != nil {
		rootNo := ""
		if parent, err := orders.GetByID(*order.ParentID); err == nil && parent != nil {
			rootNo = parent.OrderNo
		}
		return &InvoiceableView{
			InvoiceableBreakdown: InvoiceableBreakdown{
				OrderNo: order.OrderNo, RootOrderNo: rootNo, OrderStatus: order.Status,
				Currency: order.Currency, InvoiceableState: "not_root_order", Consistent: true, ComputedAt: now,
			},
		}, nil
	}
	children := order.Children
	ids := []uint{order.ID}
	for _, child := range children {
		ids = append(ids, child.ID)
	}
	payRows, err := payments.ListByOrderID(order.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range ids[1:] {
		more, err := payments.ListByOrderID(id)
		if err != nil {
			return nil, err
		}
		payRows = append(payRows, more...)
	}
	refunds, err := orders.ListRefundRecordsByOrderIDs(ids)
	if err != nil {
		return nil, err
	}
	view := &InvoiceableView{InvoiceableBreakdown: ComputeInvoiceableAmount(order, children, payRows, refunds, now)}
	buyerType := "personal"
	if strings.TrimSpace(order.InvoiceTaxNo) != "" {
		buyerType = "company"
	}
	if strings.TrimSpace(order.InvoiceBuyerType) != "" {
		buyerType = order.InvoiceBuyerType
	}
	view.Buyer = map[string]string{
		"type": buyerType, "name": order.InvoiceTitle, "tax_no": order.InvoiceTaxNo, "email": order.InvoiceEmail,
	}
	for _, item := range order.Items {
		title := ""
		if item.TitleJSON != nil {
			if s, ok := item.TitleJSON["zh-CN"].(string); ok && s != "" {
				title = s
			} else {
				for _, v := range item.TitleJSON {
					if s, ok := v.(string); ok && s != "" {
						title = s
						break
					}
				}
			}
		}
		view.Items = append(view.Items, InvoiceableItem{
			ProductID: strconv.FormatUint(uint64(item.ProductID), 10),
			Title:     title,
			Quantity:  item.Quantity,
			NetAmount: item.TotalPrice.Decimal.StringFixed(2),
		})
	}
	return view, nil
}

// PushInvoiceRequest 把已申请开票的父订单推给 Folio。未配置时直接返回。
func PushInvoiceRequest(cfg config.FolioConfig, orders ordercontract.Store, payments paymentcontract.Store, orderID uint) error {
	if !config.FolioEnabled(cfg) {
		return nil
	}
	order, err := orders.GetByID(orderID)
	if err != nil || order == nil || order.ParentID != nil || !order.InvoiceRequested {
		return err
	}
	view, err := LoadInvoiceable(orders, payments, order.OrderNo, time.Now())
	if err != nil || view == nil {
		return err
	}
	if view.InvoiceableState != "ok" {
		logger.Infow("folio_invoice_skip_not_ok",
			"order_no", order.OrderNo,
			"invoiceable_state", view.InvoiceableState,
		)
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"event_id":       "order_paid:" + order.OrderNo,
		"order_no":       order.OrderNo,
		"currency":       order.Currency,
		"invoice_amount": view.Invoiceable.Decimal.StringFixed(2),
		"buyer":          view.Buyer,
	})
	return postFolio(cfg, "/api/integration/v1/invoice-requests", body, order.OrderNo)
}

// folioOrderLookup 退款根单解析只需按 ID 取订单。
type folioOrderLookup interface {
	GetByID(id uint) (*orderdomain.Order, error)
}

// ResolveFolioRefundRoot 把退款订单归一到父单；子单退款也用父单号通知 Folio。
func ResolveFolioRefundRoot(orders folioOrderLookup, order *orderdomain.Order) (*orderdomain.Order, error) {
	if order == nil {
		return nil, nil
	}
	if order.ParentID == nil || orders == nil {
		return order, nil
	}
	parent, err := orders.GetByID(*order.ParentID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return order, nil
	}
	return parent, nil
}

// PushRefundEvent 把退款通知 Folio。仅通知曾申请开票的父订单；部分退款同样通知，由 Folio 标成异常。
func PushRefundEvent(cfg config.FolioConfig, orders folioOrderLookup, order *orderdomain.Order, record *orderdomain.OrderRefundRecord, full bool) error {
	if !config.FolioEnabled(cfg) || order == nil || record == nil {
		return nil
	}
	root, err := ResolveFolioRefundRoot(orders, order)
	if err != nil {
		return err
	}
	if root == nil || !root.InvoiceRequested {
		return nil
	}
	orderNo := root.OrderNo
	body, _ := json.Marshal(map[string]any{
		"event_id":       fmt.Sprintf("refund:%s:%d", orderNo, record.ID),
		"order_no":       orderNo,
		"refund_amount":  record.Amount.Decimal.StringFixed(2),
		"is_full_refund": full,
	})
	return postFolio(cfg, "/api/integration/v1/refund-events", body, orderNo)
}

func postFolio(cfg config.FolioConfig, path string, body []byte, orderNo string) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Folio-Timestamp", ts)
	req.Header.Set("X-Folio-Nonce", nonce)
	req.Header.Set("X-Folio-Signature", SignFolioRequest(http.MethodPost, path, ts, nonce, body, cfg.Secret))
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warnw("folio_http_error", "path", path, "order_no", orderNo, "error", err)
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 500 {
		logger.Warnw("folio_http_5xx", "path", path, "order_no", orderNo, "status", resp.StatusCode, "body", truncate(string(raw), 512))
		return fmt.Errorf("folio %d: %s", resp.StatusCode, string(raw))
	}
	var envelope struct {
		StatusCode int    `json:"status_code"`
		Msg        string `json:"msg"`
	}
	_ = json.Unmarshal(raw, &envelope)
	if envelope.StatusCode == 503 {
		logger.Warnw("folio_unavailable", "path", path, "order_no", orderNo, "msg", envelope.Msg)
		return fmt.Errorf("folio unavailable: %s", envelope.Msg)
	}
	if resp.StatusCode >= 400 || envelope.StatusCode != 0 {
		// 4xx / 业务校验失败：记日志，不重试（避免金额不一致等死循环）
		logger.Warnw("folio_push_rejected",
			"path", path,
			"order_no", orderNo,
			"http_status", resp.StatusCode,
			"status_code", envelope.StatusCode,
			"msg", envelope.Msg,
			"body", truncate(string(raw), 512),
		)
		return nil
	}
	logger.Infow("folio_push_ok", "path", path, "order_no", orderNo)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
