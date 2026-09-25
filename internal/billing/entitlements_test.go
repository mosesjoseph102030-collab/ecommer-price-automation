package billing

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// Read-only mode must fail closed. A lapsed tenant that can still publish is a
// worse failure than a lapsed tenant that cannot be served.
func TestGuardWriteFailsClosedOnPastDueWithoutGrace(t *testing.T) {
	// EffectiveReadOnly is what GuardWrite consults; this pins the rule that
	// past_due with no recorded grace is immediately read-only.
	sub := Subscription{Status: "past_due", GraceEndsAt: nil}
	readOnly, reason := EffectiveReadOnly(sub, time.Now().UTC())
	if !readOnly {
		t.Fatal("past_due with no grace window must be read-only")
	}
	if reason == "" {
		t.Error("a read-only refusal must explain itself")
	}
}

func TestReadOnlyReasonNamesTheCause(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	sub := Subscription{Status: "past_due", GraceEndsAt: &past}
	readOnly, reason := EffectiveReadOnly(sub, now)
	if !readOnly {
		t.Fatal("an expired grace period must be read-only")
	}
	// The reason is shown to the tenant, so it must be actionable and must not
	// blame them for something they can fix by paying.
	if reason == "" {
		t.Fatal("reason must be present")
	}
	for _, forbidden := range []string{"suspended", "terminated", "deleted"} {
		if contains(reason, forbidden) {
			t.Errorf("reason must not imply termination, found %q in %q", forbidden, reason)
		}
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// A tenant inside its grace window keeps full access. Making payment failure an
// instant hard stop would be a hostile product decision and is not what the spec
// asks for.
func TestGracePeriodPreservesFullAccess(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(3 * 24 * time.Hour)
	sub := Subscription{Status: "past_due", GraceEndsAt: &future}
	if readOnly, _ := EffectiveReadOnly(sub, now); readOnly {
		t.Error("a tenant inside its grace period must retain full access")
	}
	// ...and the boundary itself is inclusive: exactly at the end is read-only.
	if readOnly, _ := EffectiveReadOnly(sub, future); !readOnly {
		t.Error("the grace boundary itself must be read-only")
	}
}

// Cancellation must never make a tenant read-only. Read-only is a payment
// failure state, not a churn state.
func TestCancellationDoesNotImposeReadOnly(t *testing.T) {
	now := time.Now().UTC()
	sub := Subscription{Status: "cancelled", CancelAtPeriodEnd: true,
		CancelledAt: &now, RetentionEndsAt: timePointer(now.Add(90 * 24 * time.Hour))}
	if readOnly, _ := EffectiveReadOnly(sub, now); readOnly {
		t.Error("a cancelled tenant must not be made read-only")
	}
}

func timePointer(t time.Time) *time.Time { return &t }

// A trial that has not ended is fully entitled regardless of the grace columns
// being null.
func TestTrialIsFullyEntitled(t *testing.T) {
	now := time.Now().UTC()
	sub := Subscription{Status: "trialing", TrialEndsAt: timePointer(now.Add(13 * 24 * time.Hour))}
	if readOnly, _ := EffectiveReadOnly(sub, now); readOnly {
		t.Error("a trialing tenant must not be read-only")
	}
}

// An expired subscription row is not read-only: the store keeps its data and can
// be revived by paying.
func TestExpiredSubscriptionIsNotReadOnly(t *testing.T) {
	if readOnly, _ := EffectiveReadOnly(Subscription{Status: "expired"}, time.Now().UTC()); readOnly {
		t.Error("an expired tenant must keep read access so it can see and export its data")
	}
}

// FormatAmount is shown on invoices; a wrong value here is a billing defect a
// customer will notice immediately.
func TestFormatAmount(t *testing.T) {
	cases := map[int64]string{
		0:         "0.00",
		1:         "0.01",
		99:        "0.99",
		100:       "1.00",
		1500000:   "15000.00",
		5000000:   "50000.00",
		15000000:  "150000.00",
		123456789: "1234567.89",
	}
	for kobo, want := range cases {
		if got := FormatAmount(kobo); got != want {
			t.Errorf("FormatAmount(%d)=%q, want %q", kobo, got, want)
		}
	}
}

// The grace and retention defaults must be sane even when the operator has set
// no configuration. A zero grace period would make a payment failure an instant
// lockout.
func TestGraceAndRetentionHaveSafeDefaults(t *testing.T) {
	svc := &Service{}
	if svc.grace() <= 0 {
		t.Error("grace period must default to a positive duration")
	}
	if svc.retention() <= 0 {
		t.Error("retention period must default to a positive duration")
	}
	// Retention must outlive grace, or data would be deleted while a tenant is
	// still inside the window in which they are expected to pay.
	if svc.retention() <= svc.grace() {
		t.Error("retention must be longer than the grace period")
	}
	// A configured value must win over the default.
	custom := &Service{GracePeriod: 48 * time.Hour, RetentionPeriod: 365 * 24 * time.Hour}
	if custom.grace() != 48*time.Hour {
		t.Errorf("configured grace ignored: %s", custom.grace())
	}
	if custom.retention() != 365*24*time.Hour {
		t.Errorf("configured retention ignored: %s", custom.retention())
	}
}

// adminDB must never be nil: a webhook arriving with a nil admin pool would
// panic rather than fail, and a panic in a payment webhook is a real outage.
func TestAdminDBFallsBackToDB(t *testing.T) {
	primary := &sql.DB{}
	svc := &Service{DB: primary}
	if svc.adminDB() != primary {
		t.Error("adminDB must fall back to DB when Admin is nil")
	}
	admin := &sql.DB{}
	svc2 := &Service{DB: primary, Admin: admin}
	if svc2.adminDB() != admin {
		t.Error("adminDB must prefer Admin when set")
	}
}

func TestCancelRequiresAReason(t *testing.T) {
	// A cancellation without a recorded reason is unauditable, so it is
	// rejected before any database work happens. The service has a nil pool,
	// so reaching the query would panic: that the test passes proves the
	// validation runs first.
	svc := &Service{}
	if err := svc.Cancel(context.Background(), "org", ""); err == nil {
		t.Error("an empty reason must be rejected")
	}
	if err := svc.Cancel(context.Background(), "org", "  "); err == nil {
		t.Error("a whitespace-only reason must be rejected")
	}
}
