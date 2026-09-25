package recommendation

import (
	"testing"
	"time"

	"automation/internal/pricing"
)

func calcFor(current, cost, minMargin, maxChange int64, ceiling *int64) pricing.Result {
	result, err := pricing.Calculate(pricing.Input{ProductID: "p1", Cost: pricing.CostComponents{SupplierKobo: cost},
		CurrentPriceKobo: current, SelectedRule: &pricing.Rule{MinimumMarginBPS: minMargin, MaximumChangeBPS: maxChange, MaximumPriceKobo: ceiling, Active: true}, RoundingIncrementKobo: 100})
	if err != nil {
		panic(err)
	}
	return result
}

func baseInput(calc pricing.Result, current int64) EngineInput {
	return EngineInput{ProductID: "p1", ProductName: "Cable", ProductSKU: "CBL", CurrentPrice: current, StockStatus: "instock",
		Calc: calc, CompetitorDataFresh: true, Now: time.Now().UTC(), TTL: 30 * time.Minute}
}

func TestLockPausesEverything(t *testing.T) {
	calc := calcFor(100000, 80000, 2500, 5000, nil)
	in := baseInput(calc, 100000)
	in.Calc.ProductLocked = true
	in.Calc.LockReason = "campaign"
	rec := Engine(in)
	if rec.State != StatePause || rec.RecommendedPriceKobo != 100000 {
		t.Fatalf("lock must pause with no change, got %s -> %d", rec.State, rec.RecommendedPriceKobo)
	}
}

func TestBelowFloorRaisesToFloor(t *testing.T) {
	// cost 100000, min margin 25% -> floor 133333. From 125000 that's ~6666bps...
	// use a bigger max change so the raise is allowed.
	calc := calcFor(125000, 100000, 2500, 8000, nil)
	rec := Engine(baseInput(calc, 125000))
	if rec.State != StateRaise {
		t.Fatalf("expected raise, got %s (change %d bps vs guardrail %d)", rec.State, rec.ChangeBPS, calc.GuardrailMaximumChangeBPS)
	}
	if rec.RecommendedPriceKobo != calc.MinimumProfitableKobo {
		t.Fatalf("raise must target the floor, got %d want %d", rec.RecommendedPriceKobo, calc.MinimumProfitableKobo)
	}
}

func TestBelowFloorBlockedByGuardrailBecomesInvestigate(t *testing.T) {
	calc := calcFor(50000, 100000, 2500, 100, nil)
	rec := Engine(baseInput(calc, 50000))
	if rec.State != StateInvestigate {
		t.Fatalf("expected investigate when raise breaches guardrail, got %s", rec.State)
	}
}

func TestAboveCompetitorLowers(t *testing.T) {
	calc := calcFor(200000, 100000, 2500, 5000, nil)
	in := baseInput(calc, 200000)
	in.Competitors = []CompetitorSignal{{Name: "cheap", PriceKobo: 150000, ConfidenceBPS: 9000, ExtractionBPS: 9000, SourceURL: "https://x"}}
	rec := Engine(in)
	if rec.State != StateLower || rec.RecommendedPriceKobo != 150000 {
		t.Fatalf("expected lower to 150000, got %s -> %d", rec.State, rec.RecommendedPriceKobo)
	}
}

func TestNeverLowersBelowFloor(t *testing.T) {
	calc := calcFor(200000, 100000, 2500, 5000, nil)
	in := baseInput(calc, 200000)
	in.Competitors = []CompetitorSignal{{Name: "cheap", PriceKobo: 50000, ConfidenceBPS: 9000, ExtractionBPS: 9000, SourceURL: "https://x"}}
	rec := Engine(in)
	if rec.State != StateHold {
		t.Fatalf("must hold rather than undercut the floor, got %s", rec.State)
	}
	if rec.RecommendedPriceKobo != 200000 {
		t.Fatalf("hold must not change price")
	}
}

func TestStaleCompetitorDataInvestigates(t *testing.T) {
	calc := calcFor(200000, 100000, 2500, 5000, nil)
	in := baseInput(calc, 200000)
	in.CompetitorDataFresh = false
	in.Competitors = []CompetitorSignal{{Name: "cheap", PriceKobo: 100000, ConfidenceBPS: 9000, ExtractionBPS: 9000}}
	rec := Engine(in)
	if rec.State != StateInvestigate || rec.RecommendedPriceKobo != 200000 {
		t.Fatalf("stale competitor data must investigate without changing price, got %s -> %d", rec.State, rec.RecommendedPriceKobo)
	}
}

func TestLowConfidenceCompetitorIgnored(t *testing.T) {
	calc := calcFor(200000, 100000, 2500, 5000, nil)
	in := baseInput(calc, 200000)
	in.Competitors = []CompetitorSignal{{Name: "shaky", PriceKobo: 100000, ConfidenceBPS: 9000, ExtractionBPS: 2000}}
	rec := Engine(in)
	if rec.State != StateHold {
		t.Fatalf("low-extraction competitor must not drive a lower, got %s", rec.State)
	}
}
