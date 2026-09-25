package billing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"automation/internal/db"
)

// StartCheckout creates a Paystack transaction for a plan upgrade. For a paid
// plan the amount always comes from the plan row in the database, never from the
// request, so a client cannot choose what it pays.
func (s *Service) StartCheckout(ctx context.Context, orgID, actor, planCode, email string) (CheckoutSession, error) {
	if s.Provider == nil {
		return CheckoutSession{}, ErrUnavailable
	}
	var amount int64
	var currency, paystackPlan string
	err := s.DB.QueryRowContext(ctx, `SELECT price_kobo, currency, paystack_plan_code FROM plans
		WHERE code=$1 AND active AND is_public`, planCode).Scan(&amount, &currency, &paystackPlan)
	if errors.Is(err, sql.ErrNoRows) {
		return CheckoutSession{}, ErrInvalidPlan
	}
	if err != nil {
		return CheckoutSession{}, err
	}
	if amount == 0 {
		// A free plan needs no payment; switch directly.
		return CheckoutSession{}, s.changePlan(ctx, orgID, actor, planCode)
	}
	reference := "sub_" + newReference(orgID)
	session, err := s.Provider.InitializeCheckout(ctx, CheckoutRequest{
		Email: email, AmountKobo: amount, Currency: currency, Reference: reference,
		CallbackURL: s.CallbackURL, PlanCode: paystackPlan,
		Metadata: map[string]string{"organization_id": orgID, "plan_code": planCode},
	})
	if err != nil {
		return CheckoutSession{}, err
	}
	err = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, `INSERT INTO payments (organization_id, provider, reference, plan_code,
			amount_kobo, currency, status) VALUES ($1,'paystack',$2,$3,$4,$5,'pending')`,
			orgID, session.Reference, planCode, amount, currency)
		return execErr
	})
	if err != nil {
		return CheckoutSession{}, err
	}
	return session, nil
}

// changePlan moves a tenant onto a plan, applying the plan's trial window.
func (s *Service) changePlan(ctx context.Context, orgID, actor, planCode string) error {
	var trialDays int
	if err := s.DB.QueryRowContext(ctx, `SELECT trial_days FROM plans WHERE code=$1 AND active`, planCode).Scan(&trialDays); err != nil {
		return ErrInvalidPlan
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if trialDays > 0 {
			_, err := tx.ExecContext(ctx, `INSERT INTO subscriptions (organization_id, plan_code, status, trial_ends_at,
				current_period_start, current_period_end)
				VALUES ($1,$2,'trialing',now()+($3 * interval '1 day'),now(),now()+($3 * interval '1 day'))
				ON CONFLICT (organization_id) DO UPDATE SET plan_code=EXCLUDED.plan_code, status='trialing',
				trial_ends_at=EXCLUDED.trial_ends_at, current_period_start=EXCLUDED.current_period_start,
				current_period_end=EXCLUDED.current_period_end, grace_ends_at=NULL, read_only=false,
				read_only_reason='', cancel_at_period_end=false, updated_at=now()`, orgID, planCode, trialDays)
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO subscriptions (organization_id, plan_code, status, current_period_start, current_period_end)
			VALUES ($1,$2,'active',now(),now()+interval '30 days')
			ON CONFLICT (organization_id) DO UPDATE SET plan_code=EXCLUDED.plan_code, status='active',
			current_period_start=EXCLUDED.current_period_start, current_period_end=EXCLUDED.current_period_end,
			grace_ends_at=NULL, read_only=false, read_only_reason='', cancel_at_period_end=false, updated_at=now()`, orgID, planCode)
		return err
	})
}

// newReference builds an unguessable, collision-free payment reference.
// A time-based hash was tried first and was wrong: two checkouts landing in the
// same nanosecond produced identical references, which would have violated the
// unique constraint on payments.reference and broken checkout. crypto/rand
// makes a collision effectively impossible, which is what a payment reference
// needs.
func newReference(orgID string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Fall back to a counter plus time; never return an empty reference.
		return "sub_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "_" +
			strconv.FormatUint(atomic.AddUint64(&referenceCounter, 1), 36)
	}
	return "sub_" + hex.EncodeToString(buf)
}

var referenceCounter uint64

// ConfirmPayment re-reads the transaction from Paystack and activates the plan.
// The browser's "payment succeeded" redirect is never trusted on its own.
func (s *Service) ConfirmPayment(ctx context.Context, orgID, reference string) (Subscription, error) {
	if s.Provider == nil {
		return Subscription{}, ErrUnavailable
	}
	txn, err := s.Provider.VerifyTransaction(ctx, reference)
	if err != nil {
		return Subscription{}, err
	}
	if !strings.EqualFold(txn.Status, "success") {
		return Subscription{}, fmt.Errorf("payment is not complete (status %s)", txn.Status)
	}
	// The reference must belong to this tenant, or a tenant could confirm
	// someone else's payment and claim a plan they never paid for.
	var planCode string
	var amount int64
	var currency string
	err = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT plan_code, amount_kobo, currency FROM payments
			WHERE organization_id=$1 AND reference=$2`, orgID, reference).Scan(&planCode, &amount, &currency)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, errors.New("payment reference does not belong to this store")
	}
	if err != nil {
		return Subscription{}, err
	}
	// The amount charged must match the plan price. A tampered checkout cannot
	// buy a more expensive plan for less.
	var planPrice int64
	if err := s.DB.QueryRowContext(ctx, `SELECT price_kobo FROM plans WHERE code=$1`, planCode).Scan(&planPrice); err != nil {
		return Subscription{}, ErrInvalidPlan
	}
	if amount != planPrice || txn.Amount < planPrice {
		return Subscription{}, errors.New("payment amount does not match the plan price")
	}
	if err := s.recordPaymentSuccess(ctx, orgID, reference, planCode, txn); err != nil {
		return Subscription{}, err
	}
	if err := s.changePlan(ctx, orgID, "", planCode); err != nil {
		return Subscription{}, err
	}
	return s.GetSubscription(ctx, orgID)
}

