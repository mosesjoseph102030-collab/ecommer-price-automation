package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"automation/internal/billing"
)

// tenantSlugFromPath is the basis of the read-only guard, so its shape matters:
// a misparse either blocks a route that should be allowed or misses one that
// should be blocked.
func TestTenantSlugFromPath(t *testing.T) {
	cases := []struct {
		path string
		slug string
		ok   bool
	}{
		{"/api/v1/app/my-store/products", "my-store", true},
		{"/api/v1/app/my-store", "my-store", true},
		{"/api/v1/app/my-store/billing/checkout", "my-store", true},
		{"api/v1/app/my-store/products", "my-store", true},
		{"/api/v1/app/", "", false},
		{"/api/v1/app", "", false},
		{"/api/v1/admin/stores", "", false},
		{"/api/v1/community/rooms", "", false},
		{"/api/v1/plans", "", false},
		{"/health", "", false},
		{"/", "", false},
		{"/api/v2/app/store-x/products", "", false},
		{"/api/v1/organizations/store-x", "", false},
	}
	for _, tc := range cases {
		slug, ok := tenantSlugFromPath(tc.path)
		if ok != tc.ok || slug != tc.slug {
			t.Errorf("%q -> (%q, %v), want (%q, %v)", tc.path, slug, ok, tc.slug, tc.ok)
		}
	}
}

// A read-only store must still be able to reach the endpoints that let it fix
// the situation that made it read-only. Blocking these would turn a billing
// problem into a permanent lockout.
func TestReadOnlyExemptRoutesCoverRecoveryAndSafety(t *testing.T) {
	required := []string{
		"/api/v1/app/store/billing",
		"/api/v1/app/store/billing/checkout",
		"/api/v1/app/store/billing/confirm",
		"/api/v1/app/store/pricing-kill-switch",
		"/api/v1/app/store/ai/kill-switch",
	}
	for _, path := range required {
		exempt := false
		for _, suffix := range readOnlyExemptSuffixes {
			if len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix {
				exempt = true
				break
			}
		}
		if !exempt {
			t.Errorf("%s must stay reachable while read-only", path)
		}
	}
}

func TestReadOnlyExemptSuffixesDoNotLeakToOtherRoutes(t *testing.T) {
	// A suffix match must not accidentally exempt a route that merely ends in
	// the same characters, e.g. a nested product resource.
	blocked := []string{
		"/api/v1/app/store/pricing-rules",
		"/api/v1/app/store/products",
		"/api/v1/app/store/competitors",
		"/api/v1/app/store/ai/generate",
		"/api/v1/app/store/team/invite",
		"/api/v1/app/store/integrations/woocommerce/sync",
	}
	for _, path := range blocked {
		for _, suffix := range readOnlyExemptSuffixes {
			if len(path) >= len(suffix) && path[len(path)-len(suffix):] == suffix {
				t.Errorf("%s must NOT be exempt from the read-only guard", path)
			}
		}
	}
}

// With no billing service the guard must be a pass-through, because a server
// that has billing switched off must not be silently rejecting all writes.
func TestGuardBillingWriteIsPassthroughWithoutBilling(t *testing.T) {
	called := false
	handler := GuardBillingWrite(nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/app/store/pricing-rules", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !called {
		t.Error("the wrapped handler must still run when billing is absent")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", rec.Code)
	}
}

// GET must never be blocked, whatever the tenant's billing state: a read-only
// store keeps its visibility and its exports.
func TestGuardBillingWritePassesThroughReads(t *testing.T) {
	called := false
	// A non-nil service with a nil database is enough to exercise the
	// read-only decision: GET short-circuits before any lookup.
	svc := &billing.Service{}
	handler := GuardBillingWrite(svc, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		called = false
		req := httptest.NewRequest(method, "/api/v1/app/store/products", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if !called {
			t.Errorf("%s must reach the handler", method)
		}
	}
}

func TestMutatingMethodsCoverEveryWriteVerb(t *testing.T) {
	// If a verb is missing, writes through it would bypass read-only mode.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if !mutatingMethods[method] {
			t.Errorf("%s must be treated as mutating", method)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if mutatingMethods[method] {
			t.Errorf("%s must not be treated as mutating", method)
		}
	}
}

// No billing handler may call GuardWrite. A tenant becomes read-only *because*
// payment failed, so guarding the payment endpoints would make the state
// permanent: the tenant could never pay, and could never recover.
//
// This is a source-level assertion on purpose. A behavioural test would need a
// database, and the failure it guards against is a single stray line in a
// handler that looks entirely reasonable on its own.
func TestNoBillingHandlerGuardsAgainstItsOwnReadOnly(t *testing.T) {
	source, err := os.ReadFile("billing.go")
	if err != nil {
		t.Skipf("source not readable in this build layout: %v", err)
	}
	body := string(source)
	// GuardBillingWrite is the middleware that is *supposed* to call GuardWrite:
	// it is the single decision point. Exclude it so this test is about the
	// handlers, not about the guard itself.
	guard := strings.Index(body, "func GuardBillingWrite(")
	if guard < 0 {
		t.Fatal("GuardBillingWrite not found; this test needs updating if the guard was renamed")
	}
	handlers := body[:guard]
	if strings.Contains(handlers, "GuardWrite(") {
		t.Error("no billing handler may call GuardWrite: the /billing routes are exempt from " +
			"read-only by design so a lapsed tenant can still pay and recover. " +
			"Only GuardBillingWrite, the middleware, may call it.")
	}
	// The guard itself must still call it, or the middleware is inert.
	if !strings.Contains(body[guard:], "GuardWrite(") {
		t.Error("GuardBillingWrite must call svc.GuardWrite to have any effect")
	}
}
