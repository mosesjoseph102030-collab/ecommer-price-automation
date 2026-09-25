package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"automation/internal/db"
	"automation/internal/ratelimit"
)

// MetricAIRequests is the usage metric an AI call consumes. It must match the
// metric vocabulary in the billing schema, which is why billing_test asserts
// the two agree rather than leaving it to a comment.
const MetricAIRequests = "ai_requests"

// Meter enforces and records billable usage against a tenant's plan.
// billing.Service satisfies it. It is declared as an interface here so the AI
// package does not take a dependency on billing; a nil Meter means usage is
// unmetered, which is the correct behaviour for a server with billing disabled.
type Meter interface {
	// CheckLimit returns an error when requested units would exceed the plan
	// entitlement for limitKey.
	CheckLimit(ctx context.Context, orgID, limitKey string, requested int64) error
	// IncrementUsage records consumed units and returns the new period total.
	IncrementUsage(ctx context.Context, orgID, metric string, delta int64) (int64, error)
}

// Service is the only entry point to model output. Every request passes the
// kill switch, the feature flag, the quota, and the rate limiter in that order,
// and every attempt is logged to ai_invocations including refusals.
type Service struct {
	DB       *sql.DB
	Admin    *sql.DB
	Provider Provider
	Limiter  *ratelimit.Limiter
	Logger   *slog.Logger
	// Meter enforces the tenant's plan limit for AI requests. Nil disables
	// metering, so a server without billing configured is never blocked.
	Meter Meter
	// RequestsPerMinute bounds per-tenant request rate independently of quota.
	RequestsPerMinute int
}

func (s *Service) requestLimit() int {
	if s.RequestsPerMinute > 0 {
		return s.RequestsPerMinute
	}
	return 10
}

// Control is the effective governance state for a tenant.
type Control struct {
	KillSwitchEnabled bool
	KillSwitchScope   string
	KillSwitchReason  string
	FeatureEnabled    bool
	DailyRequestLimit int
	DailyTokenLimit   int64
	MaxInputBytes     int
}

