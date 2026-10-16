package application

import (
	"context"
	"time"

	"github.com/dujiao-next/internal/logger"
	cdkissuecontract "github.com/dujiao-next/internal/modules/cdkissue/contract"
)

const orderableCheckTimeout = 5 * time.Second

// CheckOrderable 下单前校验：返回该 SKU 的实时发码参数（非实时发码返回 nil）；
// 不可售时返回 ErrNotOrderable。返回的套餐与地区会固化到订单项上。
//
// 卡台暂时不可达时放行（订单付款后由发货重试兜底），只有明确停售、地区不支持或
// 余额低于预警线时才拦截下单。
func (s *Service) CheckOrderable(productID, skuID uint, quantity int) (*cdkissuecontract.RealtimeIssueSnapshot, error) {
	bindings, err := s.loadBindings([]uint{productID})
	if err != nil {
		return nil, err
	}
	binding, ok := bindings[productID]
	if !ok {
		return nil, nil
	}
	config, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	if !config.Active() {
		return nil, cdkissuecontract.ErrNotOrderable
	}
	plan, country := binding.Resolve(skuID)
	if plan == "" || quantity <= 0 {
		return nil, cdkissuecontract.ErrNotOrderable
	}
	snapshot := &cdkissuecontract.RealtimeIssueSnapshot{Plan: plan, PaymentCountry: config.ResolveCountry(country)}

	ctx, cancel := context.WithTimeout(context.Background(), orderableCheckTimeout)
	defer cancel()
	overview, err := s.cachedOverview(ctx, config, false)
	if err != nil {
		logger.Warnw("cdk_issue_orderable_overview_unavailable", "product_id", productID, "sku_id", skuID, "error", err)
		return snapshot, nil
	}
	info, found := overview.FindPlan(plan)
	if !found || !info.Purchasable {
		return nil, cdkissuecontract.ErrNotOrderable
	}
	if !overview.SupportsCountry(snapshot.PaymentCountry) {
		return nil, cdkissuecontract.ErrNotOrderable
	}
	if overview.Balance.SpendableBalance.LessThan(config.MinSpendableBalance) {
		if s.alerts != nil {
			if alertErr := s.alerts.NotifyLowBalance(overview.Balance.SpendableBalance, config.MinSpendableBalance); alertErr != nil {
				logger.Warnw("cdk_issue_low_balance_alert_failed", "error", alertErr)
			}
		}
		return nil, cdkissuecontract.ErrNotOrderable
	}
	return snapshot, nil
}
