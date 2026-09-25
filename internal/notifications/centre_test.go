package notifications

import "testing"

// The specification requires that critical alerts cannot be silently muted
// without explicit policy. These tests pin that rule at the pure-decision level
// so it cannot be weakened by a future refactor.

func TestCriticalAlertTypesAreRegistered(t *testing.T) {
	required := []string{"price_change.conflict", "price_change.failed", "announcement.critical", "security.credential_expired"}
	for _, alertType := range required {
		if !CriticalAlertTypes[alertType] {
			t.Errorf("%q must be treated as a critical alert type", alertType)
		}
	}
}

func TestNonCriticalAlertTypesAreNotProtected(t *testing.T) {
	for _, alertType := range []string{"competitor.price_drop", "price_change.approved", "cost.updated", ""} {
		if CriticalAlertTypes[alertType] {
			t.Errorf("%q should not be a critical alert type", alertType)
		}
	}
}

// dedupe is used so a critical alert is not enqueued twice on the in-app channel.
func TestDedupeRemovesDuplicateChannels(t *testing.T) {
	got := dedupe([]string{ChannelInApp, ChannelEmail, ChannelInApp, ChannelWhatsApp, ChannelEmail})
	want := []string{ChannelInApp, ChannelEmail, ChannelWhatsApp}
	if len(got) != len(want) {
		t.Fatalf("dedupe length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedupe[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTruncateBoundsErrorText(t *testing.T) {
	long := ""
	for i := 0; i < 1000; i++ {
		long += "x"
	}
	if got := truncate(long, 300); len(got) != 300 {
		t.Errorf("expected 300 chars, got %d", len(got))
	}
	if got := truncate("short", 300); got != "short" {
		t.Errorf("short text must be unchanged, got %q", got)
	}
}