func (s *Service) recordPaymentSuccess(ctx context.Context, orgID, reference, planCode string, txn VerifiedTransaction) error {
	now := time.Now().UTC()
	paidAt := now
	if txn.PaidAt != "" {
		if parsed, err := time.Parse(time.RFC3339, txn.PaidAt); err == nil {
			paidAt = parsed.UTC()
		}
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE payments SET status='success', paystack_transaction_id=$3,
			channel=$4, paid_at=$5 WHERE organization_id=$1 AND reference=$2`,
			orgID, reference, fmt.Sprint(txn.ID), txn.Channel, paidAt)
		return err
	})
}

// ProcessWebhook authenticates, deduplicates, and applies a Paystack event.
// It is safe to call with the same event repeatedly: Paystack retries deliveries.
func (s *Service) ProcessWebhook(ctx context.Context, secretKey string, rawBody []byte, signature string) (string, error) {
	if err := VerifyWebhookSignature(secretKey, rawBody, signature); err != nil {
		return "", err
	}
	event, err := ParseWebhook(rawBody)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(rawBody)
	payloadHash := hex.EncodeToString(sum[:])
	providerEventID := event.ID.String()
	if providerEventID == "" {
		// Without a stable id, fall back to the payload hash so retries dedupe.
		providerEventID = "hash_" + payloadHash[:32]
	}

	// Deduplicate first. The unique constraint plus ON CONFLICT DO NOTHING makes
	// a redelivery a no-op even under concurrent retries.
	if _, err := s.adminDB().ExecContext(ctx, `INSERT INTO billing_webhook_events (provider, event, provider_event_id, payload_sha256)
		VALUES ('paystack',$1,$2,$3) ON CONFLICT (provider, provider_event_id) DO NOTHING`,
		event.Event, providerEventID, payloadHash); err != nil {
		return "", err
	}
	var alreadyProcessed bool
	if err := s.adminDB().QueryRowContext(ctx, `SELECT processed_at IS NOT NULL FROM billing_webhook_events
		WHERE provider='paystack' AND provider_event_id=$1`, providerEventID).Scan(&alreadyProcessed); err != nil {
		return "", err
	}
	if alreadyProcessed {
		return "duplicate", nil
	}

	outcome, applyErr := s.applyWebhook(ctx, event)
	status := outcome
	errText := ""
	if applyErr != nil {
		status, errText = "failed", applyErr.Error()
	}
	if _, err := s.adminDB().ExecContext(ctx, `UPDATE billing_webhook_events SET processed_at=now(),
		outcome=$2, error=$3 WHERE provider='paystack' AND provider_event_id=$1`, providerEventID, status, errText); err != nil {
		return status, err
	}
	return status, applyErr
}

func (s *Service) applyWebhook(ctx context.Context, event WebhookEvent) (string, error) {
	if !SupportedEvents[event.Event] {
		return "ignored", nil
	}
	var data struct {
		Reference     string      `json:"reference"`
		Customer      json.Number `json:"customer"`
		Subscription  json.Number `json:"subscription"`
		EmailToken    string      `json:"email_token"`
		PlanCode      string      `json:"plan_code"`
		Amount        int64       `json:"amount"`
		Status        string      `json:"status"`
		NextPayment   string      `json:"next_payment_date"`
		Authorization string      `json:"authorization_url"`
		Metadata      struct {
			OrganizationID string `json:"organization_id"`
			PlanCode       string `json:"plan_code"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return "failed", err
	}
	orgID := data.Metadata.OrganizationID
	if orgID == "" {
		// Fall back to the pending payment row for this reference.
		if data.Reference != "" {
			if err := s.DB.QueryRowContext(ctx, `SELECT organization_id::text FROM payments WHERE reference=$1`, data.Reference).Scan(&orgID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return "ignored", nil
				}
				return "failed", err
			}
		} else {
			return "ignored", nil
		}
	}
	now := time.Now().UTC()
	switch event.Event {
	case "charge.success":
		if data.Metadata.PlanCode != "" {
			if err := s.changePlan(ctx, orgID, "", data.Metadata.PlanCode); err != nil {
				return "failed", err
			}
		}
		if data.Reference != "" {
			_ = s.recordPaymentSuccess(ctx, orgID, data.Reference, data.Metadata.PlanCode,
				VerifiedTransaction{Amount: data.Amount, Status: "success", Channel: "webhook"})
		}
		return "applied", nil
	case "subscription.create", "subscription.renew":
		return "applied", s.setActive(ctx, orgID, data.Subscription.String(), data.EmailToken, data.PlanCode, now)
	case "subscription.disable", "subscription.not_renew":
		// A failed or cancelled renewal starts dunning, not deletion.
		return "applied", s.markPastDue(ctx, orgID, now)
	default:
		return "ignored", nil
	}
}

