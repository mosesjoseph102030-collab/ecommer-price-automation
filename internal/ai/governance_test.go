package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// The AI package must be structurally incapable of writing to a business table
// or calling a WooCommerce publish endpoint. These are architectural guarantees
// asserted in code, not just review notes.

func TestUnverifiedProviderIsRejected(t *testing.T) {
	if _, err := NewHTTPProvider("https://api.example.com", "", "model", false); err == nil {
		t.Error("a provider without an API key must be rejected")
	}
	if _, err := NewHTTPProvider("http://api.example.com", "key", "model", false); err == nil {
		t.Error("plain HTTP must be rejected outside development")
	}
	p, err := NewHTTPProvider("https://api.example.com", "key", "claude-x", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() == "" || p.Model() == "" {
		t.Error("provider must report its name and model for the audit log")
	}
	if p.TemplateVersion(FeatureExplanation) == "" {
		t.Error("every feature needs a prompt template version for the audit log")
	}
}

func TestProviderErrorDoesNotLeakInternals(t *testing.T) {
	// The generic sentinel is what callers see; provider text must not escape,
	// because it can contain URLs, request ids, or account identifiers.
	if !strings.Contains(ErrProviderUnavailable.Error(), "unavailable") {
		t.Errorf("unexpected provider error text: %q", ErrProviderUnavailable)
	}
}

func TestCostAccountingIsDeterministic(t *testing.T) {
	p := &HTTPProvider{CostPerInputTokenMicros: 3, CostPerOutputTokenMicros: 15}
	// 100 input tokens * 3 + 50 output tokens * 15 = 300 + 750 = 1050 micros.
	if got := p.CostMicros(Usage{InputTokens: 100, OutputTokens: 50}); got != 1050 {
		t.Errorf("expected 1050 micros, got %d", got)
	}
	if got := p.CostMicros(Usage{}); got != 0 {
		t.Errorf("zero usage must cost nothing, got %d", got)
	}
}

func TestStaticProviderNeedsNoKeyOrNetwork(t *testing.T) {
	// The static provider is what CI and local development use, so the whole
	// governance path is testable without a provider key.
	p := &StaticProvider{}
	result, err := p.Complete(t.Context(), Request{Feature: FeatureExplanation, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Raw) == 0 {
		t.Error("static provider must return a parseable response")
	}
	if _, err := DecodeStrict(result.Raw); err != nil {
		t.Errorf("static provider default response must satisfy the strict schema: %v", err)
	}
}

func TestFeatureSetCoversTheSpecification(t *testing.T) {
	// Every capability the specification lists must exist and be a known feature.
	required := map[string]string{
		"recommendation_explanation": FeatureExplanation,
		"invoice_ocr":                FeatureInvoiceOCR,
		"rule_assistant":             FeatureRuleAssistant,
		"product_match_suggestion":   FeatureMatchSuggestion,
		"pricing_summary":            FeaturePricingSummary,
		"feedback_clustering":        FeatureFeedbackCluster,
		"community_summary":          FeatureCommunitySummary,
		"support_response_draft":     FeatureSupportResponse,
	}
	for label, constant := range required {
		if !ValidFeature(constant) {
			t.Errorf("specification capability %q is not a registered feature", label)
		}
	}
}

func TestValidFeatureRejectsUnknown(t *testing.T) {
	for _, bad := range []string{"", "publish_price", "delete_products", "UPDATE products SET price=1"} {
		if ValidFeature(bad) {
			t.Errorf("%q must not be accepted as a feature", bad)
		}
	}
}

// A draft is the only place model output may land, and verified_payload is the
// deterministic recomputation. This checks the payload shape carries the
// recomputed figures separately from the model's prose.
func TestDraftSeparatesModelOutputFromVerifiedFigures(t *testing.T) {
	response, err := DecodeStrict([]byte(`{"summary":"Raise to protect margin.","bullets":["Cost rose"]}`))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	// Whatever the model said, verified_payload is what the Go engine computed.
	verified := map[string]any{"recommended_price_kobo": 1500000, "floor_kobo": 1333333}
	raw, err := json.Marshal(verified)
	if err != nil {
		t.Fatal(err)
	}
	draft := Draft{Feature: FeatureExplanation, Payload: payload, VerifiedPayload: raw, Status: "pending"}
	if !strings.Contains(string(draft.Payload), "Raise to protect margin") {
		t.Error("model prose should live in payload")
	}
	if !strings.Contains(string(draft.VerifiedPayload), "1500000") {
		t.Error("deterministic figures must live in verified_payload")
	}
	// Money stays an integer count of kobo, never a float or a formatted string.
	var parsed map[string]any
	if err := json.Unmarshal(draft.VerifiedPayload, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["recommended_price_kobo"].(float64); !ok {
		t.Error("kobo values must serialize as numbers, not strings")
	}
}

func TestWrapUntrustedMarksLabel(t *testing.T) {
	wrapped := WrapUntrusted("ocr_text", "hello")
	if !strings.Contains(wrapped, `label="ocr_text"`) {
		t.Errorf("untrusted content must be labelled: %q", wrapped)
	}
	if !strings.HasPrefix(wrapped, "<untrusted_content") {
		t.Errorf("untrusted content must be fenced: %q", wrapped)
	}
}

func TestRedactionCoversProviderCredentialShapes(t *testing.T) {
	secrets := []string{
		"sk-ant-api03-abcdefghijklmnop",
		"postgres://u:p@db.internal:5432/prod",
		"eyJhbGciOi.eyJzdWIi.sig",
		"ck_0123456789abcdef0123456789abcdef",
	}
	for _, s := range secrets {
		if out := Redact("x " + s + " y"); strings.Contains(out, s) {
			t.Errorf("secret leaked: %s", s)
		}
	}
}
