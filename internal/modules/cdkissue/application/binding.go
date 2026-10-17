package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
	cdkissuedomain "github.com/dujiao-next/internal/modules/cdkissue/domain"
)

// SKUBindingView 是 SKU 套餐覆盖。
type SKUBindingView struct {
	SKUID          uint   `json:"sku_id"`
	Plan           string `json:"plan"`
	PaymentCountry string `json:"payment_country"`
}

// BindingView 是商品的实时发码绑定。
type BindingView struct {
	ProductID      uint             `json:"product_id"`
	Enabled        bool             `json:"enabled"`
	Plan           string           `json:"plan"`
	PaymentCountry string           `json:"payment_country"`
	SKUs           []SKUBindingView `json:"skus"`
}

// BindingInput 是后台保存绑定的请求。
type BindingInput struct {
	Enabled        bool             `json:"enabled"`
	Plan           string           `json:"plan"`
	PaymentCountry string           `json:"payment_country"`
	SKUs           []SKUBindingView `json:"skus"`
}

// GetBinding 返回商品绑定；未配置时返回关闭状态。
func (s *Service) GetBinding(productID uint) (BindingView, error) {
	view := BindingView{ProductID: productID, SKUs: []SKUBindingView{}}
	rows, err := s.store.ListBindings([]uint{productID})
	if err != nil {
		return view, err
	}
	binding := cdkissuedomain.NewProductBinding(productID, rows)
	if binding == nil {
		return view, nil
	}
	view.Enabled = binding.Enabled
	view.Plan = binding.Plan
	view.PaymentCountry = binding.PaymentCountry
	for _, row := range rows {
		if row.SKUID == 0 {
			continue
		}
		view.SKUs = append(view.SKUs, SKUBindingView{
			SKUID:          row.SKUID,
			Plan:           cdkissuedomain.NormalizePlan(row.Plan),
			PaymentCountry: cdkissuedomain.NormalizeCountry(row.PaymentCountry),
		})
	}
	return view, nil
}

// SaveBinding 校验并保存商品绑定。启用时每个启用的 SKU 都必须解析出可售套餐。
func (s *Service) SaveBinding(ctx context.Context, productID uint, input BindingInput) (BindingView, error) {
	if productID == 0 {
		return BindingView{}, cdkissuecontract.ErrInvalidInput
	}
	if s.products == nil {
		return BindingView{}, cdkissuecontract.ErrInvalidInput
	}
	product, err := s.products.GetProduct(productID)
	if err != nil {
		return BindingView{}, err
	}
	if product == nil {
		return BindingView{}, cdkissuecontract.ErrNotFound
	}

	skuByID := make(map[uint]cdkissuecontract.ProductSKU, len(product.SKUs))
	for _, sku := range product.SKUs {
		skuByID[sku.ID] = sku
	}

	rows := []cdkissuedomain.Binding{{
		ProductID:      productID,
		SKUID:          0,
		Provider:       cdkissuedomain.ProviderZovoCard,
		Plan:           cdkissuedomain.NormalizePlan(input.Plan),
		PaymentCountry: cdkissuedomain.NormalizeCountry(input.PaymentCountry),
		Enabled:        input.Enabled,
	}}
	seen := make(map[uint]bool)
	for _, item := range input.SKUs {
		if item.SKUID == 0 || seen[item.SKUID] {
			continue
		}
		if _, ok := skuByID[item.SKUID]; !ok {
			return BindingView{}, &cdkissuecontract.BindingError{Reason: fmt.Sprintf("SKU #%d 不属于该商品", item.SKUID)}
		}
		seen[item.SKUID] = true
		plan := cdkissuedomain.NormalizePlan(item.Plan)
		country := cdkissuedomain.NormalizeCountry(item.PaymentCountry)
		if plan == "" && country == "" {
			continue
		}
		rows = append(rows, cdkissuedomain.Binding{
			ProductID:      productID,
			SKUID:          item.SKUID,
			Provider:       cdkissuedomain.ProviderZovoCard,
			Plan:           plan,
			PaymentCountry: country,
			Enabled:        input.Enabled,
		})
	}

	if input.Enabled {
		if strings.TrimSpace(product.FulfillmentType) != constants.FulfillmentTypeAuto {
			return BindingView{}, &cdkissuecontract.BindingError{Reason: "只有「自动发货」商品才能使用实时发码"}
		}
		if err := s.validateEnabledBinding(ctx, productID, product, rows); err != nil {
			return BindingView{}, err
		}
	}

	if err := s.store.ReplaceBindings(productID, rows); err != nil {
		return BindingView{}, err
	}
	return s.GetBinding(productID)
}

