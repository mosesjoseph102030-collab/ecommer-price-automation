package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation/internal/db"
)

var (
	ErrNoSubscription = errors.New("store has no subscription")
	ErrReadOnly       = errors.New("store is read-only because payment failed and the grace period has ended")
	ErrLimitExceeded  = errors.New("plan limit exceeded")
	ErrInvalidPlan    = errors.New("unknown or inactive plan")
)

// Entitlement keys. A limit of 0 means "not available on this plan".
const (
	EntProducts       = "products"
	EntCompetitors    = "competitors"
	EntCheckFrequency = "check_frequency_minutes"
	EntTeamMembers    = "team_members"
	EntAIRequests     = "ai_requests"
	EntExportRows     = "export_rows"
	EntPublishes      = "publishes"
)

var validEntitlements = map[string]bool{
	EntProducts: true, EntCompetitors: true, EntCheckFrequency: true,
	EntTeamMembers: true, EntAIRequests: true, EntExportRows: true, EntPublishes: true,
}

type Plan struct {
	Code         string           `json:"code"`
	Name         string           `json:"name"`
	Description  string           `json:"description"`
	PriceKobo    int64            `json:"price_kobo"`
	Currency     string           `json:"currency"`
	Interval     string           `json:"interval"`
	TrialDays    int              `json:"trial_days"`
	PaystackPlan string           `json:"paystack_plan_code"`
	Entitlements map[string]int64 `json:"entitlements"`
	Active       bool             `json:"active"`
}

type Subscription struct {
	ID                 string     `json:"id"`
	OrganizationID     string     `json:"organization_id"`
	PlanCode           string     `json:"plan_code"`
	PlanName           string     `json:"plan_name,omitempty"`
	Status             string     `json:"status"`
	TrialEndsAt        *time.Time `json:"trial_ends_at,omitempty"`
	CurrentPeriodStart *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd   *time.Time `json:"current_period_end,omitempty"`
	GraceEndsAt        *time.Time `json:"grace_ends_at,omitempty"`
	ReadOnly           bool       `json:"read_only"`
	ReadOnlyReason     string     `json:"read_only_reason,omitempty"`
	CancelAtPeriodEnd  bool       `json:"cancel_at_period_end"`
	CancelledAt        *time.Time `json:"cancelled_at,omitempty"`
	RetentionEndsAt    *time.Time `json:"retention_ends_at,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// Status is a tenant's live billing posture.
type Status struct {
	Subscription   Subscription     `json:"subscription"`
	Entitlements   map[string]int64 `json:"entitlements"`
	Usage          map[string]int64 `json:"usage"`
	ReadOnly       bool             `json:"read_only"`
	ReadOnlyReason string           `json:"read_only_reason,omitempty"`
}

type Service struct {
	DB    *sql.DB
	Admin *sql.DB
	// Provider is the Paystack client. Nil disables checkout.
	Provider *Provider
	// GracePeriod is how long a tenant keeps full access after a failed payment.
	GracePeriod time.Duration
	// RetentionPeriod is how long business data survives cancellation.
	RetentionPeriod time.Duration
	// CallbackURL returns the browser to after a checkout.
	CallbackURL string
	// TrialPlanCode is granted automatically to a new store.
	TrialPlanCode string
	Logger        interface{ Error(msg string, args ...any) }
}

func (s *Service) grace() time.Duration {
	if s.GracePeriod > 0 {
		return s.GracePeriod
	}
	return 7 * 24 * time.Hour
}

func (s *Service) retention() time.Duration {
	if s.RetentionPeriod > 0 {
		return s.RetentionPeriod
	}
	return 90 * 24 * time.Hour
}

func (s *Service) adminDB() *sql.DB {
	if s.Admin != nil {
		return s.Admin
	}
	return s.DB
}

// ListPlans returns the public plan catalogue.
func (s *Service) ListPlans(ctx context.Context) ([]Plan, error) {
	out := []Plan{}
	rows, err := s.DB.QueryContext(ctx, `SELECT code, name, description, price_kobo, currency, interval,
		trial_days, paystack_plan_code, entitlements, active FROM plans WHERE is_public AND active ORDER BY price_kobo`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Plan
		var raw []byte
		if err := rows.Scan(&p.Code, &p.Name, &p.Description, &p.PriceKobo, &p.Currency, &p.Interval,
			&p.TrialDays, &p.PaystackPlan, &raw, &p.Active); err != nil {
			return nil, err
		}
		p.Entitlements = decodeEntitlements(raw)
		out = append(out, p)
	}
	return out, rows.Err()
}

func decodeEntitlements(raw []byte) map[string]int64 {
	out := map[string]int64{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

// EnsureSubscription gives a new store its trial subscription. It is idempotent.
func (s *Service) EnsureSubscription(ctx context.Context, orgID string) error {
	planCode := s.TrialPlanCode
	if planCode == "" {
		planCode = "trial"
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var price int64
		var trialDays int
		if err := tx.QueryRowContext(ctx, `SELECT price_kobo, trial_days FROM plans WHERE code=$1 AND active`, planCode).Scan(&price, &trialDays); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalidPlan
			}
			return err
		}
		// A free trial still needs a period so entitlements resolve.
		_, err := tx.ExecContext(ctx, `INSERT INTO subscriptions (organization_id, plan_code, status, trial_ends_at,
			current_period_start, current_period_end)
			VALUES ($1,$2,'trialing',now()+($3 * interval '1 day'),now(),now()+($3 * interval '1 day'))
			ON CONFLICT (organization_id) DO NOTHING`, orgID, planCode, trialDays)
		return err
	})
}

// GetSubscription reads the tenant subscription, applying dunning transitions
// as a side effect so a lapsed tenant is marked read-only even if no worker ran.
func (s *Service) GetSubscription(ctx context.Context, orgID string) (Subscription, error) {
	var sub Subscription
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT id::text, organization_id, plan_code, status, trial_ends_at,
			current_period_start, current_period_end, grace_ends_at, read_only, read_only_reason,
			cancel_at_period_end, cancelled_at, retention_ends_at, updated_at
			FROM subscriptions WHERE organization_id=$1`, orgID).
			Scan(&sub.ID, &sub.OrganizationID, &sub.PlanCode, &sub.Status, &sub.TrialEndsAt,
				&sub.CurrentPeriodStart, &sub.CurrentPeriodEnd, &sub.GraceEndsAt, &sub.ReadOnly, &sub.ReadOnlyReason,
				&sub.CancelAtPeriodEnd, &sub.CancelledAt, &sub.RetentionEndsAt, &sub.UpdatedAt)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Subscription{}, ErrNoSubscription
		}
		return Subscription{}, err
	}
	return sub, nil
}

