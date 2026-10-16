package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/jsonslice"
)

const (
	// RequestStatusPending 已建单，尚未拿到码（首次调用或等待重试）。
	RequestStatusPending = "pending"
	// RequestStatusSucceeded 卡台已返回完整码。
	RequestStatusSucceeded = "succeeded"
	// RequestStatusFailed 卡台明确拒绝（4xx），未扣费，等待管理员处理。
	RequestStatusFailed = "failed"
	// RequestStatusUnknown 多次重试仍无法确认结果，可能已扣费，只允许原键重试。
	RequestStatusUnknown = "unknown"
)

// MaxCodesPerRequest 是卡台单次发码上限。
const MaxCodesPerRequest = 200

// MaxAutoAttempts 是自动重试的最大调用次数，超过后转为 unknown。
const MaxAutoAttempts = 8

// StalePendingAfter 之后的 pending 视为卡住，允许管理员原键重试。
const StalePendingAfter = 10 * time.Minute

// IssueRequest 记录一次向卡台的发码请求（一个订单项的一个分片）。
//
// 码在交付前以密文暂存在 CodesEncrypted，交付事务写入卡密表后清空，
// 保证任一时刻只有一份明文来源。
type IssueRequest struct {
	ID              uint            `gorm:"primarykey" json:"id"`
	RootOrderID     uint            `gorm:"not null;index" json:"root_order_id"`
	OrderID         uint            `gorm:"not null;uniqueIndex:idx_cdk_issue_request_item_chunk" json:"order_id"`
	OrderNo         string          `gorm:"type:varchar(64);not null;index" json:"order_no"`
	OrderItemID     uint            `gorm:"not null;uniqueIndex:idx_cdk_issue_request_item_chunk" json:"order_item_id"`
	ChunkIndex      int             `gorm:"not null;default:0;uniqueIndex:idx_cdk_issue_request_item_chunk" json:"chunk_index"`
	ProductID       uint            `gorm:"not null;index" json:"product_id"`
	ItemTitle       jsonmap.JSON    `gorm:"type:json" json:"item_title"`
	SKUID           uint            `gorm:"column:sku_id;not null;default:0" json:"sku_id"`
	Provider        string          `gorm:"type:varchar(32);not null;default:'zovocard'" json:"provider"`
	Plan            string          `gorm:"type:varchar(64);not null;index" json:"plan"`
	PaymentCountry  string          `gorm:"type:varchar(8);not null;default:''" json:"payment_country"`
	Count           int             `gorm:"not null" json:"count"`
	KeyVersion      int             `gorm:"not null;default:0" json:"key_version"`
	IdempotencyKey  string          `gorm:"type:varchar(128);not null;index" json:"idempotency_key"`
	Status          string          `gorm:"type:varchar(16);not null;index" json:"status"`
	Attempts        int             `gorm:"not null;default:0" json:"attempts"`
	LastHTTPStatus  int             `gorm:"column:last_http_status;not null;default:0" json:"last_http_status"`
	LastErrorCode   string          `gorm:"type:varchar(64);not null;default:''" json:"last_error_code"`
	LastErrorMsg    string          `gorm:"type:varchar(500);not null;default:''" json:"last_error_msg"`
	IssuedCount     int             `gorm:"not null;default:0" json:"issued_count"`
	FeeAmountMinor  int64           `gorm:"not null;default:0" json:"fee_amount_minor"`
	ProviderCDKIDs  jsonslice.Uints `gorm:"column:provider_cdk_ids;type:json" json:"provider_cdk_ids"`
	CodesEncrypted  string          `gorm:"type:text" json:"-"`
	DeliveredAt     *time.Time      `json:"delivered_at"`
	CodesDisabledAt *time.Time      `json:"codes_disabled_at"`
	CreatedAt       time.Time       `gorm:"index" json:"created_at"`
	UpdatedAt       time.Time       `gorm:"index" json:"updated_at"`
}

// TableName 指定表名。
func (IssueRequest) TableName() string {
	return "cdk_issue_requests"
}

// BuildIdempotencyKey 生成稳定幂等键：同一订单项分片在同一 key 版本下永远相同。
func BuildIdempotencyKey(orderNo string, orderItemID uint, chunk, keyVersion int) string {
	orderNo = strings.TrimSpace(orderNo)
	key := fmt.Sprintf("dj-%s-%d-%d", orderNo, orderItemID, chunk)
	if keyVersion > 0 {
		key = fmt.Sprintf("%s-r%d", key, keyVersion)
	}
	return key
}

// SplitCount 把购买数量切成不超过卡台上限的分片。
func SplitCount(quantity int) []int {
	if quantity <= 0 {
		return nil
	}
	chunks := make([]int, 0, (quantity+MaxCodesPerRequest-1)/MaxCodesPerRequest)
	for quantity > 0 {
		n := quantity
		if n > MaxCodesPerRequest {
			n = MaxCodesPerRequest
		}
		chunks = append(chunks, n)
		quantity -= n
	}
	return chunks
}

// RetryMode 是管理员重试方式。
type RetryMode string

const (
	// RetrySameKey 沿用原幂等键：卡台若已发码会原样返回，不会重复扣费。
	RetrySameKey RetryMode = "same_key"
	// RetryNewKey 换新幂等键：仅允许在卡台明确拒绝（未扣费）后使用。
	RetryNewKey RetryMode = "new_key"
)

// AllowedRetryMode 返回该状态下允许的重试方式；空表示不可重试。
func (r IssueRequest) AllowedRetryMode(now time.Time) RetryMode {
	switch r.Status {
	case RequestStatusFailed:
		return RetryNewKey
	case RequestStatusUnknown:
		return RetrySameKey
	case RequestStatusPending:
		if now.Sub(r.UpdatedAt) >= StalePendingAfter {
			return RetrySameKey
		}
	}
	return ""
}

// CanDisableCodes 表示是否可以去卡台停用本请求发出的码。
func (r IssueRequest) CanDisableCodes() bool {
	return r.Status == RequestStatusSucceeded && len(r.ProviderCDKIDs) > 0 && r.CodesDisabledAt == nil
}
