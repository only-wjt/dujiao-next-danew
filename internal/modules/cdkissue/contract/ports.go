package contract

import (
	"context"
	"time"

	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// RequestFilter 是发码记录列表查询条件。
type RequestFilter struct {
	Status      string
	Plan        string
	OrderNo     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Page        int
	PageSize    int
}

// Store 是实时发码模块的持久化端口。
type Store interface {
	GetConfig(provider string) (*cdkissuedomain.Config, error)
	SaveConfig(config *cdkissuedomain.Config) error
	// SaveConfigExclusive 保存一个启用中的渠道配置，并在同一事务里停用其它渠道。
	SaveConfigExclusive(config *cdkissuedomain.Config) error

	ListBindings(productIDs []uint) ([]cdkissuedomain.Binding, error)
	ReplaceBindings(productID uint, rows []cdkissuedomain.Binding) error
	ListEnabledProductIDs(productIDs []uint) ([]uint, error)

	// CreateRequestIfAbsent 按 (order_id, order_item_id, chunk_index) 幂等建单，已存在时返回已有行。
	CreateRequestIfAbsent(request *cdkissuedomain.IssueRequest) (*cdkissuedomain.IssueRequest, error)
	GetRequest(id uint) (*cdkissuedomain.IssueRequest, error)
	// ClaimRequest 原子地占用一个 pending 请求并累加尝试次数；已被占用或状态已变化时返回 false。
	ClaimRequest(id uint, now, until time.Time) (bool, error)
	// UpdateRequestIfStatus 仅在当前状态等于 expectedStatus 时保存整行，返回是否更新成功。
	UpdateRequestIfStatus(request *cdkissuedomain.IssueRequest, expectedStatus string) (bool, error)
	// MarkCodesDisabled 记录卡台卡密已停用。
	MarkCodesDisabled(id uint, at time.Time) error
	// ListAwaitingFulfillment 返回付款时间在区间内、已付款未交付的自动发货订单项及其快照。
	ListAwaitingFulfillment(paidAfter, paidBefore time.Time, limit int) ([]AwaitingOrderItem, error)
	ListRequestsByOrder(orderID uint) ([]cdkissuedomain.IssueRequest, error)
	ListRequestsByRootOrder(rootOrderID uint) ([]cdkissuedomain.IssueRequest, error)
	ListRequests(filter RequestFilter) ([]cdkissuedomain.IssueRequest, int64, error)
	CountRequestsByStatus(filter RequestFilter) (map[string]int64, error)
	MarkOrderDelivered(orderID uint, deliveredAt time.Time) error
}

// AwaitingOrderItem 是待补发扫描读取的订单项。
type AwaitingOrderItem struct {
	OrderID     uint
	SKUSnapshot jsonmap.JSON
}

// Credentials 是调用卡台所需的凭据。
type Credentials struct {
	BaseURL   string
	AppID     string
	AppSecret string
}

// IssueCommand 是一次发码请求。
type IssueCommand struct {
	Plan           string
	Count          int
	PaymentCountry string
	IdempotencyKey string
	// OrderNo / ExternalRef 仅用于渠道侧检索与对账（danew_cdk），不参与幂等。
	OrderNo     string
	ExternalRef string
}

// IssuedCode 是卡台返回的一张码。
type IssuedCode struct {
	ID             uint
	Code           string
	Plan           string
	FeeAmountMinor int64
}

// IssueResult 是发码结果。
type IssueResult struct {
	Codes    []IssuedCode
	Replayed bool
	// ProviderRequestID 是渠道侧请求号（danew_cdk），用于两边对账。
	ProviderRequestID string
}

// DisableResult 是批量停用结果。
type DisableResult struct {
	Disabled []uint `json:"disabled"`
	Failed   []uint `json:"failed"`
}

// Gateway 是卡台 Open API 端口。返回 *GatewayError 表示卡台给出了明确响应。
type Gateway interface {
	Issue(ctx context.Context, credentials Credentials, command IssueCommand) (*IssueResult, error)
	Overview(ctx context.Context, credentials Credentials) (*cdkissuedomain.Overview, error)
	DisableCDKs(ctx context.Context, credentials Credentials, ids []uint) (*DisableResult, error)
}

// ProductSKU 是绑定校验所需的 SKU 信息。
type ProductSKU struct {
	ID       uint
	Code     string
	IsActive bool
}

// Product 是绑定校验所需的商品信息。
type Product struct {
	ID              uint
	FulfillmentType string
	SKUs            []ProductSKU
}

// ProductReader 读取商品信息。
type ProductReader interface {
	GetProduct(productID uint) (*Product, error)
}

// FulfillmentEnqueuer 重新投递自动发货任务。
type FulfillmentEnqueuer interface {
	Enabled() bool
	EnqueueAutoFulfill(orderID uint) error
}

// IssueOrderItem 是需要发码的订单项。
type IssueOrderItem struct {
	OrderItemID uint
	ProductID   uint
	SKUID       uint
	Quantity    int
	Title       jsonmap.JSON
	// Realtime/Provider/Plan/PaymentCountry 来自下单时固化在订单项上的快照。
	Realtime       bool
	Provider       string
	Plan           string
	PaymentCountry string
}

// NewIssueOrderItem 从订单项快照构造发码订单项。
func NewIssueOrderItem(orderItemID, productID, skuID uint, quantity int, title, skuSnapshot jsonmap.JSON) IssueOrderItem {
	provider, plan, country, ok := cdkissuedomain.ReadItemSnapshotProvider(skuSnapshot)
	return IssueOrderItem{
		OrderItemID:    orderItemID,
		ProductID:      productID,
		SKUID:          skuID,
		Quantity:       quantity,
		Title:          title,
		Realtime:       ok,
		Provider:       provider,
		Plan:           plan,
		PaymentCountry: country,
	}
}

// IssueOrder 是发货时交给实时发码模块的订单快照。
type IssueOrder struct {
	OrderID     uint
	RootOrderID uint
	OrderNo     string
	Items       []IssueOrderItem
}

// IssuedItemCodes 是某个订单项拿到的全部码（按分片顺序）。
type IssuedItemCodes map[uint][]string

// RealtimeIssuer 是交付模块依赖的实时发码端口。
type RealtimeIssuer interface {
	// RealtimeItems 返回订单中走实时发码的订单项 ID。
	RealtimeItems(order IssueOrder) (map[uint]bool, error)
	// EnsureIssued 确保实时发码订单项都已拿到码；可安全重复调用。
	EnsureIssued(ctx context.Context, order IssueOrder) (IssuedItemCodes, error)
	// MarkDelivered 在交付事务成功后清除暂存的密文。
	MarkDelivered(orderID uint) error
}

// RealtimeIssueSnapshot 是下单时确定的实时发码参数。
type RealtimeIssueSnapshot struct {
	Provider       string
	Plan           string
	PaymentCountry string
}

// OrderabilityChecker 是下单校验依赖的端口。
type OrderabilityChecker interface {
	// CheckOrderable 返回该 SKU 的实时发码参数（非实时发码返回 nil）；不可售时返回 ErrNotOrderable。
	CheckOrderable(productID, skuID uint, quantity int) (*RealtimeIssueSnapshot, error)
}

// RealtimeProductResolver 是库存展示依赖的端口。
type RealtimeProductResolver interface {
	RealtimeProductIDs(productIDs []uint) (map[uint]bool, error)
}