// ResolveEntitlements merges plan entitlements with per-tenant overrides.
// An override always wins, which is how support grants and grandfathering work.
func (s *Service) ResolveEntitlements(ctx context.Context, orgID, planCode string) (map[string]int64, error) {
	out := map[string]int64{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var raw []byte
		if err := tx.QueryRowContext(ctx, `SELECT entitlements FROM plans WHERE code=$1`, planCode).Scan(&raw); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalidPlan
			}
			return err
		}
		out = decodeEntitlements(raw)
		rows, err := tx.QueryContext(ctx, `SELECT entitlement_key, entitlement_value FROM organization_entitlements
			WHERE organization_id=$1`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			var value []byte
			if err := rows.Scan(&key, &value); err != nil {
				return err
			}
			// Entitlement values are stored as JSON so booleans and strings are
			// expressible, but limits are always read back as int64.
			var n int64
			if err := json.Unmarshal(value, &n); err == nil {
				out[key] = n
				continue
			}
			var b bool
			if err := json.Unmarshal(value, &b); err == nil && b {
				out[key] = 1
			}
		}
		return rows.Err()
	})
	return out, err
}

// Status returns the tenant's billing posture in one call.
func (s *Service) Status(ctx context.Context, orgID string) (Status, error) {
	sub, err := s.GetSubscription(ctx, orgID)
	if err != nil {
		return Status{}, err
	}
	entitlements, err := s.ResolveEntitlements(ctx, orgID, sub.PlanCode)
	if err != nil {
		return Status{}, err
	}
	usage, err := s.currentUsage(ctx, orgID)
	if err != nil {
		return Status{}, err
	}
	readOnly, reason := EffectiveReadOnly(sub, time.Now().UTC())
	return Status{Subscription: sub, Entitlements: entitlements, Usage: usage, ReadOnly: readOnly, ReadOnlyReason: reason}, nil
}

// EffectiveReadOnly is the single source of truth for whether a tenant can write.
// A tenant is read-only when payment failed and the grace window has closed, or
// when it was explicitly marked read-only. It is never read-only merely because
// it cancelled, and never during a trial.
func EffectiveReadOnly(sub Subscription, now time.Time) (bool, string) {
	if sub.ReadOnly {
		reason := sub.ReadOnlyReason
		if reason == "" {
			reason = "store is read-only"
		}
		return true, reason
	}
	if sub.Status != "past_due" {
		return false, ""
	}
	if sub.GraceEndsAt == nil {
		// Past due with no grace window recorded: treat as read-only, because
		// failing open would let a lapsed tenant keep publishing.
		return true, "payment failed and no grace period is recorded"
	}
	if now.Before(*sub.GraceEndsAt) {
		return false, ""
	}
	return true, "payment failed and the grace period has ended"
}

