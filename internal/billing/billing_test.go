package billing

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"testing"
	"time"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// A forged webhook must be rejected outright. This is the single most important
// billing check: without it an attacker grants themselves a paid plan.
func TestWebhookSignatureMustMatch(t *testing.T) {
	secret := "sk_test_secret_key"
	body := []byte(`{"event":"charge.success","data":{"reference":"sub_1"}}`)

	if err := VerifyWebhookSignature(secret, body, sign(secret, body)); err != nil {
		t.Fatalf("a correctly signed webhook must be accepted: %v", err)
	}
	// Wrong key.
	if err := VerifyWebhookSignature("sk_test_wrong", body, sign(secret, body)); err == nil {
		t.Error("a signature from the wrong key must be rejected")
	}
	// Tampered body.
	tampered := []byte(`{"event":"charge.success","data":{"reference":"sub_999"}}`)
	if err := VerifyWebhookSignature(secret, tampered, sign(secret, body)); err == nil {
		t.Error("a tampered body must be rejected")
	}
	// Empty inputs.
	if err := VerifyWebhookSignature("", body, "abc"); err == nil {
		t.Error("empty secret must be rejected")
	}
	if err := VerifyWebhookSignature(secret, body, ""); err == nil {
		t.Error("empty signature must be rejected")
	}
	// Truncated signature.
	full := sign(secret, body)
	if err := VerifyWebhookSignature(secret, body, full[:len(full)-2]); err == nil {
		t.Error("a truncated signature must be rejected")
	}
}

func TestWebhookSignatureIsCaseInsensitiveOnHex(t *testing.T) {
	secret := "sk_test"
	body := []byte(`{"event":"x","data":{}}`)
	upper := sign(secret, body)
	if err := VerifyWebhookSignature(secret, body, upper); err != nil {
		t.Errorf("uppercase hex should verify: %v", err)
	}
}

func TestParseWebhookRejectsGarbage(t *testing.T) {
	if _, err := ParseWebhook([]byte("not json")); err == nil {
		t.Error("malformed JSON must be rejected")
	}
	if _, err := ParseWebhook([]byte(`{"data":{}}`)); err == nil {
		t.Error("an event with no name must be rejected")
	}
	event, err := ParseWebhook([]byte(`{"event":"charge.success","data":{"reference":"sub_abc"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Event != "charge.success" {
		t.Errorf("event name not parsed: %q", event.Event)
	}
	if event.ID.String() != "sub_abc" {
		t.Errorf("dedupe id should prefer the reference, got %q", event.ID.String())
	}
}

func TestSupportedEventsAreAWhitelist(t *testing.T) {
	// Events we do not act on must be acknowledged and ignored, so Paystack
	// stops retrying, rather than failing the delivery.
	for _, event := range []string{"charge.success", "subscription.create", "subscription.renew",
		"subscription.disable", "subscription.not_renew"} {
		if !SupportedEvents[event] {
			t.Errorf("%s should be supported", event)
		}
	}
	for _, event := range []string{"customer.created", "transfer.created", "", "charge.failed"} {
		if SupportedEvents[event] {
			t.Errorf("%s should not be treated as a state change", event)
		}
	}
}

func TestParseWebhookFallsBackToNumericID(t *testing.T) {
	event, err := ParseWebhook([]byte(`{"event":"subscription.create","data":{"id":98765}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.ID.String() != "98765" {
		t.Errorf("expected numeric id fallback, got %q", event.ID.String())
	}
}

// Read-only mode is the mechanism that stops a lapsed tenant publishing prices,
// while never deleting their data.
func TestEffectiveReadOnlyHonoursGracePeriod(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	graceEnd := now.Add(48 * time.Hour)

	// Inside the grace window: still writable.
	sub := Subscription{Status: "past_due", GraceEndsAt: &graceEnd}
	if ro, _ := EffectiveReadOnly(sub, now); ro {
		t.Error("a tenant inside its grace period must not be read-only")
	}
	// Grace expired: read-only.
	if ro, reason := EffectiveReadOnly(sub, now.Add(72*time.Hour)); !ro {
		t.Error("an expired grace period must make the tenant read-only")
	} else if reason == "" {
		t.Error("read-only must carry a reason")
	}
}

func TestPastDueWithoutGraceIsReadOnlyImmediately(t *testing.T) {
	// Failing open would let a lapsed tenant keep publishing, so this must fail closed.
	sub := Subscription{Status: "past_due"}
	if ro, _ := EffectiveReadOnly(sub, time.Now().UTC()); !ro {
		t.Error("past_due with no recorded grace must be read-only (fail closed)")
	}
}

func TestActiveAndTrialingAreNeverReadOnly(t *testing.T) {
	now := time.Now().UTC()
	for _, status := range []string{"active", "trialing", "cancelled"} {
		sub := Subscription{Status: status}
		if ro, _ := EffectiveReadOnly(sub, now); ro {
			t.Errorf("status %s must not be read-only", status)
		}
	}
}

func TestExplicitReadOnlyFlagWins(t *testing.T) {
	sub := Subscription{Status: "active", ReadOnly: true, ReadOnlyReason: "manual hold"}
	if ro, reason := EffectiveReadOnly(sub, time.Now().UTC()); !ro || reason != "manual hold" {
		t.Errorf("explicit read-only must win, got %v %q", ro, reason)
	}
}

func TestProviderRequiresSecretKey(t *testing.T) {
	if _, err := NewProvider("", false); err == nil {
		t.Error("a provider without a secret key must be rejected")
	}
	p, err := NewProvider("sk_test_123", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.BaseURL != "https://api.paystack.co" {
		t.Errorf("base URL must be the Paystack API, got %q", p.BaseURL)
	}
}

func TestPaymentReferenceIsNotGuessable(t *testing.T) {
	// References are tenant-scoped; they must not be sequential or guessable.
	seen := map[string]bool{}
	for i := 0; i < 25; i++ {
		ref := newReference("11111111-1111-1111-1111-111111111111")
		if seen[ref] {
			t.Fatalf("duplicate reference generated: %s", ref)
		}
		seen[ref] = true
		if len(ref) < 16 {
			t.Fatalf("reference is too short to be unguessable: %s", ref)
		}
	}
}

func TestEntitlementKeysAreClosedSet(t *testing.T) {
	// An unknown entitlement key must never be accepted; it would be a
	// silent no-op that looks like enforcement.
	for _, key := range []string{EntProducts, EntCompetitors, EntCheckFrequency,
		EntTeamMembers, EntAIRequests, EntExportRows, EntPublishes} {
		if !validEntitlements[key] {
			t.Errorf("%s should be a valid entitlement", key)
		}
	}
	for _, key := range []string{"", "unlimited", "everything", "products; DROP TABLE"} {
		if validEntitlements[key] {
			t.Errorf("%q must not be a valid entitlement", key)
		}
	}
}

func TestDecodeEntitlementsHandlesGarbage(t *testing.T) {
	// A malformed entitlements blob must degrade to an empty map rather than panic.
	if got := decodeEntitlements([]byte(`{bad`)); len(got) != 0 {
		t.Errorf("malformed JSON should decode to an empty map, got %v", got)
	}
	if got := decodeEntitlements(nil); len(got) != 0 {
		t.Errorf("nil should decode to an empty map, got %v", got)
	}
}
