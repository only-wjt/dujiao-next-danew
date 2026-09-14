package format

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

var notificationTemplateVariablePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

func BuildTemplateVariables(payload queue.NotificationDispatchPayload) map[string]interface{} {
	data := CloneVariables(payload.Data)
	if data == nil {
		data = map[string]interface{}{}
	}
	data["event_type"] = strings.ToLower(strings.TrimSpace(payload.EventType))
	data["biz_type"] = strings.TrimSpace(payload.BizType)
	data["biz_id"] = fmt.Sprintf("%d", payload.BizID)
	data["occurred_at"] = time.Now().Format("2006-01-02 15:04:05")
	locale := ResolveLocale(payload.Locale, constants.LocaleZhCN)
	data["invoice_notice"] = InvoiceNoticeLine(locale)
	data["invoice_block"] = BuildInvoiceBlock(data, locale)
	if !invoiceRequestedFromVariables(data) {
		data["invoice_notice"] = ""
	}
	return data
}

func RenderTemplate(tmpl string, variables map[string]interface{}) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	return notificationTemplateVariablePattern.ReplaceAllStringFunc(tmpl, func(matched string) string {
		submatch := notificationTemplateVariablePattern.FindStringSubmatch(matched)
		if len(submatch) != 2 {
			return matched
		}
		value, ok := variables[strings.TrimSpace(submatch[1])]
		if !ok {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	})
}

func ResolveLocale(locale, fallback string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = strings.TrimSpace(fallback)
	}
	return normalizeNotificationLocale(locale)
}

func ComposePlainTextMessage(title, body string) string {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		return body
	}
	if body == "" {
		return title
	}
	return title + "\n\n" + body
}

func ComposeTelegramMessage(title, body string) string {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		return telegramHTMLBody(body)
	}
	escapedTitle := "<b>" + html.EscapeString(title) + "</b>"
	if body == "" {
		return escapedTitle
	}
	return escapedTitle + "\n\n" + telegramHTMLBody(body)
}

func InvoiceNoticeLine(locale string) string {
	return localizedNotificationText(locale, "⚠️ 本单需要开发票", "⚠️ 本單需要開發票", "⚠️ This order requires an invoice")
}

func BuildInvoiceBlock(variables map[string]interface{}, locale string) string {
	if !invoiceRequestedFromVariables(variables) {
		return ""
	}
	amount := notificationVariableText(variables["invoice_amount"])
	if amount == "" {
		amount = notificationVariableText(variables["amount"])
	}
	currency := notificationVariableText(variables["currency"])
	amountLine := amount
	if currency != "" {
		amountLine = strings.TrimSpace(amount + " " + currency)
	}
	lines := []string{InvoiceNoticeLine(locale)}
	lines = append(lines, localizedNotificationText(locale, "发票抬头：", "發票抬頭：", "Invoice Title: ")+notificationVariableText(variables["invoice_title"]))
	if note := notificationVariableText(variables["invoice_note"]); note != "" {
		lines = append(lines, localizedNotificationText(locale, "发票备注：", "發票備註：", "Invoice Note: ")+note)
	}
	lines = append(lines, localizedNotificationText(locale, "开票金额：", "開票金額：", "Invoice Amount: ")+amountLine)
	lines = append(lines, localizedNotificationText(locale, "收票邮箱：", "收票信箱：", "Invoice Email: ")+notificationVariableText(variables["invoice_email"]))
	return strings.Join(lines, "\n")
}

func AppendInvoiceBlockIfNeeded(body string, variables map[string]interface{}, locale string) string {
	block := BuildInvoiceBlock(variables, locale)
	if block == "" {
		return body
	}
	notice := InvoiceNoticeLine(locale)
	if strings.Contains(body, notice) || strings.Contains(body, block) {
		return body
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return block
	}
	return body + "\n\n" + block
}

func invoiceRequestedFromVariables(variables map[string]interface{}) bool {
	if variables == nil {
		return false
	}
	switch value := variables["invoice_requested"].(type) {
	case bool:
		return value
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		return normalized == "true" || normalized == "1" || normalized == "yes"
	default:
		normalized := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
		return normalized == "true" || normalized == "1"
	}
}

func notificationVariableText(value interface{}) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func telegramHTMLBody(body string) string {
	if body == "" {
		return ""
	}
	notices := []string{
		"⚠️ 本单需要开发票",
		"⚠️ 本單需要開發票",
		"⚠️ This order requires an invoice",
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		escaped := html.EscapeString(line)
		trimmed := strings.TrimSpace(line)
		for _, notice := range notices {
			if trimmed == notice {
				escaped = "<b>" + escaped + "</b>"
				break
			}
		}
		lines[i] = escaped
	}
	return strings.Join(lines, "\n")
}

func JSONToMap(data jsonmap.JSON) map[string]interface{} {
	if data == nil {
		return map[string]interface{}{}
	}
	result := make(map[string]interface{}, len(data))
	for key, value := range data {
		result[key] = value
	}
	return result
}

func CloneVariables(data map[string]interface{}) map[string]interface{} {
	if len(data) == 0 {
		return map[string]interface{}{}
	}
	result := make(map[string]interface{}, len(data))
	for key, value := range data {
		result[key] = value
	}
	return result
}

func normalizeNotificationLocale(locale string) string {
	return settingsmessaging.NormalizeNotificationLocale(locale)
}

func normalizeNotificationInventoryAlertInterval(seconds int) int {
	return settingsmessaging.NormalizeNotificationInventoryAlertInterval(seconds)
}

func normalizeNotificationPaymentOrderAlertInterval(seconds int) int {
	return settingsmessaging.NormalizeNotificationPaymentOrderAlertInterval(seconds)
}
