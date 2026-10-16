package domain

import (
	"strings"
	"time"
)

// ProviderZovoCard 是当前唯一支持的实时发码卡台。
const ProviderZovoCard = "zovocard"

// Binding 描述商品（sku_id=0）或 SKU 与卡台套餐的对应关系。
//
// 每个启用实时发码的商品都有一行 sku_id=0 的默认绑定，它决定商品是否走实时发码；
// sku_id>0 的行只覆盖该 SKU 的套餐/地区，留空表示跟随商品默认。
type Binding struct {
	ID             uint      `gorm:"primarykey" json:"id"`
	ProductID      uint      `gorm:"not null;uniqueIndex:idx_cdk_issue_binding_product_sku" json:"product_id"`
	SKUID          uint      `gorm:"column:sku_id;not null;default:0;uniqueIndex:idx_cdk_issue_binding_product_sku" json:"sku_id"`
	Provider       string    `gorm:"type:varchar(32);not null;default:'zovocard'" json:"provider"`
	Plan           string    `gorm:"type:varchar(64);not null;default:''" json:"plan"`
	PaymentCountry string    `gorm:"type:varchar(8);not null;default:''" json:"payment_country"`
	Enabled        bool      `gorm:"not null;default:false" json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (Binding) TableName() string {
	return "cdk_issue_bindings"
}

// ProductBinding 是一个商品的完整绑定视图。
type ProductBinding struct {
	ProductID      uint
	Enabled        bool
	Plan           string
	PaymentCountry string
	SKUs           map[uint]SKUBinding
}

// SKUBinding 是单个 SKU 的套餐覆盖。
type SKUBinding struct {
	Plan           string
	PaymentCountry string
}

// NewProductBinding 从持久化行组装商品绑定；没有默认行时返回 nil。
func NewProductBinding(productID uint, rows []Binding) *ProductBinding {
	var result *ProductBinding
	skus := make(map[uint]SKUBinding)
	for _, row := range rows {
		if row.ProductID != productID {
			continue
		}
		if row.SKUID == 0 {
			result = &ProductBinding{
				ProductID:      productID,
				Enabled:        row.Enabled,
				Plan:           NormalizePlan(row.Plan),
				PaymentCountry: NormalizeCountry(row.PaymentCountry),
			}
			continue
		}
		skus[row.SKUID] = SKUBinding{
			Plan:           NormalizePlan(row.Plan),
			PaymentCountry: NormalizeCountry(row.PaymentCountry),
		}
	}
	if result != nil {
		result.SKUs = skus
	}
	return result
}

// Resolve 返回 SKU 生效的套餐与地区；地区为空表示使用全局默认。
func (b *ProductBinding) Resolve(skuID uint) (plan, country string) {
	if b == nil {
		return "", ""
	}
	plan, country = b.Plan, b.PaymentCountry
	if override, ok := b.SKUs[skuID]; ok {
		if override.Plan != "" {
			plan = override.Plan
		}
		if override.PaymentCountry != "" {
			country = override.PaymentCountry
		}
	}
	return plan, country
}

// NormalizePlan 统一套餐键格式。
func NormalizePlan(plan string) string {
	return strings.ToLower(strings.TrimSpace(plan))
}

// NormalizeCountry 统一付款地区格式。
func NormalizeCountry(country string) string {
	return strings.ToUpper(strings.TrimSpace(country))
}

// itemSnapshotKey 是订单项 SKU 快照中记录实时发码参数的键。
const itemSnapshotKey = "cdk_issue"

// ApplyItemSnapshot 在下单时把实时发码套餐固化到订单项快照，
// 之后修改或关闭商品绑定都不会改变已下单订单的发码方式。
func ApplyItemSnapshot(snapshot map[string]interface{}, plan, country string) {
	if snapshot == nil {
		return
	}
	snapshot[itemSnapshotKey] = map[string]interface{}{
		"provider":        ProviderZovoCard,
		"plan":            NormalizePlan(plan),
		"payment_country": NormalizeCountry(country),
	}
}

// ReadItemSnapshot 读取订单项上的实时发码快照。
func ReadItemSnapshot(snapshot map[string]interface{}) (plan, country string, ok bool) {
	if snapshot == nil {
		return "", "", false
	}
	raw, exists := snapshot[itemSnapshotKey].(map[string]interface{})
	if !exists {
		return "", "", false
	}
	plan, _ = raw["plan"].(string)
	country, _ = raw["payment_country"].(string)
	plan = NormalizePlan(plan)
	if plan == "" {
		return "", "", false
	}
	return plan, NormalizeCountry(country), true
}