// LoadControl resolves the effective control state. A platform kill switch wins
// over everything; otherwise a per-feature flag must be explicitly enabled.
func (s *Service) LoadControl(ctx context.Context, orgID, feature string) (Control, error) {
	if !ValidFeature(feature) {
		return Control{}, fmt.Errorf("unknown feature %q", feature)
	}
	out := Control{DailyRequestLimit: 50, DailyTokenLimit: 200000, MaxInputBytes: 60000}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		// A tenant can never write a platform-scope kill switch (the write policy
		// requires organization_id = current tenant), so a null scope here can
		// only have come from the admin role.
		err := tx.QueryRowContext(ctx, `SELECT scope, enabled, reason FROM ai_kill_switches
			WHERE scope='platform' OR (scope='tenant' AND organization_id=$1)
			ORDER BY (scope='platform') DESC LIMIT 1`, orgID).Scan(&out.KillSwitchScope, &out.KillSwitchEnabled, &out.KillSwitchReason)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// A tenant flag or a platform-wide flag enables the feature; the most
		// specific enabled flag wins.
		err = tx.QueryRowContext(ctx, `SELECT enabled FROM ai_feature_flags
			WHERE feature=$1 AND (organization_id=$2 OR organization_id IS NULL)
			ORDER BY (organization_id = $2) DESC LIMIT 1`, feature, orgID).Scan(&out.FeatureEnabled)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT daily_request_limit, daily_token_limit, max_input_bytes
			FROM ai_tenant_quotas WHERE organization_id=$1`, orgID).
			Scan(&out.DailyRequestLimit, &out.DailyTokenLimit, &out.MaxInputBytes)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	return out, err
}

// Generate runs one governed AI call and stores the result as an inert draft.
//
// It never writes to a business table. Callers receive a draft ID; a separate
// human action is required before anything is applied.
func (s *Service) Generate(ctx context.Context, orgID, actor, feature, targetType, targetID string, recordIDs []string, req Request, untrusted *Untrusted) (Draft, error) {
	if !ValidFeature(feature) {
		return Draft{}, fmt.Errorf("unknown feature %q", feature)
	}
	if s.Provider == nil {
		return Draft{}, ErrNotConfigured
	}
	control, err := s.LoadControl(ctx, orgID, feature)
	if err != nil {
		return Draft{}, err
	}

	// Governance gates, cheapest and most absolute first.
	if control.KillSwitchEnabled {
		s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeBlockedKillSwitch, ErrorCode: "kill_switch"})
		return Draft{}, ErrKillSwitch
	}
	if !control.FeatureEnabled {
		s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeBlockedFlag, ErrorCode: "feature_disabled"})
		return Draft{}, ErrFeatureDisabled
	}
	// Untrusted input is screened before it can influence a model at all.
	if untrusted != nil {
		if err := ScreenUntrusted(untrusted.Text); err != nil {
			s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeBlockedUntrusted, ErrorCode: "prompt_injection"})
			return Draft{}, err
		}
	}
	used, tokensUsed, err := s.Usage(ctx, orgID, feature, time.Now().UTC())
	if err != nil {
		return Draft{}, err
	}
	if used >= control.DailyRequestLimit {
		s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeBlockedQuota, ErrorCode: "daily_requests"})
		return Draft{}, ErrQuotaExceeded
	}
	if tokensUsed >= control.DailyTokenLimit {
		s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeBlockedQuota, ErrorCode: "daily_tokens"})
		return Draft{}, ErrQuotaExceeded
	}
	if s.Limiter != nil {
		allowed, err := s.Limiter.Allow(ctx, fmt.Sprintf("ai:%s:%s", orgID, feature), s.requestLimit(), time.Minute)
		if err != nil {
			return Draft{}, err
		}
		if !allowed.Allowed {
			s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeRateLimited, ErrorCode: "per_minute"})
			return Draft{}, ErrRateLimited
		}
	}
	// What the tenant has paid for is checked before the provider is called, so a
	// tenant over their plan limit never incurs a model cost. A nil Meter means
	// billing is not configured on this server, which is not a refusal.
	if s.Meter != nil {
		if err := s.Meter.CheckLimit(ctx, orgID, MetricAIRequests, 1); err != nil {
			s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeBlockedPlanLimit, ErrorCode: MetricAIRequests})
			return Draft{}, ErrPlanLimit
		}
	}

	// Minimize and fence before the provider ever sees the text.
	prompt := MinimizeForModel(req.Prompt)
	if untrusted != nil {
		prompt = MinimizeForModel(req.Prompt) + "\n\n" + WrapUntrusted(untrusted.Label, untrusted.Text)
	}
	if len(prompt) > control.MaxInputBytes {
		s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeBlockedQuota, ErrorCode: "input_too_large", InputBytes: len(prompt)})
		return Draft{}, ErrQuotaExceeded
	}
	req.Prompt = prompt
	if req.Feature == "" {
		req.Feature = feature
	}

	result, err := s.Provider.Complete(ctx, req)
	if err != nil {
		s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeProviderError, ErrorCode: "transport",
			InputBytes: len(prompt), LatencyMs: 0})
		return Draft{}, ErrProviderUnavailable
	}
	// Metered after the call, not before: a provider outage must not consume a
	// tenant's paid allowance. Metering failure is logged and ignored, because
	// failing a successful AI call over a counter would be worse than drift.
	if s.Meter != nil {
		if _, meterErr := s.Meter.IncrementUsage(ctx, orgID, MetricAIRequests, 1); meterErr != nil {
			s.Logger.Error("ai_usage_meter_failed", "organization_id", orgID, "feature", feature, "error", meterErr)
		}
	}
	// The raw response is parsed under the strict schema before anything is stored
	// as usable content.
	response, err := DecodeStrict(result.Raw)
	if err != nil {
		s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeSchemaRejected, ErrorCode: "strict_schema",
			InputBytes: len(prompt), RawSample: string(result.Raw), LatencyMs: result.Latency.Milliseconds(),
			ProviderName: s.Provider.Name(), Model: s.Provider.Model(), TemplateVersion: s.Provider.TemplateVersion(feature)})
		return Draft{}, err
	}

	payload, _ := json.Marshal(response)
	cost := int64(0)
	if priced, ok := s.Provider.(*HTTPProvider); ok {
		cost = priced.CostMicros(result.Usage)
	}
	draft := Draft{
		OrganizationID: orgID, Feature: feature, TargetType: targetType, TargetID: targetID,
		RecordIDs: recordIDs, Provider: s.Provider.Name(), Model: s.Provider.Model(),
		TemplateVersion: s.Provider.TemplateVersion(feature), RawResponse: SanitizeForLog(string(result.Raw), 8000),
		Payload: payload, Status: "pending",
	}
	if err := s.insertDraft(ctx, &draft, actor, len(prompt), result, cost); err != nil {
		return Draft{}, err
	}
	s.log(ctx, orgID, actor, feature, Invocation{Outcome: OutcomeSucceeded, DraftID: draft.ID,
		ProviderName: s.Provider.Name(), Model: s.Provider.Model(), TemplateVersion: draft.TemplateVersion,
		InputBytes: len(prompt), InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		CostMicros: cost, LatencyMs: result.Latency.Milliseconds(), RecordIDs: recordIDs})
	return draft, nil
}

// Untrusted is text that an outside party controls (OCR output, a competitor
// page, a community post). It is screened and fenced, never trusted.
type Untrusted struct {
	Label string
	Text  string
}

type Invocation struct {
	Outcome         string
	ErrorCode       string
	ProviderName    string
	Model           string
	TemplateVersion string
	InputBytes      int
	InputTokens     int
	OutputTokens    int
	CostMicros      int64
	LatencyMs       int64
	DraftID         string
	RecordIDs       []string
	RawSample       string
}

// log writes the required audit row.
//
// This MUST run inside the tenant context: ai_invocations has FORCE ROW LEVEL
// SECURITY with a tenant policy, so a bare ExecContext on the pool is rejected
// by RLS and the row is silently lost. An audit trail that fails quietly is
// worse than no audit trail, so the write goes through db.WithTenant and a
// failure is surfaced to the logger.
func (s *Service) log(ctx context.Context, orgID, actor, feature string, in Invocation) {
	if in.Outcome == "" {
		in.Outcome = OutcomeProviderError
	}
	recordIDs := in.RecordIDs
	if recordIDs == nil {
		recordIDs = []string{}
	}
	raw, _ := json.Marshal(recordIDs)
	if in.ErrorCode == "" {
		in.ErrorCode = SanitizeForLog(in.RawSample, 120)
	}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, `INSERT INTO ai_invocations (organization_id, feature, provider, model,
			prompt_template_version, record_ids, input_redacted, input_bytes, input_tokens, output_tokens,
			cost_micros, outcome, error_code, latency_ms, draft_id, created_by_user_id)
			VALUES ($1,$2,$3,$4,$5,$6,true,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,'')::uuid,NULLIF($15,'')::uuid)`,
			orgID, feature, in.ProviderName, in.Model, in.TemplateVersion, string(raw),
			in.InputBytes, in.InputTokens, in.OutputTokens, in.CostMicros, in.Outcome,
			SanitizeForLog(in.ErrorCode, 120), in.LatencyMs, in.DraftID, actor)
		return execErr
	})
	if err != nil && s.Logger != nil {
		s.Logger.Error("ai_invocation_log_failed", "feature", feature, "outcome", in.Outcome, "error", err)
	}
}

// Usage returns requests and tokens consumed today for a tenant. An empty
// feature means "every feature", which is what the quota endpoint needs; the
// alternative is filtering on feature=” and always reporting zero.
func (s *Service) Usage(ctx context.Context, orgID, feature string, day time.Time) (int, int64, error) {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	var requests int
	var tokens int64
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT COUNT(*), COALESCE(SUM(input_tokens+output_tokens),0)
			FROM ai_invocations WHERE organization_id=$1 AND created_at >= $2`
		args := []any{orgID, start}
		if feature != "" {
			args = append(args, feature)
			query += fmt.Sprintf(" AND feature=$%d", len(args))
		}
		return tx.QueryRowContext(ctx, query, args...).Scan(&requests, &tokens)
	})
	return requests, tokens, err
}

