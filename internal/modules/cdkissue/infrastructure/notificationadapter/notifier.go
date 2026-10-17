package notificationadapter

import (
	"fmt"
	"time"

	"github.com/dujiao-next/internal/constants"
	cdkissueapp "github.com/dujiao-next/internal/modules/cdkissue/application"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	notificationcontract "github.com/dujiao-next/internal/modules/notification/contract"
	"github.com/dujiao-next/internal/shared/jsonmap"

	"github.com/shopspring/decimal"
)

// Notifier 把实时发码异常推送到通知中心的「异常告警」场景。
type Notifier struct {
	enqueuer notificationcontract.NotificationEnqueuer
}

var _ cdkissueapp.AlertNotifier = (*Notifier)(nil)

// New 创建告警适配器；enqueuer 为空时返回 nil。
func New(enqueuer notificationcontract.NotificationEnqueuer) cdkissueapp.AlertNotifier {
	if enqueuer == nil {
		return nil
	}
	return &Notifier{enqueuer: enqueuer}
}

// NotifyIssueAlert 发码失败或结果不明时告警。
func (n *Notifier) NotifyIssueAlert(request *cdkissuedomain.IssueRequest, message string) error {
	statusLabel := "发码失败"
	if request.Status == cdkissuedomain.RequestStatusUnknown {
		statusLabel = "发码结果不明（请先到卡台核实，勿换新键重发）"
	}
	return n.enqueuer.Enqueue(notificationcontract.EnqueueInput{
		EventType: constants.NotificationEventExceptionAlert,
		BizType:   constants.NotificationBizTypeCDKIssue,
		BizID:     request.ID,
		Data: jsonmap.JSON{
			"alert_type":      providerName(request.Provider) + " 实时发码",
			"alert_type_key":  "cdk_issue_" + request.Status,
			"alert_level":     "critical",
			"alert_value":     request.Status,
			"alert_threshold": "-",
			"message": fmt.Sprintf("订单 %s（%s × %d）%s：%s %s",
				request.OrderNo, request.Plan, request.Count, statusLabel, request.LastErrorCode, message),
			"order_no":    request.OrderNo,
			"request_id":  request.ID,
			"occurred_at": time.Now().Format("2006-01-02 15:04:05"),
		},
	})
}

func providerName(provider string) string {
	if cdkissuedomain.NormalizeProvider(provider) == cdkissuedomain.ProviderDanewCDK {
		return "danew_cdk"
	}
	return "ZovoCard"
}

// NotifyLowBalance 卡台可用余额低于预警线时告警（通知中心按内容去重）。
func (n *Notifier) NotifyLowBalance(spendable, threshold decimal.Decimal) error {
	return n.enqueuer.Enqueue(notificationcontract.EnqueueInput{
		EventType: constants.NotificationEventExceptionAlert,
		BizType:   constants.NotificationBizTypeCDKIssue,
		BizID:     0,
		Data: jsonmap.JSON{
			"alert_type":      "实时发码余额不足",
			"alert_type_key":  "cdk_issue_low_balance",
			"alert_level":     "warning",
			"alert_value":     spendable.StringFixed(2),
			"alert_threshold": threshold.StringFixed(2),
			"message":         fmt.Sprintf("发码卡台可用余额 %s U 低于预警线 %s U，实时发码商品已暂停下单，请尽快充值。", spendable.StringFixed(2), threshold.StringFixed(2)),
		},
	})
}

// NotifyRefundDisableFailed 订单已退款但卡台卡密停用失败，可能已被兑换造成损失。
func (n *Notifier) NotifyRefundDisableFailed(orderNo string, failed int, message string) error {
	return n.enqueuer.Enqueue(notificationcontract.EnqueueInput{
		EventType: constants.NotificationEventExceptionAlert,
		BizType:   constants.NotificationBizTypeCDKIssue,
		BizID:     0,
		Data: jsonmap.JSON{
			"alert_type":      "实时发码退款停码",
			"alert_type_key":  "cdk_issue_refund_disable_failed",
			"alert_level":     "critical",
			"alert_value":     fmt.Sprintf("%d", failed),
			"alert_threshold": "0",
			"message":         fmt.Sprintf("订单 %s 已全额退款，但卡台卡密停用失败：%s", orderNo, message),
			"order_no":        orderNo,
		},
	})
}
