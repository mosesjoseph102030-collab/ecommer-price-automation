package pricing

import (
	"errors"
	"fmt"
	"math"
)

const SystemMinimumMarginBPS int64 = 1

var (
	ErrMissingCost       = errors.New("cost required")
	ErrInvalidMargin     = errors.New("minimum margin must be between 1 and 9999 basis points")
	ErrInvalidPrice      = errors.New("selling price must be positive")
	ErrPriceGuardrail    = errors.New("requested price violates a configured guardrail")
	ErrFloorAboveCeiling = errors.New("minimum profitable price exceeds maximum price")
)

type CostComponents struct {
	SupplierKobo   int64 `json:"supplier_cost_kobo"`
	ShippingKobo   int64 `json:"shipping_cost_kobo"`
	PackagingKobo  int64 `json:"packaging_cost_kobo"`
	PaymentFeeKobo int64 `json:"payment_fee_kobo"`
	TaxImportKobo  int64 `json:"tax_import_cost_kobo"`
	OtherKobo      int64 `json:"other_allocated_cost_kobo"`
}

func (c CostComponents) Validate() error {
	values := []int64{c.SupplierKobo, c.ShippingKobo, c.PackagingKobo, c.PaymentFeeKobo, c.TaxImportKobo, c.OtherKobo}
	total := int64(0)
	for _, value := range values {
		if value < 0 {
			return errors.New("cost components cannot be negative")
		}
		if total > math.MaxInt64-value {
			return errors.New("landed cost is out of range")
		}
		total += value
	}
	if total == 0 {
		return ErrMissingCost
	}
	return nil
}

func (c CostComponents) LandedKobo() int64 {
	return c.SupplierKobo + c.ShippingKobo + c.PackagingKobo + c.PaymentFeeKobo + c.TaxImportKobo + c.OtherKobo
}

type RuleConditions struct {
	MinimumStock       *int   `json:"minimum_stock,omitempty"`
	MaximumCurrentKobo *int64 `json:"maximum_current_price_kobo,omitempty"`
	InStockOnly        bool   `json:"in_stock_only,omitempty"`
}

type Rule struct {
	ID                    string         `json:"id"`
	Name                  string         `json:"name"`
	Scope                 string         `json:"scope_type"`
	ProductID             string         `json:"product_id,omitempty"`
	VariantID             string         `json:"variant_id,omitempty"`
	CategoryID            string         `json:"category_id,omitempty"`
	MinimumMarginBPS      int64          `json:"minimum_margin_bps"`
	MaximumChangeBPS      int64          `json:"maximum_change_bps"`
	MaximumPriceKobo      *int64         `json:"maximum_price_kobo,omitempty"`
	RoundingIncrementKobo int64          `json:"rounding_increment_kobo"`
	Priority              int            `json:"priority"`
	Active                bool           `json:"active"`
	Conditions            RuleConditions `json:"conditions,omitempty"`
}

type Input struct {
	ProductID                string         `json:"product_id"`
	VariantID                string         `json:"variant_id,omitempty"`
	Cost                     CostComponents `json:"cost"`
	CurrentPriceKobo         int64          `json:"current_price_kobo"`
	CurrentSalePriceKobo     *int64         `json:"current_sale_price_kobo,omitempty"`
	SelectedRule             *Rule          `json:"selected_rule,omitempty"`
	ExplicitMinimumPriceKobo *int64         `json:"explicit_minimum_price_kobo,omitempty"`
	ExplicitMaximumPriceKobo *int64         `json:"explicit_maximum_price_kobo,omitempty"`
	RoundingIncrementKobo    int64          `json:"rounding_increment_kobo,omitempty"`
	ProductLocked            bool           `json:"product_locked"`
	LockReason               string         `json:"lock_reason,omitempty"`
}

type Result struct {
	Status                    string `json:"status"`
	Reason                    string `json:"reason,omitempty"`
	ProductID                 string `json:"product_id"`
	VariantID                 string `json:"variant_id,omitempty"`
	Currency                  string `json:"currency"`
	LandedCostKobo            int64  `json:"landed_cost_kobo"`
	CurrentPriceKobo          int64  `json:"current_price_kobo"`
	CurrentMarginBPS          int64  `json:"current_margin_bps"`
	MinimumMarginBPS          int64  `json:"minimum_margin_bps"`
	MinimumProfitableKobo     int64  `json:"minimum_profitable_price_kobo"`
	MaximumPriceKobo          *int64 `json:"maximum_price_kobo,omitempty"`
	GrossProfitKobo           int64  `json:"gross_profit_kobo"`
	PriceChangeRequiredBPS    int64  `json:"price_change_required_bps"`
	GuardrailMaximumChangeBPS int64  `json:"guardrail_maximum_change_bps"`
	RuleID                    string `json:"rule_id,omitempty"`
	RuleName                  string `json:"rule_name,omitempty"`
	ProductLocked             bool   `json:"product_locked"`
	LockReason                string `json:"lock_reason,omitempty"`
}