type Draft struct {
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organization_id"`
	Feature         string          `json:"feature"`
	Status          string          `json:"status"`
	TargetType      string          `json:"target_type"`
	TargetID        string          `json:"target_id,omitempty"`
	Payload         json.RawMessage `json:"payload"`
	VerifiedPayload json.RawMessage `json:"verified_payload,omitempty"`
	RecordIDs       []string        `json:"record_ids"`
	Provider        string          `json:"provider"`
	Model           string          `json:"model"`
	TemplateVersion string          `json:"prompt_template_version"`
	// RawResponse retains the model output for audit. It is never returned to
	// store users and is truncated on write.
	RawResponse     string    `json:"-"`
	CreatedByUserID string    `json:"created_by_user_id,omitempty"`
	DecisionNote    string    `json:"decision_note,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Service) insertDraft(ctx context.Context, d *Draft, actor string, inputBytes int, result Result, cost int64) error {
	ids, _ := json.Marshal(d.RecordIDs)
	return db.WithTenant(ctx, s.DB, d.OrganizationID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO ai_drafts (organization_id, feature, status, target_type, target_id,
			raw_response, payload, record_ids, provider, model, prompt_template_version, created_by_user_id)
			VALUES ($1,$2,'pending',$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')::uuid)
			RETURNING id::text, created_at`,
			d.OrganizationID, d.Feature, d.TargetType, d.TargetID, d.RawResponse, string(d.Payload),
			string(ids), d.Provider, d.Model, d.TemplateVersion, actor).Scan(&d.ID, &d.CreatedAt)
	})
}