func (s *Service) validateEnabledBinding(ctx context.Context, productID uint, product *cdkissuecontract.Product, rows []cdkissuedomain.Binding) error {
	binding := cdkissuedomain.NewProductBinding(productID, rows)
	config, err := s.activeConfig()
	if err != nil {
		return err
	}
	if !config.Configured() {
		return &cdkissuecontract.BindingError{Reason: "实时发码渠道尚未配置，请先到「实时发码」设置页填写并启用渠道"}
	}
	label := providerLabel(config.Provider)

	// 无 SKU 的商品使用默认 SKU（sku_id=0）。
	targets := []cdkissuecontract.ProductSKU{{ID: 0, Code: "DEFAULT", IsActive: true}}
	if len(product.SKUs) > 0 {
		targets = targets[:0]
		for _, sku := range product.SKUs {
			if sku.IsActive {
				targets = append(targets, sku)
			}
		}
	}

	overview, overviewErr := s.cachedOverview(ctx, config, false)
	if overviewErr != nil {
		// 卡台暂不可达时只做本地校验，避免一次网络抖动挡住保存。
		logger.Warnw("cdk_issue_binding_overview_unavailable", "product_id", productID, "error", overviewErr)
	}
	for _, sku := range targets {
		plan, country := binding.Resolve(sku.ID)
		skuLabel := sku.Code
		if skuLabel == "" {
			skuLabel = fmt.Sprintf("#%d", sku.ID)
		}
		if plan == "" {
			return &cdkissuecontract.BindingError{Reason: fmt.Sprintf("SKU %s 没有设置套餐，默认套餐也为空", skuLabel)}
		}
		if overview == nil {
			continue
		}
		info, ok := overview.FindPlan(plan)
		if !ok {
			return &cdkissuecontract.BindingError{Reason: fmt.Sprintf("SKU %s 的套餐 %s 在 %s 不存在", skuLabel, plan, label)}
		}
		if !info.Purchasable {
			return &cdkissuecontract.BindingError{Reason: fmt.Sprintf("SKU %s 的套餐 %s 在 %s 已停售", skuLabel, plan, label)}
		}
		effectiveCountry := config.ResolveCountry(country)
		if !overview.SupportsCountry(effectiveCountry) {
			return &cdkissuecontract.BindingError{Reason: fmt.Sprintf("SKU %s 的付款地区 %s 不受 %s 支持", skuLabel, effectiveCountry, label)}
		}
		if !overview.ClientAllows(plan, effectiveCountry) {
			return &cdkissuecontract.BindingError{Reason: fmt.Sprintf("SKU %s 的套餐 %s / 地区 %s 未在 danew_cdk 卡网客户端授权范围内", skuLabel, plan, effectiveCountry)}
		}
	}
	return nil
}

func (s *Service) loadBindings(productIDs []uint) (map[uint]*cdkissuedomain.ProductBinding, error) {
	rows, err := s.store.ListBindings(productIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[uint]*cdkissuedomain.ProductBinding, len(productIDs))
	for _, productID := range productIDs {
		if binding := cdkissuedomain.NewProductBinding(productID, rows); binding != nil && binding.Enabled {
			out[productID] = binding
		}
	}
	return out, nil
}

// RealtimeProductIDs 返回启用实时发码的商品。
func (s *Service) RealtimeProductIDs(productIDs []uint) (map[uint]bool, error) {
	out := make(map[uint]bool)
	if len(productIDs) == 0 {
		return out, nil
	}
	ids, err := s.store.ListEnabledProductIDs(productIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// RealtimeStock 返回实时发码商品的展示库存：生效时 -1（不限量），关闭时 0（售罄）。
func (s *Service) RealtimeStock(productIDs []uint) (map[uint]int64, error) {
	realtime, err := s.RealtimeProductIDs(productIDs)
	if err != nil || len(realtime) == 0 {
		return map[uint]int64{}, err
	}
	config, err := s.activeConfig()
	if err != nil {
		return nil, err
	}
	stock := int64(0)
	if config.Active() {
		stock = -1
	}
	out := make(map[uint]int64, len(realtime))
	for id := range realtime {
		out[id] = stock
	}
	return out, nil
}
