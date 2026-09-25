package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"automation/internal/ai"
)

func writeAIError(w http.ResponseWriter, err error) {
	rid := ""
	message := err.Error()
	switch {
	case errors.Is(err, ai.ErrKillSwitch):
		writeErr(w, 409, "AI_KILL_SWITCH", "AI is disabled by the kill switch.", rid)
	case errors.Is(err, ai.ErrFeatureDisabled):
		writeErr(w, 403, "AI_FEATURE_DISABLED", "This AI feature is not enabled for your store.", rid)
	case errors.Is(err, ai.ErrQuotaExceeded):
		writeErr(w, 429, "AI_QUOTA_EXCEEDED", "AI daily quota exhausted. Try again tomorrow.", rid)
	case errors.Is(err, ai.ErrPlanLimit):
		// 402: this is a commercial limit, not a safety one. The fix is an
		// upgrade, and the message should say so rather than "try later".
		writeErr(w, 402, "PLAN_LIMIT_EXCEEDED", "Your plan's monthly AI allowance is used up. Upgrade to continue.", rid)
	case errors.Is(err, ai.ErrRateLimited):
		writeErr(w, 429, "AI_RATE_LIMITED", "Too many AI requests. Slow down.", rid)
	case errors.Is(err, ai.ErrUntrustedInput):
		writeErr(w, 422, "AI_UNTRUSTED_INPUT", "The supplied document failed the safety screen and was not processed.", rid)
	case errors.Is(err, ai.ErrSchema):
		writeErr(w, 502, "AI_SCHEMA_REJECTED", "The model response did not match the required schema, so it was discarded.", rid)
	case errors.Is(err, ai.ErrNotConfigured):
		writeErr(w, 503, "AI_NOT_CONFIGURED", "AI assistance is not configured on this server.", rid)
	case errors.Is(err, ai.ErrProviderUnavailable):
		writeErr(w, 502, "AI_PROVIDER_UNAVAILABLE", "The model provider is unavailable. Try again shortly.", rid)
	default:
		writeErr(w, 400, "VALIDATION_ERROR", message, rid)
	}
}

type aiRequestBody struct {
	Feature    string         `json:"feature"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	RecordIDs  []string       `json:"record_ids"`
	Facts      map[string]any `json:"facts"`
	Intention  string         `json:"intention"`
	Untrusted  *struct {
		Label string `json:"label"`
		Text  string `json:"text"`
	} `json:"untrusted"`
}

// GenerateAI runs one governed AI call. The response is always an inert draft.
func GenerateAI(service *ai.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body aiRequestBody
		if !decodeJSON(w, r, 128<<10, &body) {
			return
		}
		if !ai.ValidFeature(body.Feature) {
			writeErr(w, 400, "VALIDATION_ERROR", "Unknown AI feature.", RequestIDFromContext(r.Context()))
			return
		}
		facts := ai.FormatFacts(body.Facts)
		var req ai.Request
		switch body.Feature {
		case ai.FeatureExplanation:
			req = ai.BuildExplanation(facts)
		case ai.FeaturePricingSummary:
			req = ai.BuildSummary(facts)
		case ai.FeatureRuleAssistant:
			req = ai.BuildRuleAssistant(facts, body.Intention)
		case ai.FeatureInvoiceOCR:
			req = ai.BuildInvoice(facts)
		case ai.FeatureMatchSuggestion:
			req = ai.BuildMatch(facts)
		case ai.FeatureFeedbackCluster:
			req = ai.BuildCluster(facts)
		case ai.FeatureCommunitySummary:
			req = ai.BuildRoomSummary(facts)
		case ai.FeatureSupportResponse:
			req = ai.BuildSupportReply(facts)
		}
		var untrusted *ai.Untrusted
		if body.Untrusted != nil {
			untrusted = &ai.Untrusted{Label: body.Untrusted.Label, Text: body.Untrusted.Text}
		}
		draft, err := service.Generate(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()),
			body.Feature, body.TargetType, body.TargetID, body.RecordIDs, req, untrusted)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{
			"draft":  draft,
			"notice": "This is an AI draft. Nothing has been changed. A human must approve it before anything is applied.",
		})
	}
}

func ListAIDrafts(service *ai.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListDrafts(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("status"), 0)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"drafts": values})
	}
}

func DecideAIDraft(service *ai.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Decision string `json:"decision"`
			Note     string `json:"note"`
		}
		if !decodeJSON(w, r, 8<<10, &input) {
			return
		}
		err := service.Decide(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"), input.Decision, input.Note)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok":     true,
			"notice": "Approval recorded. The draft is still inert until deterministic Go code applies it.",
		})
	}
}

func GetAIQuota(service *ai.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Quota(r.Context(), OrgIDFromContext(r.Context()))
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}
}

func SetAIKillSwitch(service *ai.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled bool   `json:"enabled"`
			Reason  string `json:"reason"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		err := service.SetKillSwitch(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), "tenant", input.Enabled, input.Reason)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func SetPlatformAIKillSwitch(service *ai.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled bool   `json:"enabled"`
			Reason  string `json:"reason"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		err := service.SetKillSwitch(r.Context(), "", UserIDFromContext(r.Context()), "platform", input.Enabled, input.Reason)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func SetAIFeatureFlag(service *ai.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Feature string `json:"feature"`
			Enabled bool   `json:"enabled"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		err := service.SetFeatureFlag(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input.Feature, input.Enabled)
		if err != nil {
			writeAIError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

var _ = json.Marshal