// ListDrafts returns the tenant's drafts, newest first.
func (s *Service) ListDrafts(ctx context.Context, orgID, status string, limit int) ([]Draft, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	out := []Draft{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT id::text, feature, status, target_type, target_id, payload, record_ids, provider, model,
			prompt_template_version, created_at FROM ai_drafts WHERE organization_id=$1`
		args := []any{orgID}
		if status != "" {
			args = append(args, status)
			query += fmt.Sprintf(" AND status=$%d", len(args))
		}
		args = append(args, limit)
		query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Draft
			var payload, ids []byte
			if err := rows.Scan(&d.ID, &d.Feature, &d.Status, &d.TargetType, &d.TargetID, &payload, &ids,
				&d.Provider, &d.Model, &d.TemplateVersion, &d.CreatedAt); err != nil {
				return err
			}
			d.OrganizationID, d.Payload = orgID, payload
			_ = json.Unmarshal(ids, &d.RecordIDs)
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// Decide records a human approve/reject on a draft. Approving a draft does NOT
// apply it; the caller must then run the deterministic, human-gated apply step.
func (s *Service) Decide(ctx context.Context, orgID, actor, draftID, decision, note string) error {
	if decision != "approved" && decision != "rejected" {
		return errors.New("decision must be approved or rejected")
	}
	if len(strings.TrimSpace(note)) > 1000 {
		return errors.New("note is too long")
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE ai_drafts SET status=$3, decided_by_user_id=NULLIF($4,'')::uuid,
			decision_note=$5, decided_at=now() WHERE organization_id=$1 AND id=$2 AND status='pending'`,
			orgID, draftID, decision, actor, strings.TrimSpace(note))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errors.New("draft is not pending or does not belong to this tenant")
		}
		return nil
	})
}

// MarkApplied records that deterministic Go code applied an approved draft.
func (s *Service) MarkApplied(ctx context.Context, orgID, actor, draftID string, verified json.RawMessage) error {
	if len(verified) > 0 && !json.Valid(verified) {
		return errors.New("verified payload must be valid JSON")
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE ai_drafts SET status='applied', applied_at=now(),
			verified_payload=$3 WHERE organization_id=$1 AND id=$2 AND status='approved'`,
			orgID, draftID, string(verified))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errors.New("only an approved draft can be applied")
		}
		return nil
	})
}

// SetKillSwitch toggles the AI kill switch. orgID empty means platform scope,
// which requires the admin role and therefore runs outside tenant RLS.
func (s *Service) SetKillSwitch(ctx context.Context, orgID, actor, scope string, enabled bool, reason string) error {
	if len(strings.TrimSpace(reason)) < 3 {
		return errors.New("a kill-switch reason is required")
	}
	apply := func(execer execer) error {
		_, err := execer.ExecContext(ctx, `INSERT INTO ai_kill_switches (organization_id, scope, enabled, reason, changed_by_user_id)
			VALUES (NULLIF($1,'')::uuid,$2,$3,$4,NULLIF($5,'')::uuid)
			ON CONFLICT (scope, COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid))
			DO UPDATE SET enabled=EXCLUDED.enabled, reason=EXCLUDED.reason,
				changed_by_user_id=EXCLUDED.changed_by_user_id, updated_at=now()`,
			orgID, scope, enabled, strings.TrimSpace(reason), actor)
		return err
	}
	if scope == "platform" {
		database := s.Admin
		if database == nil {
			return errors.New("admin database role is not configured")
		}
		return apply(database)
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error { return apply(tx) })
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// SetFeatureFlag enables or disables one AI feature for a tenant.
func (s *Service) SetFeatureFlag(ctx context.Context, orgID, actor, feature string, enabled bool) error {
	if !ValidFeature(feature) {
		return fmt.Errorf("unknown feature %q", feature)
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO ai_feature_flags (organization_id, feature, enabled, updated_by_user_id)
			VALUES ($1,$2,$3,NULLIF($4,'')::uuid)
			ON CONFLICT (feature, COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid))
			DO UPDATE SET enabled=EXCLUDED.enabled, updated_by_user_id=EXCLUDED.updated_by_user_id, updated_at=now()`,
			orgID, feature, enabled, actor)
		return err
	})
}

// Quota returns the tenant's AI quota and current usage.
func (s *Service) Quota(ctx context.Context, orgID string) (map[string]any, error) {
	control, err := s.LoadControl(ctx, orgID, FeatureExplanation)
	if err != nil {
		return nil, err
	}
	requests, tokens, err := s.Usage(ctx, orgID, "", time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kill_switch_enabled": control.KillSwitchEnabled,
		"kill_switch_scope":   control.KillSwitchScope,
		"kill_switch_reason":  control.KillSwitchReason,
		"daily_request_limit": control.DailyRequestLimit,
		"daily_token_limit":   control.DailyTokenLimit,
		"max_input_bytes":     control.MaxInputBytes,
		"requests_today":      requests,
		"tokens_today":        tokens,
	}, nil
}