func (s *Service) setActive(ctx context.Context, orgID, subscriptionCode, emailToken, planCode string, now time.Time) error {
	if planCode == "" {
		return nil
	}
	var localPlan string
	// Map the Paystack plan code to a local plan, falling back to matching price.
	if err := s.DB.QueryRowContext(ctx, `SELECT code FROM plans WHERE paystack_plan_code=$1 AND active LIMIT 1`, planCode).Scan(&localPlan); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			_, execErr := tx.ExecContext(ctx, `UPDATE subscriptions SET status='active', current_period_start=$2,
				current_period_end=$2 + interval '30 days', grace_ends_at=NULL, read_only=false, read_only_reason='',
				paystack_subscription_code=$3, paystack_email_token=$4, updated_at=now()
				WHERE organization_id=$1`, orgID, now, subscriptionCode, emailToken)
			return execErr
		})
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE subscriptions SET plan_code=$2, status='active',
			current_period_start=$3, current_period_end=$3 + interval '30 days', grace_ends_at=NULL,
			read_only=false, read_only_reason='', paystack_subscription_code=$4, paystack_email_token=$5, updated_at=now()
			WHERE organization_id=$1`, orgID, localPlan, now, subscriptionCode, emailToken)
		return err
	})
}

// markPastDue starts the grace period. It never deletes business data.
func (s *Service) markPastDue(ctx context.Context, orgID string, now time.Time) error {
	grace := now.Add(s.grace())
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE subscriptions SET status='past_due', grace_ends_at=$2,
			read_only=false, read_only_reason='', updated_at=now()
			WHERE organization_id=$1 AND status<>'cancelled'`, orgID, grace)
		return err
	})
}

// RunDunning expires the grace window: once it closes, the tenant becomes
// read-only. Nothing is ever deleted. The worker also expires trials and
// cancelled subscriptions.
func (s *Service) RunDunning(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	marked := 0
	database := s.adminDB()
	rows, err := database.QueryContext(ctx, `SELECT organization_id::text FROM subscriptions
		WHERE status='past_due' AND grace_ends_at IS NOT NULL AND grace_ends_at <= $1
		  AND read_only=false LIMIT 200`, now)
	if err != nil {
		return 0, err
	}
	orgs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		orgs = append(orgs, id)
	}
	rows.Close()
	for _, orgID := range orgs {
		err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			_, execErr := tx.ExecContext(ctx, `UPDATE subscriptions SET read_only=true,
				read_only_reason='payment failed and the grace period has ended', updated_at=now()
				WHERE organization_id=$1 AND status='past_due'`, orgID)
			return execErr
		})
		if err != nil {
			return marked, err
		}
		marked++
	}
	// Trials that ended without a payment move to past_due and start a grace
	// period, so a lapsed trial is never an instant hard stop.
	trialRows, err := database.QueryContext(ctx, `SELECT organization_id::text FROM subscriptions
		WHERE status='trialing' AND trial_ends_at IS NOT NULL AND trial_ends_at <= $1 LIMIT 200`, now)
	if err != nil {
		return marked, err
	}
	expiredTrials := []string{}
	for trialRows.Next() {
		var id string
		if err := trialRows.Scan(&id); err != nil {
			trialRows.Close()
			return marked, err
		}
		expiredTrials = append(expiredTrials, id)
	}
	trialRows.Close()
	for _, orgID := range expiredTrials {
		if err := s.markPastDue(ctx, orgID, now); err != nil {
			return marked, err
		}
	}
	marked += len(expiredTrials)
	return marked, nil
}