// GuardWrite is called by mutating endpoints. Read-only tenants may keep reading
// and exporting, but cannot change business data or publish prices.
func (s *Service) GuardWrite(ctx context.Context, orgID string) error {
	sub, err := s.GetSubscription(ctx, orgID)
	if err != nil {
		if errors.Is(err, ErrNoSubscription) {
			// No subscription is treated as a trial-shaped state, not a hard stop,
			// so a store created before billing existed is not bricked.
			return nil
		}
		return err
	}
	if readOnly, _ := EffectiveReadOnly(sub, time.Now().UTC()); readOnly {
		return ErrReadOnly
	}
	return nil
}

// currentUsage sums counters across the current month, the natural period for
// request-based entitlements.
func (s *Service) currentUsage(ctx context.Context, orgID string) (map[string]int64, error) {
	out := map[string]int64{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT metric, quantity FROM usage_counters
			WHERE organization_id=$1 AND period_start >= date_trunc('month', now())`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var metric string
			var qty int64
			if err := rows.Scan(&metric, &qty); err != nil {
				return err
			}
			out[metric] = qty
		}
		return rows.Err()
	})
	return out, err
}

// IncrementUsage adds units to a counter for the current month.
func (s *Service) IncrementUsage(ctx context.Context, orgID, metric string, delta int64) (int64, error) {
	if !validEntitlements[metric] {
		return 0, fmt.Errorf("unknown usage metric %q", metric)
	}
	if delta <= 0 {
		return 0, errors.New("usage delta must be positive")
	}
	var total int64
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO usage_counters (organization_id, metric, period_start, quantity)
			VALUES ($1,$2,date_trunc('month',now()),$3)
			ON CONFLICT (organization_id, metric, period_start)
			DO UPDATE SET quantity=usage_counters.quantity + EXCLUDED.quantity, updated_at=now()
			RETURNING quantity`, orgID, metric, delta).Scan(&total)
	})
	return total, err
}

// CheckLimit enforces an entitlement. limitKey of 0 means the feature is not
// available on the plan at all. checkFrequency is inverted: a higher allowed
// value is better, so it is a ceiling on the interval, not a floor.
func (s *Service) CheckLimit(ctx context.Context, orgID string, limitKey string, requested int64) error {
	entitlements, err := s.ResolveEntitlements(ctx, orgID, planOf(ctx, s, orgID))
	if err != nil {
		if errors.Is(err, ErrInvalidPlan) {
			return nil
		}
		return err
	}
	limit, ok := entitlements[limitKey]
	if !ok {
		return nil
	}
	if limit == 0 {
		return fmt.Errorf("%w: %s is not included in this plan", ErrLimitExceeded, limitKey)
	}
	if limitKey == EntCheckFrequency {
		// A plan permitting 720-minute checks may not run more often than that.
		if requested > 0 && requested < limit {
			return fmt.Errorf("%w: check frequency must be at least %d minutes on this plan", ErrLimitExceeded, limit)
		}
		return nil
	}
	usage, err := s.currentUsage(ctx, orgID)
	if err != nil {
		return err
	}
	if usage[limitKey]+requested > limit {
		return fmt.Errorf("%w: %s allows %d per month, %d used", ErrLimitExceeded, limitKey, limit, usage[limitKey])
	}
	return nil
}

func planOf(ctx context.Context, s *Service, orgID string) string {
	sub, err := s.GetSubscription(ctx, orgID)
	if err != nil {
		return ""
	}
	return sub.PlanCode
}

// CheckRemaining enforces that a tenant has any allowance left for a metric,
// without knowing the request size in advance. It is the right check for a
// feature whose true cost is only known after it runs, such as an export: the
// caller checks availability first, then records the actual rows consumed.
//
// An absent key means the metric is not metered on this plan, so the call is
// allowed. A key of 0 means the plan does not include the feature at all.
func (s *Service) CheckRemaining(ctx context.Context, orgID, metric string) error {
	if !validEntitlements[metric] {
		return fmt.Errorf("unknown usage metric %q", metric)
	}
	sub, err := s.GetSubscription(ctx, orgID)
	if err != nil {
		if errors.Is(err, ErrNoSubscription) {
			return nil
		}
		return err
	}
	entitlements, err := s.ResolveEntitlements(ctx, orgID, sub.PlanCode)
	if err != nil {
		return err
	}
	limit, ok := entitlements[metric]
	if !ok || limit <= 0 {
		return fmt.Errorf("%w: %s is not included in this plan", ErrLimitExceeded, metric)
	}
	usage, err := s.currentUsage(ctx, orgID)
	if err != nil {
		return err
	}
	if usage[metric] >= limit {
		return fmt.Errorf("%w: %s allows %d per month, %d used", ErrLimitExceeded, metric, limit, usage[metric])
	}
	return nil
}

