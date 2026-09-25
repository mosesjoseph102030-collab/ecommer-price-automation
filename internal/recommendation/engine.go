package recommendation

import (
	"fmt"
	"time"

	"automation/internal/pricing"
)

// CompetitorSignal is a confirmed, in-stock, fresh competitor observation.
type CompetitorSignal struct {
	Name          string
	PriceKobo     int64
	ObservedAt    time.Time
	SourceURL     string
	ConfidenceBPS int
	ExtractionBPS int
}

// EngineInput is everything needed to decide one product's recommendation.
type EngineInput struct {
	ProductID     string
	ProductName   string
	ProductSKU    string
	VariantID     string
	CategoryIDs   []string
	CurrentPrice  int64
	StockStatus   string
	StockQuantity *int
	Calc          pricing.Result
	Competitors   []CompetitorSignal
	// CompetitorDataFresh reports whether every competitor signal is still fresh.
	CompetitorDataFresh bool
	Now                 time.Time
	// TTL is how long a recommendation stays valid before it must be recomputed.
	TTL time.Duration
}

// Engine produces a deterministic recommendation. It never invents numbers and
// never mutates WooCommerce. It only uses confirmed, in-stock, fresh competitor
// data and the deterministic pricing calculator.
func Engine(in EngineInput) Recommendation {
	if in.TTL <= 0 {
		in.TTL = 30 * time.Minute
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rec := Recommendation{
		GenerationKey:              generationKey(in),
		ProductID:                  in.ProductID,
		ProductName:                in.ProductName,
		ProductSKU:                 in.ProductSKU,
		VariantID:                  in.VariantID,
		CategoryIDs:                in.CategoryIDs,
		State:                      StateHold,
		PreviousPriceKobo:          in.CurrentPrice,
		RecommendedPriceKobo:       in.CurrentPrice,
		MinimumProfitablePriceKobo: in.Calc.MinimumProfitableKobo,
		MaximumPriceKobo:           in.Calc.MaximumPriceKobo,
		ChangeBPS:                  0,
		ConfidenceBPS:              10000,
		Urgency:                    "normal",
		MarginRisk:                 "none",
		Explanation:                "Current price meets the configured minimum margin.",
		RuleID:                     in.Calc.RuleID,
		RuleName:                   in.Calc.RuleName,
		StockStatus:                in.StockStatus,
		StockQuantity:              in.StockQuantity,
		Reasons:                    []Reason{},
		GeneratedAt:                now,
		ExpiresAt:                  now.Add(in.TTL),
	}
	appendReason(&rec, Reason{Type: "margin", Message: fmt.Sprintf("Current margin is %s; minimum profitable price is %s at %s target margin.", formatPercent(in.Calc.CurrentMarginBPS), formatMoney(in.Calc.MinimumProfitableKobo), formatPercent(in.Calc.MinimumMarginBPS)), Amount: &in.Calc.MinimumProfitableKobo, Source: "cost"})

	// A manual price lock always pauses; no rule, competitor, or owner action overrides it.
	if in.Calc.ProductLocked {
		rec.State = StatePause
		rec.ConfidenceBPS = 10000
		rec.Explanation = "Product price is locked; no automatic recommendation is made."
		appendReason(&rec, Reason{Type: "lock", Message: "Product price is locked" + optionalReason(in.Calc.LockReason) + ".", Source: "lock"})
		return rec
	}

	// Freshness guard: stale competitor data must be recomputed, never acted on.
	if !in.CompetitorDataFresh {
		rec.State = StateInvestigate
		rec.ConfidenceBPS = 4000
		rec.Explanation = "Competitor data is stale. Recompute before acting."
		appendReason(&rec, Reason{Type: "stale_competitor", Message: "One or more competitor observations are outside the freshness window; require recomputation.", Source: "competitor"})
		return rec
	}

	lowest, lowestSource, lowestCount := lowestConfirmedInStock(in.Competitors)
	rec.LowestConfirmedCompetitorKobo = lowest

	// Below floor: the only safe action is a raise to the floor. If the required
	// raise breaches the guardrail or the ceiling, escalate to investigate.
	if in.CurrentPrice < in.Calc.MinimumProfitableKobo {
		rec.State = StateRaise
		rec.RecommendedPriceKobo = in.Calc.MinimumProfitableKobo
		rec.MarginRisk = "critical"
		rec.Urgency = "high"
		rec.ConfidenceBPS = 9500
		rec.Explanation = fmt.Sprintf("Current price %s is below the minimum profitable price %s.", formatMoney(in.CurrentPrice), formatMoney(in.Calc.MinimumProfitableKobo))
		appendReason(&rec, Reason{Type: "cost", Message: fmt.Sprintf("Landed cost requires a minimum profitable price of %s.", formatMoney(in.Calc.MinimumProfitableKobo)), Amount: &in.Calc.MinimumProfitableKobo, Source: "cost"})
		change := pricing.ChangeBPS(in.CurrentPrice, rec.RecommendedPriceKobo)
		rec.ChangeBPS = change
		appendOpportunity(&rec, in.CurrentPrice, rec.RecommendedPriceKobo)
		if in.Calc.MaximumPriceKobo != nil && rec.RecommendedPriceKobo > *in.Calc.MaximumPriceKobo {
			escalateCeiling(&rec)
			return rec
		}
		if change > in.Calc.GuardrailMaximumChangeBPS {
			rec.State = StateInvestigate
			rec.ConfidenceBPS = 5000
			rec.Explanation = fmt.Sprintf("Required increase to %s exceeds the %s guardrail.", formatMoney(rec.RecommendedPriceKobo), formatPercent(in.Calc.GuardrailMaximumChangeBPS))
			appendReason(&rec, Reason{Type: "guardrail", Message: fmt.Sprintf("Required increase of %s exceeds the configured %s maximum change.", formatPercent(change), formatPercent(in.Calc.GuardrailMaximumChangeBPS)), Source: "rule"})
		}
		return rec
	}

	// Above the market: consider a lower to the lowest confirmed in-stock
	// competitor, but never below the floor.
	if lowest != nil && in.CurrentPrice > *lowest {
		target := *lowest
		if target < in.Calc.MinimumProfitableKobo {
			// Lowering to the competitor would breach the floor: hold, do not lose money.
			rec.State = StateHold
			rec.Explanation = fmt.Sprintf("Lowest confirmed in-stock competitor is %s, but matching it would fall below the minimum profitable price %s.", formatMoney(target), formatMoney(in.Calc.MinimumProfitableKobo))
			appendReason(&rec, Reason{Type: "competitor", Message: fmt.Sprintf("%d confirmed in-stock competitor(s); lowest is %s (%s), which is below the floor %s.", lowestCount, lowestSource.Name, formatMoney(target), formatMoney(in.Calc.MinimumProfitableKobo)), Amount: lowest, Source: "competitor", Evidence: lowestSource.SourceURL})
			return rec
		}
		if in.Calc.MaximumPriceKobo != nil && target > *in.Calc.MaximumPriceKobo {
			// The competitor is above our ceiling; do not follow.
			rec.State = StateHold
			rec.Explanation = "Lowest confirmed competitor is above the configured maximum price; holding."
			appendReason(&rec, Reason{Type: "competitor", Message: fmt.Sprintf("Lowest confirmed in-stock competitor is %s, above the maximum price %s.", formatMoney(target), formatMoney(*in.Calc.MaximumPriceKobo)), Amount: lowest, Source: "competitor", Evidence: lowestSource.SourceURL})
			return rec
		}
		change := pricing.ChangeBPS(in.CurrentPrice, target)
		if change > in.Calc.GuardrailMaximumChangeBPS {
			rec.State = StateInvestigate
			rec.ConfidenceBPS = 5000
			rec.Explanation = fmt.Sprintf("Lowering to the lowest confirmed competitor %s exceeds the %s guardrail.", formatMoney(target), formatPercent(in.Calc.GuardrailMaximumChangeBPS))
			appendReason(&rec, Reason{Type: "guardrail", Message: fmt.Sprintf("Decrease of %s to the lowest confirmed competitor exceeds the configured %s maximum change.", formatPercent(change), formatPercent(in.Calc.GuardrailMaximumChangeBPS)), Source: "rule"})
			return rec
		}
		rec.State = StateLower
		rec.RecommendedPriceKobo = target
		rec.ChangeBPS = change
		rec.ConfidenceBPS = clampBPS(lowestSource.ConfidenceBPS)
		rec.Explanation = fmt.Sprintf("Current price %s is above the lowest confirmed in-stock competitor %s.", formatMoney(in.CurrentPrice), formatMoney(target))
		appendReason(&rec, Reason{Type: "competitor", Message: fmt.Sprintf("%d confirmed in-stock competitor(s); lowest is %s (%s).", lowestCount, lowestSource.Name, formatMoney(target)), Amount: lowest, Source: "competitor", Evidence: lowestSource.SourceURL})
		return rec
	}

	// At or below the lowest competitor and above the floor: hold, but the
	// opportunity may be to raise toward the competitor if margin is thin.
	if lowest != nil && in.CurrentPrice < *lowest {
		headroom := *lowest - in.CurrentPrice
		appendOpportunity(&rec, in.CurrentPrice, in.CurrentPrice+headroom)
		if thinMargin(in.Calc.CurrentMarginBPS, in.Calc.MinimumMarginBPS) {
			rec.MarginRisk = "warning"
			rec.Urgency = "normal"
			appendReason(&rec, Reason{Type: "margin_risk", Message: fmt.Sprintf("Margin %s is close to the %s target and %s below the lowest confirmed competitor.", formatPercent(in.Calc.CurrentMarginBPS), formatPercent(in.Calc.MinimumMarginBPS), formatMoney(headroom)), Source: "cost"})
		}
		if in.StockQuantity != nil && *in.StockQuantity <= 5 {
			rec.Urgency = "high"
			appendReason(&rec, Reason{Type: "stock", Message: fmt.Sprintf("Only %d unit(s) left in stock; margin risk is elevated.", *in.StockQuantity), Source: "stock"})
		}
		return rec
	}

	// No competitor signal and price is at/above floor: pure hold.
	if in.StockQuantity != nil && *in.StockQuantity <= 5 {
		rec.Urgency = "high"
		appendReason(&rec, Reason{Type: "stock", Message: fmt.Sprintf("Only %d unit(s) left in stock.", *in.StockQuantity), Source: "stock"})
	}
	return rec
}

func generationKey(in EngineInput) string {
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d", in.ProductID, in.VariantID, in.CurrentPrice, in.Calc.MinimumProfitableKobo, in.MaxPriceOrZero(), lowestOrZero(in.Competitors))
}

func (in EngineInput) MaxPriceOrZero() int64 {
	if in.Calc.MaximumPriceKobo == nil {
		return 0
	}
	return *in.Calc.MaximumPriceKobo
}

func lowestConfirmedInStock(signals []CompetitorSignal) (*int64, CompetitorSignal, int) {
	var best *int64
	var bestSource CompetitorSignal
	count := 0
	for _, s := range signals {
		if s.PriceKobo <= 0 || s.ExtractionBPS < 5000 {
			continue
		}
		count++
		if best == nil || s.PriceKobo < *best {
			best = &s.PriceKobo
			bestSource = s
		}
	}
	return best, bestSource, count
}

func lowestOrZero(signals []CompetitorSignal) int64 {
	lowest, _, _ := lowestConfirmedInStock(signals)
	if lowest == nil {
		return 0
	}
	return *lowest
}

func escalateCeiling(rec *Recommendation) {
	rec.State = StateInvestigate
	rec.ConfidenceBPS = 4000
	rec.Explanation = "Required increase exceeds the configured maximum price; investigate before acting."
	appendReason(rec, Reason{Type: "ceiling", Message: fmt.Sprintf("Minimum profitable price %s exceeds the configured maximum price %s.", formatMoney(rec.MinimumProfitablePriceKobo), formatMoney(deref(rec.MaximumPriceKobo))), Source: "policy"})
}

func thinMargin(current, target int64) bool {
	return target > 0 && current < target+(target/2)
}

func appendReason(rec *Recommendation, r Reason) { rec.Reasons = append(rec.Reasons, r) }

func appendOpportunity(rec *Recommendation, from, to int64) {
	if to <= from {
		return
	}
	value := to - from
	rec.OpportunityKobo = &value
}

func optionalReason(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}

func clampBPS(v int) int {
	if v < 0 {
		return 0
	}
	if v > 10000 {
		return 10000
	}
	return v
}

func deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func formatPercent(bps int64) string {
	if bps <= 0 {
		return "0%"
	}
	whole := bps / 100
	frac := bps % 100
	if frac == 0 {
		return fmt.Sprintf("%d%%", whole)
	}
	return fmt.Sprintf("%d.%02d%%", whole, frac)
}

func formatMoney(k int64) string {
	neg := k < 0
	if neg {
		k = -k
	}
	whole := k / 100
	frac := k % 100
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("NGN %s%d.%02d", sign, whole, frac)
}
