package recommendation

import "testing"

// The publisher must never silently overwrite a manual change and must never
// clobber an active sale price.
func TestClassifyLivePublishesOnlyWhenUnchanged(t *testing.T) {
	if got := classifyLive(false, 10000, 10000, 12000); got != actionPublish {
		t.Fatalf("unchanged price should publish, got %s", got)
	}
}

func TestClassifyLiveTreatsRequestedAsAlreadyApplied(t *testing.T) {
	// Lost response: the write landed but we never saw the reply.
	if got := classifyLive(false, 12000, 10000, 12000); got != actionVerify {
		t.Fatalf("price already at target must be treated as applied, got %s", got)
	}
}

func TestClassifyLiveDetectsManualChange(t *testing.T) {
	if got := classifyLive(false, 13000, 10000, 12000); got != actionConflict {
		t.Fatalf("external manual change must conflict, got %s", got)
	}
}

func TestClassifyLiveGuardsSalePrice(t *testing.T) {
	// Even when the regular price is unchanged, an active sale price must stop
	// the publish so the customer-visible price is not changed unexpectedly.
	if got := classifyLive(true, 10000, 10000, 12000); got != actionSaleGuard {
		t.Fatalf("sale price must block publish, got %s", got)
	}
}

func TestChangeBetweenIsAbsoluteBPS(t *testing.T) {
	if got := changeBetween(10000, 9000); got != 1000 {
		t.Fatalf("expected 1000 bps, got %d", got)
	}
	if got := changeBetween(10000, 12000); got != 2000 {
		t.Fatalf("expected 2000 bps, got %d", got)
	}
}