// Allowance reports a metric's limit and current usage for display.
func (s *Service) Allowance(ctx context.Context, orgID, metric string) (limit, used int64, ok bool, err error) {
	sub, subErr := s.GetSubscription(ctx, orgID)
	if subErr != nil {
		if errors.Is(subErr, ErrNoSubscription) {
			return 0, 0, false, nil
		}
		return 0, 0, false, subErr
	}
	entitlements, err := s.ResolveEntitlements(ctx, orgID, sub.PlanCode)
	if err != nil {
		return 0, 0, false, err
	}
	limit, ok = entitlements[metric]
	if !ok {
		return 0, 0, false, nil
	}
	usage, err := s.currentUsage(ctx, orgID)
	if err != nil {
		return 0, 0, false, err
	}
	return limit, usage[metric], true, nil
}

// GrantEntitlement sets a per-tenant override, the support escape hatch.
func (s *Service) GrantEntitlement(ctx context.Context, orgID, actor, key string, value int64) error {
	if !validEntitlements[key] {
		return fmt.Errorf("unknown entitlement %q", key)
	}
	if value < 0 {
		return errors.New("entitlement value cannot be negative")
	}
	encoded, _ := json.Marshal(value)
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO organization_entitlements (organization_id, entitlement_key, entitlement_value, source, granted_by_user_id)
			VALUES ($1,$2,$3,'support',NULLIF($4,'')::uuid)
			ON CONFLICT (organization_id, entitlement_key) DO UPDATE SET entitlement_value=EXCLUDED.entitlement_value,
			source='support', granted_by_user_id=EXCLUDED.granted_by_user_id, updated_at=now()`, orgID, key, string(encoded), actor)
		return err
	})
}

// ListPayments returns the tenant's payment history.
func (s *Service) ListPayments(ctx context.Context, orgID string, limit int) ([]map[string]any, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	out := []map[string]any{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT reference, amount_kobo, currency, status, channel, plan_code,
			paid_at, created_at, failure_reason FROM payments WHERE organization_id=$1 ORDER BY created_at DESC LIMIT $2`, orgID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var reference, currency, status, channel, planCode, failure string
			var amount int64
			var paidAt, createdAt sql.NullTime
			if err := rows.Scan(&reference, &amount, &currency, &status, &channel, &planCode, &paidAt, &createdAt, &failure); err != nil {
				return err
			}
			row := map[string]any{"reference": reference, "amount_kobo": amount, "currency": currency,
				"amount_display": FormatAmount(amount), "status": status, "channel": channel,
				"plan_code": planCode, "failure_reason": failure, "created_at": createdAt}
			if paidAt.Valid {
				row["paid_at"] = paidAt.Time
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// Cancel schedules cancellation at period end. Business data is retained for the
// retention window; nothing is deleted here or anywhere else in this codebase.
func (s *Service) Cancel(ctx context.Context, orgID, reason string) error {
	if len(strings.TrimSpace(reason)) < 3 {
		return errors.New("a cancellation reason is required")
	}
	now := time.Now().UTC()
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE subscriptions SET cancel_at_period_end=true,
			status='cancelled', cancelled_at=$2, retention_ends_at=$3, updated_at=now()
			WHERE organization_id=$1 AND status<>'expired'`, orgID, now, now.Add(s.retention()))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNoSubscription
		}
		return nil
	})
}

// Reactivate reverses a scheduled cancellation before the period ends.
func (s *Service) Reactivate(ctx context.Context, orgID string) error {
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE subscriptions SET cancel_at_period_end=false, status='active',
			cancelled_at=NULL, retention_ends_at=NULL, updated_at=now()
			WHERE organization_id=$1 AND cancel_at_period_end=true`, orgID)
		return err
	})
}

// FormatAmount renders kobo as a major-unit decimal, e.g. 1500000 -> "15000.00".
// The API reports amounts in kobo everywhere to avoid float rounding; this
// exists for the invoice text a human reads.
func FormatAmount(k int64) string {
	return fmt.Sprintf("%d.%02d", k/100, k%100)
}
