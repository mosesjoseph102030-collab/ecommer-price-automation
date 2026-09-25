package pricing

import "testing"

func TestMinimumProfitablePrice(t *testing.T) {
	got, err := MinimumProfitablePrice(10900_00, 2500)
	if err != nil {
		t.Fatal(err)
	}
	if got != 14533_34 {
		t.Fatalf("got %d", got)
	}
}

func TestMarginAndRounding(t *testing.T) {
	if got := MarginBPS(14500_00, 10900_00); got != 2483 {
		t.Errorf("margin=%d", got)
	}
	if got := RoundUp(14533_34, 100); got != 14534_00 {
		t.Errorf("round=%d", got)
	}
}

func TestCalculateCompliant(t *testing.T) {
	result, err := Calculate(Input{ProductID: "p1", Cost: CostComponents{SupplierKobo: 10900_00}, CurrentPriceKobo: 15000_00, SelectedRule: &Rule{ID: "r1", Name: "25%", Scope: "store", MinimumMarginBPS: 2500, MaximumChangeBPS: 2000, RoundingIncrementKobo: 100, Active: true}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "compliant" || result.MinimumProfitableKobo != 14534_00 {
		t.Fatalf("unexpected %#v", result)
	}
}

func TestExplicitPolicyFloorCannotBeLoweredByRule(t *testing.T) {
	explicit := int64(16000)
	result, err := Calculate(Input{ProductID: "p1", Cost: CostComponents{SupplierKobo: 10000}, CurrentPriceKobo: 15000,
		SelectedRule:             &Rule{MinimumMarginBPS: 1000, MaximumChangeBPS: 5000, RoundingIncrementKobo: 100, Active: true},
		ExplicitMinimumPriceKobo: &explicit})
	if err != nil {
		t.Fatal(err)
	}
	if result.MinimumProfitableKobo != explicit {
		t.Fatalf("floor=%d", result.MinimumProfitableKobo)
	}
}

func TestProductLockOverridesRules(t *testing.T) {
	result, err := Calculate(Input{ProductID: "p1", Cost: CostComponents{SupplierKobo: 1000}, CurrentPriceKobo: 900, ProductLocked: true, LockReason: "Fragile item"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "locked" || result.LockReason != "Fragile item" {
		t.Fatalf("unexpected %#v", result)
	}
}

func TestGuardrailBlocksLargeIncrease(t *testing.T) {
	result, err := Calculate(Input{ProductID: "p1", Cost: CostComponents{SupplierKobo: 8000}, CurrentPriceKobo: 5000, SelectedRule: &Rule{MinimumMarginBPS: 2000, MaximumChangeBPS: 1000, RoundingIncrementKobo: 100, Active: true}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "guardrail_blocked" {
		t.Fatalf("unexpected %#v", result)
	}
}

func TestMissingAndInvalidCosts(t *testing.T) {
	if _, err := Calculate(Input{CurrentPriceKobo: 1000}); err != ErrMissingCost {
		t.Fatalf("got %v", err)
	}
	if _, err := Calculate(Input{Cost: CostComponents{SupplierKobo: -1}, CurrentPriceKobo: 1000}); err == nil {
		t.Fatal("negative cost accepted")
	}
	if _, err := MinimumProfitablePrice(100, 10000); err != ErrInvalidMargin {
		t.Fatalf("got %v", err)
	}
}

func TestRuleConditions(t *testing.T) {
	stock := 2
	product := ProductSnapshot{PriceKobo: 1000, StockStatus: "instock", StockQuantity: &stock}
	minimum := 3
	if conditionsMatch(RuleConditions{MinimumStock: &minimum}, product) {
		t.Fatal("minimum stock condition should fail")
	}
	maximum := int64(900)
	if conditionsMatch(RuleConditions{MaximumCurrentKobo: &maximum}, product) {
		t.Fatal("maximum current price condition should fail")
	}
	if !conditionsMatch(RuleConditions{InStockOnly: true}, product) {
		t.Fatal("in-stock condition should pass")
	}
}

func TestRulePrecedence(t *testing.T) {
	rules := []Rule{{ID: "store", Scope: "store", Active: true, Priority: 1}, {ID: "category", Scope: "category", Active: true, Priority: 1}, {ID: "variant", Scope: "variant", Active: true, Priority: 50}, {ID: "product", Scope: "product", Active: true, Priority: 99}}
	if got := SelectRule(rules, true); got == nil || got.ID != "product" {
		t.Fatalf("got %#v", got)
	}
}
