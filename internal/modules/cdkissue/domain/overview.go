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
	// Issuable 仅 danew_cdk 下发：为 false 时其主台不可用，不能下单。
	Issuable *bool `json:"issuable,omitempty"`
	// Unavailable 是 Issuable=false 时的原因。
	Unavailable string `json:"unavailable,omitempty"`
	// Client 仅 danew_cdk 下发：本卡网客户端的授权范围与当日用量。
	Client *ClientLimits `json:"client,omitempty"`
}

// ClientLimits 是 danew_cdk 对本卡网客户端的授权范围。
type ClientLimits struct {
	AllowedPlans     []string `json:"allowed_plans"`
	AllowedCountries []string `json:"allowed_countries"`
	DailyCodeLimit   int      `json:"daily_code_limit"`
	IssuedToday      int      `json:"issued_today"`
}

// CanIssue 渠道没有声明不可发码时视为可发码（ZovoCard 不下发该字段）。
func (o *Overview) CanIssue() bool {
	return o != nil && (o.Issuable == nil || *o.Issuable)
}

// ClientAllows 判断 danew_cdk 客户端是否被授权该套餐与地区；ZovoCard 无此限制。
// 与 danew_cdk 同一口径：空地区按卡台默认地区 PH 计，授权地区为空时只允许 PH。
func (o *Overview) ClientAllows(plan, country string) bool {
	if o == nil || o.Client == nil {
		return true
	}
	planOK := false
	for _, p := range o.Client.AllowedPlans {
		if NormalizePlan(p) == NormalizePlan(plan) {
			planOK = true
			break
		}
	}
	if !planOK {
		return false
	}
	country = NormalizeCountry(country)
	if country == "" {
		country = DefaultPaymentCountry
	}
	if len(o.Client.AllowedCountries) == 0 {
		return country == DefaultPaymentCountry
	}
	for _, c := range o.Client.AllowedCountries {
		if NormalizeCountry(c) == country {
			return true
		}
	}
	return false
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
