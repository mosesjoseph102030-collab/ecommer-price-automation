package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A browser enforces CORS by refusing the request, not by reporting an error the
// page can show. A verb missing from Access-Control-Allow-Methods therefore looks
// like "the button does nothing" in the dashboard while working perfectly from
// curl. These tests pin the verb list against the verbs the API actually routes.
func TestCORSAllowsEveryMethodTheAPIServes(t *testing.T) {
	handler := corsMiddleware("http://localhost:3001", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions,
	} {
		req := httptest.NewRequest(method, "/api/v1/app/store/pricing-rules", nil)
		req.Header.Set("Origin", "http://localhost:3001")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		allowed := rec.Header().Get("Access-Control-Allow-Methods")
		if !containsMethod(allowed, method) {
			t.Errorf("%s is not in Access-Control-Allow-Methods %q; a browser would block it", method, allowed)
		}
	}
}

// The concrete regressions this list caused: the Phase 5 kill switch and Phase 6
// notification preferences are PUT, and rule deletion is DELETE.
func TestCORSIncludesTheVerbsTheDashboardActuallyUses(t *testing.T) {
	handler := corsMiddleware("http://localhost:3001", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/app/store/pricing-kill-switch", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	req.Header.Set("Access-Control-Request-Method", http.MethodPut)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status=%d, want 204", rec.Code)
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		if !containsMethod(rec.Header().Get("Access-Control-Allow-Methods"), method) {
			t.Errorf("%s missing from the preflight response", method)
		}
	}
}

func containsMethod(list, method string) bool {
	for _, part := range splitAndTrim(list) {
		if part == method {
			return true
		}
	}
	return false
}

func splitAndTrim(list string) []string {
	out := []string{}
	current := ""
	for _, r := range list {
		if r == ',' {
			out = append(out, trimSpace(current))
			current = ""
			continue
		}
		current += string(r)
	}
	if trimSpace(current) != "" {
		out = append(out, trimSpace(current))
	}
	return out
}

func trimSpace(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t') {
		end--
	}
	return value[start:end]
}

// Credentials must be allowed, because the dashboard authenticates with the
// fintrade_session cookie rather than a bearer token.
func TestCORSAllowsCredentialsForTheDashboard(t *testing.T) {
	handler := corsMiddleware("http://localhost:3001", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/app/store/billing", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3001" {
		t.Errorf("allow-origin=%q, want the requesting origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("allow-credentials=%q, want true (the dashboard uses the session cookie)", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary=%q, want Origin so a shared cache cannot serve one origin's response to another", got)
	}
}

// An origin that is not allow-listed must never be reflected back, even when
// only one origin is configured. A single `Access-Control-Allow-Origin` value
// would otherwise be paired with a readable response.
func TestCORSDoesNotReflectAnUnknownOrigin(t *testing.T) {
	handler := corsMiddleware("http://localhost:3001", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/app/store/billing", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin == "https://evil.example" {
		t.Error("an unknown origin was reflected back; the response would be readable cross-origin")
	}
	// With exactly one configured origin the server may still name it, but only
	// when the request did not come from somewhere else. Name the check clearly.
	if origin != "" && origin != "http://localhost:3001" {
		t.Errorf("allow-origin=%q, want either empty or the configured origin", origin)
	}
}

// Multiple configured origins must each be echoed, which is why the allowed set
// is a set and not a single string.
func TestCORSHandlesMultipleConfiguredOrigins(t *testing.T) {
	handler := corsMiddleware("http://localhost:3000, http://localhost:3001,http://localhost:3002",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, origin := range []string{"http://localhost:3000", "http://localhost:3001", "http://localhost:3002"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/app/store/billing", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("allow-origin=%q for request origin %q", got, origin)
		}
	}
}
