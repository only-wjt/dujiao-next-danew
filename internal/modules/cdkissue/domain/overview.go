package domain

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// Plan 是卡台可发码套餐。
type Plan struct {
	Key                 string `json:"key"`
	Label               string `json:"label"`
	Purchasable         bool   `json:"purchasable"`
	ServiceFeeUSDMinor  int64  `json:"service_fee_usd_minor"`
	SortOrder           int    `json:"sort_order"`
	CheckoutCurrency    string `json:"checkout_currency,omitempty"`
	CheckoutAmountMinor int64  `json:"checkout_amount_minor,omitempty"`
}

// PaymentRegion 是卡台支持的付款地区。
type PaymentRegion struct {
	Country  string `json:"country"`
	Currency string `json:"currency"`
}

// Balance 是卡台账户余额（USD）。
type Balance struct {
	Balance              decimal.Decimal `json:"balance"`
	SpendableBalance     decimal.Decimal `json:"spendable_balance"`
	AccountReserveAmount decimal.Decimal `json:"account_reserve_amount"`
}

// Overview 汇总余额、可售套餐与付款地区。
type Overview struct {
	Balance        Balance         `json:"balance"`
	Plans          []Plan          `json:"plans"`
	PaymentRegions []PaymentRegion `json:"payment_regions"`
	CheckedAt      time.Time       `json:"checked_at"`
}

// SortPlans 按卡台排序权重排列，权重相同时按键名。
func SortPlans(plans []Plan) {
	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].SortOrder != plans[j].SortOrder {
			return plans[i].SortOrder < plans[j].SortOrder
		}
		return plans[i].Key < plans[j].Key
	})
}

// FindPlan 查找套餐。
func (o *Overview) FindPlan(key string) (Plan, bool) {
	if o == nil {
		return Plan{}, false
	}
	key = NormalizePlan(key)
	for _, plan := range o.Plans {
		if plan.Key == key {
			return plan, true
		}
	}
	return Plan{}, false
}

// SupportsCountry 判断付款地区是否可用；卡台未下发地区清单时只认 PH。
func (o *Overview) SupportsCountry(country string) bool {
	country = NormalizeCountry(country)
	if o == nil || len(o.PaymentRegions) == 0 {
		return country == DefaultPaymentCountry
	}
	for _, region := range o.PaymentRegions {
		if NormalizeCountry(region.Country) == country {
			return true
		}
	}
	return false
}