func Calculate(input Input) (Result, error) {
	if err := input.Cost.Validate(); err != nil {
		return Result{}, err
	}
	price := input.CurrentPriceKobo
	if input.CurrentSalePriceKobo != nil {
		price = *input.CurrentSalePriceKobo
	}
	if price <= 0 {
		return Result{}, ErrInvalidPrice
	}

	marginBPS := SystemMinimumMarginBPS
	maxChangeBPS := int64(10000)
	rounding := int64(100)
	ruleID, ruleName := "", ""
	if input.SelectedRule != nil {
		if input.SelectedRule.MinimumMarginBPS < 1 || input.SelectedRule.MinimumMarginBPS > 9999 {
			return Result{}, ErrInvalidMargin
		}
		if input.SelectedRule.MaximumChangeBPS < 1 || input.SelectedRule.MaximumChangeBPS > 10000 {
			return Result{}, fmt.Errorf("%w: maximum change", ErrPriceGuardrail)
		}
		marginBPS = input.SelectedRule.MinimumMarginBPS
		maxChangeBPS = input.SelectedRule.MaximumChangeBPS
		rounding = input.SelectedRule.RoundingIncrementKobo
		ruleID, ruleName = input.SelectedRule.ID, input.SelectedRule.Name
	}
	if input.RoundingIncrementKobo > 0 {
		rounding = input.RoundingIncrementKobo
	}
	if rounding <= 0 {
		rounding = 100
	}
	landed := input.Cost.LandedKobo()
	minimum, err := MinimumProfitablePrice(landed, marginBPS)
	if err != nil {
		return Result{}, err
	}
	minimum = RoundUp(minimum, rounding)
	if input.ExplicitMinimumPriceKobo != nil && *input.ExplicitMinimumPriceKobo > minimum {
		minimum = *input.ExplicitMinimumPriceKobo
	}
	maximum := input.ExplicitMaximumPriceKobo
	if input.SelectedRule != nil && input.SelectedRule.MaximumPriceKobo != nil && (maximum == nil || *input.SelectedRule.MaximumPriceKobo < *maximum) {
		maximum = input.SelectedRule.MaximumPriceKobo
	}
	if maximum != nil && *maximum < minimum {
		return Result{}, ErrFloorAboveCeiling
	}

	currentMargin := MarginBPS(price, landed)
	changeRequired := ChangeBPS(price, minimum)
	status := "compliant"
	reason := "current price meets the configured minimum margin"
	if price < minimum {
		status = "below_floor"
		reason = "current price is below the minimum profitable price"
	}
	if input.ProductLocked {
		status = "locked"
		reason = "product lock overrides all pricing rules"
	}
	if status == "below_floor" && changeRequired > maxChangeBPS {
		status = "guardrail_blocked"
		reason = fmt.Sprintf("price increase required exceeds %d bps guardrail", maxChangeBPS)
	}
	grossProfit := int64(0)
	if price > landed {
		grossProfit = price - landed
	}
	return Result{
		Status: status, Reason: reason, ProductID: input.ProductID, VariantID: input.VariantID, Currency: "NGN",
		LandedCostKobo: landed, CurrentPriceKobo: price, CurrentMarginBPS: currentMargin,
		MinimumMarginBPS: marginBPS, MinimumProfitableKobo: minimum, MaximumPriceKobo: maximum,
		GrossProfitKobo: grossProfit, PriceChangeRequiredBPS: changeRequired,
		GuardrailMaximumChangeBPS: maxChangeBPS, RuleID: ruleID, RuleName: ruleName,
		ProductLocked: input.ProductLocked, LockReason: input.LockReason,
	}, nil
}

func MinimumProfitablePrice(costKobo, marginBPS int64) (int64, error) {
	if costKobo <= 0 {
		return 0, ErrMissingCost
	}
	if marginBPS < 1 || marginBPS >= 10000 {
		return 0, ErrInvalidMargin
	}
	denominator := int64(10000) - marginBPS
	if costKobo > math.MaxInt64/10000 {
		return 0, errors.New("cost is out of range")
	}
	return ceilDiv(costKobo*10000, denominator), nil
}

func MarginBPS(priceKobo, costKobo int64) int64 {
	if priceKobo <= 0 || costKobo < 0 {
		return 0
	}
	if priceKobo > math.MaxInt64/10000 {
		return 0
	}
	return maxInt64(0, ((priceKobo-costKobo)*10000+priceKobo/2)/priceKobo)
}

func ChangeBPS(current, desired int64) int64 {
	if current <= 0 {
		return 0
	}
	diff := desired - current
	if diff < 0 {
		diff = -diff
	}
	return diff * 10000 / current
}

func RoundUp(value, increment int64) int64 {
	if increment <= 0 || value <= 0 {
		return value
	}
	if value > math.MaxInt64-(increment-1) {
		return value
	}
	return ((value + increment - 1) / increment) * increment
}

func ceilDiv(a, b int64) int64 { return (a + b - 1) / b }
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// SelectRule follows the product lock and explicit policy precedence first, then
// product, variant, category, store, and finally the system safety margin.
func SelectRule(rules []Rule, hasVariant bool) *Rule {
	bestRank := 8
	var best *Rule
	for i := range rules {
		rule := &rules[i]
		if !rule.Active {
			continue
		}
		rank := 0
		switch rule.Scope {
		case "product":
			rank = 3
		case "variant":
			rank = 4
		case "category":
			rank = 5
		case "store":
			rank = 6
		default:
			continue
		}
		if rule.Scope == "variant" && !hasVariant {
			continue
		}
		if rank < bestRank || (rank == bestRank && rule.Priority < best.Priority) {
			bestRank, best = rank, rule
		}
	}
	return best
}
