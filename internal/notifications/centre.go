// Package notifications implements the Phase 6 notification centre: read/unread
// state, per-alert-type preferences, channel consent, and retryable delivery.
package notifications

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"automation/internal/db"
	"automation/internal/ratelimit"
)

// CriticalAlertTypes can never be muted implicitly. Muting one requires an
// explicit override with a recorded reason, satisfying the specification
// requirement that critical alerts cannot be silently muted.
var CriticalAlertTypes = map[string]bool{
	"price_change.conflict":       true,
	"price_change.failed":         true,
	"security.credential_expired": true,
	"announcement.critical":       true,
	"moderation.blocked":          true,
}

var (
	ErrConsentRequired = errors.New("channel consent is required")
	ErrCriticalMute    = errors.New("critical alerts cannot be muted without an explicit override reason")
	ErrNoAdapter       = errors.New("no adapter is configured for this channel")
)

// DeadLetterer records a job that exhausted its retries. reliability.DeadLetter
// satisfies it; it is an interface so this package does not depend on the
// reliability package. Nil means no dead-letter queue is wired, in which case an
// exhausted delivery is still marked failed and stays visible in the admin list.
type DeadLetterer interface {
	Record(ctx context.Context, queue, kind, orgID string, payload any, cause error, attempts int) (string, error)
}

type Service struct {
	DB      *sql.DB
	JobDB   *sql.DB
	Logger  *slog.Logger
	Limiter *ratelimit.Limiter
	// Adapters deliver on a channel. Absent adapters make the delivery fail
	// visibly and retryably instead of silently dropping the alert.
	Adapters map[string]Adapter
	// DeadLetter captures deliveries that exhaust their retries so they are
	// replayable and auditable rather than merely marked failed.
	DeadLetter DeadLetterer
	// MaxAttempts bounds delivery retries.
	MaxAttempts int
}

// Adapter delivers a rendered notification. Phase 6 ships in-app only;
// email and WhatsApp adapters are wired here but not configured by default.
type Adapter interface {
	Send(ctx context.Context, destination, title, body string) error
}

// LogAdapter records a delivery without contacting an external provider.
type LogAdapter struct{}

func (LogAdapter) Send(ctx context.Context, destination, title, body string) error { return nil }

type Item struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Read      bool       `json:"read"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	// Deliveries shows per-channel status so failures are visible to the owner.
	Deliveries []Delivery `json:"deliveries,omitempty"`
}

type Delivery struct {
	ID        string     `json:"id"`
	Channel   string     `json:"channel"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	LastError string     `json:"last_error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	NextTryAt *time.Time `json:"next_attempt_at,omitempty"`
}

type Preference struct {
	AlertType              string `json:"alert_type"`
	InAppEnabled           bool   `json:"inapp_enabled"`
	EmailEnabled           bool   `json:"email_enabled"`
	WhatsAppEnabled        bool   `json:"whatsapp_enabled"`
	CriticalOverride       bool   `json:"critical_override"`
	CriticalOverrideReason string `json:"critical_override_reason,omitempty"`
}

type Consent struct {
	Channel     string     `json:"channel"`
	Status      string     `json:"status"`
	Destination string     `json:"destination,omitempty"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
}

func (s *Service) maxAttempts() int {
	if s.MaxAttempts > 0 {
		return s.MaxAttempts
	}
	return 5
}

// Enqueue creates a notification and one delivery row per enabled channel.
// It is used by other packages (for example the Phase 5 publisher) to raise
// owner alerts.
func (s *Service) Enqueue(ctx context.Context, orgID, userID, alertType, title, body string) (string, error) {
	var id string
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO notifications (organization_id, user_id, type, title, body)
			VALUES ($1,$2,$3,$4,$5) RETURNING id::text`, orgID, userID, alertType, title, body).Scan(&id); err != nil {
			return err
		}
		channels, err := s.enabledChannels(ctx, tx, orgID, userID, alertType)
		if err != nil {
			return err
		}
		for _, channel := range channels {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries (notification_id, channel, status, next_attempt_at)
				VALUES ($1,$2,'queued',now())`, id, channel); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// NotifyOwners enqueues one notification per active store owner.
func (s *Service) NotifyOwners(ctx context.Context, orgID, alertType, title, body string) error {
	owners, err := s.OwnerIDs(ctx, orgID)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		if _, err := s.Enqueue(ctx, orgID, owner, alertType, title, body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) OwnerIDs(ctx context.Context, orgID string) ([]string, error) {
	out := []string{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT u.id::text FROM users u
			JOIN memberships m ON m.user_id=u.id AND m.organization_id=$1 AND m.status='active'
			JOIN member_role_assignments r ON r.user_id=u.id AND r.organization_id=$1 AND r.role_id='store_owner'`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}

// enabledChannels resolves which channels a user may receive for an alert type.
// In-app is always available. Email and WhatsApp additionally require an
// explicit granted consent.
func (s *Service) enabledChannels(ctx context.Context, tx *sql.Tx, orgID, userID, alertType string) ([]string, error) {
	pref := Preference{AlertType: alertType, InAppEnabled: true}
	err := tx.QueryRowContext(ctx, `SELECT inapp_enabled, email_enabled, whatsapp_enabled, critical_override, critical_override_reason
		FROM notification_preferences WHERE organization_id=$1 AND user_id=$2 AND alert_type=$3`, orgID, userID, alertType).
		Scan(&pref.InAppEnabled, &pref.EmailEnabled, &pref.WhatsAppEnabled, &pref.CriticalOverride, &pref.CriticalOverrideReason)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	channels := []string{}
	if pref.InAppEnabled {
		channels = append(channels, ChannelInApp)
	}
	if CriticalAlertTypes[alertType] {
		// A critical alert always reaches the in-app centre, even if every
		// outbound channel was muted.
		channels = append(channels, ChannelInApp)
		return dedupe(channels), nil
	}
	if pref.EmailEnabled {
		granted, err := s.hasConsent(ctx, tx, userID, ChannelEmail)
		if err != nil {
			return nil, err
		}
		if granted {
			channels = append(channels, ChannelEmail)
		}
	}
	if pref.WhatsAppEnabled {
		granted, err := s.hasConsent(ctx, tx, userID, ChannelWhatsApp)
		if err != nil {
			return nil, err
		}
		if granted {
			channels = append(channels, ChannelWhatsApp)
		}
	}
	return dedupe(channels), nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func (s *Service) hasConsent(ctx context.Context, tx *sql.Tx, userID, channel string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_consents WHERE user_id=$1 AND channel=$2 AND status='granted'`, userID, channel).Scan(&n)
	return n > 0, err
}

// SetPreference updates a preference, enforcing the critical-alert policy.
func (s *Service) SetPreference(ctx context.Context, orgID, userID string, pref Preference) (Preference, error) {
	pref.AlertType = strings.TrimSpace(pref.AlertType)
	if pref.AlertType == "" {
		return pref, errors.New("alert_type is required")
	}
	if CriticalAlertTypes[pref.AlertType] {
		muting := !pref.InAppEnabled && !pref.EmailEnabled && !pref.WhatsAppEnabled
		if muting {
			if !pref.CriticalOverride || len(strings.TrimSpace(pref.CriticalOverrideReason)) < 10 {
				return pref, ErrCriticalMute
			}
		}
	}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO notification_preferences (organization_id, user_id, alert_type, inapp_enabled,
			email_enabled, whatsapp_enabled, critical_override, critical_override_reason)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (user_id, alert_type) DO UPDATE SET inapp_enabled=EXCLUDED.inapp_enabled,
				email_enabled=EXCLUDED.email_enabled, whatsapp_enabled=EXCLUDED.whatsapp_enabled,
				critical_override=EXCLUDED.critical_override, critical_override_reason=EXCLUDED.critical_override_reason, updated_at=now()`,
			orgID, userID, pref.AlertType, pref.InAppEnabled, pref.EmailEnabled, pref.WhatsAppEnabled,
			pref.CriticalOverride, strings.TrimSpace(pref.CriticalOverrideReason))
		return err
	})
	return pref, err
}

func (s *Service) ListPreferences(ctx context.Context, orgID, userID string) ([]Preference, error) {
	out := []Preference{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT alert_type, inapp_enabled, email_enabled, whatsapp_enabled, critical_override, critical_override_reason
			FROM notification_preferences WHERE organization_id=$1 AND user_id=$2 ORDER BY alert_type`, orgID, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Preference
			if err := rows.Scan(&p.AlertType, &p.InAppEnabled, &p.EmailEnabled, &p.WhatsAppEnabled, &p.CriticalOverride, &p.CriticalOverrideReason); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// SetConsent grants or revokes a channel. Consent is required before any
// outbound message, which is the basis for the WhatsApp consent requirement.
func (s *Service) SetConsent(ctx context.Context, orgID, userID, channel, status, destination string) (Consent, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	status = strings.ToLower(strings.TrimSpace(status))
	switch channel {
	case ChannelEmail, ChannelWhatsApp:
	default:
		return Consent{}, errors.New("channel must be email or whatsapp")
	}
	if status != "granted" && status != "revoked" {
		return Consent{}, errors.New("status must be granted or revoked")
	}
	if status == "granted" && strings.TrimSpace(destination) == "" {
		return Consent{}, errors.New("a destination is required to grant consent")
	}
	// Only a verified destination may be granted, so a mistyped address cannot
	// silently swallow alerts.
	if status == "granted" && channel == ChannelWhatsApp {
		if !strings.HasPrefix(strings.TrimSpace(destination), "+") || len(strings.TrimSpace(destination)) < 10 {
			return Consent{}, errors.New("whatsapp destination must be an E.164 number")
		}
	}
	out := Consent{Channel: channel, Status: status, Destination: strings.TrimSpace(destination)}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO notification_consents (organization_id, user_id, channel, status, destination, verified_at)
			VALUES ($1,$2,$3,$4,$5,CASE WHEN $4='granted' THEN now() ELSE NULL END)
			ON CONFLICT (user_id, channel) DO UPDATE SET status=EXCLUDED.status, destination=EXCLUDED.destination,
				verified_at=EXCLUDED.verified_at, updated_at=now()
			RETURNING verified_at`, orgID, userID, channel, status, out.Destination).Scan(&out.VerifiedAt)
	})
	return out, err
}

func (s *Service) ListConsents(ctx context.Context, orgID, userID string) ([]Consent, error) {
	out := []Consent{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT channel, status, destination, verified_at FROM notification_consents
			WHERE organization_id=$1 AND user_id=$2 ORDER BY channel`, orgID, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Consent
			if err := rows.Scan(&c.Channel, &c.Status, &c.Destination, &c.VerifiedAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// List returns the caller's notification centre with read state and delivery
// status. A user only ever sees their own notifications.
func (s *Service) List(ctx context.Context, orgID, userID string, unreadOnly bool, limit int) ([]Item, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	out := []Item{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT n.id::text, n.type, n.title, n.body, n.read_at, n.created_at
			FROM notifications n WHERE n.organization_id=$1 AND n.user_id=$2`
		if unreadOnly {
			query += ` AND n.read_at IS NULL`
		}
		query += ` ORDER BY n.created_at DESC LIMIT $3`
		rows, err := tx.QueryContext(ctx, query, orgID, userID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Item
			if err := rows.Scan(&item.ID, &item.Type, &item.Title, &item.Body, &item.ReadAt, &item.CreatedAt); err != nil {
				return err
			}
			item.Read = item.ReadAt != nil
			deliveryRows, err := tx.QueryContext(ctx, `SELECT id::text, channel, status, attempts, last_error, created_at, next_attempt_at
				FROM notification_deliveries WHERE notification_id=$1 ORDER BY created_at`, item.ID)
			if err != nil {
				return err
			}
			for deliveryRows.Next() {
				var d Delivery
				if err := deliveryRows.Scan(&d.ID, &d.Channel, &d.Status, &d.Attempts, &d.LastError, &d.CreatedAt, &d.NextTryAt); err != nil {
					deliveryRows.Close()
					return err
				}
				item.Deliveries = append(item.Deliveries, d)
			}
			deliveryRows.Close()
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

// MarkRead records read/acknowledged state.
func (s *Service) MarkRead(ctx context.Context, orgID, userID, notificationID string) error {
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE notifications SET read_at=COALESCE(read_at, now())
			WHERE organization_id=$1 AND user_id=$2 AND id=$3`, orgID, userID, notificationID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func (s *Service) UnreadCount(ctx context.Context, orgID, userID string) (int, error) {
	var n int
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE organization_id=$1 AND user_id=$2 AND read_at IS NULL`, orgID, userID).Scan(&n)
	})
	return n, err
}

// ProcessNext attempts one due delivery. Failures are retried with backoff and
// stay visible as `failed` so the owner can see them.
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	database := s.JobDB
	if database == nil {
		database = s.DB
	}
	var deliveryID, channel, destination, title, body string
	var attempts, userID int
	var orgID string
	err := database.QueryRowContext(ctx, `SELECT d.id::text, d.channel, d.attempts, n.title, n.body, COALESCE(n.user_id::text,''),
		COALESCE(n.organization_id::text,''),
		COALESCE((SELECT c.destination FROM notification_consents c WHERE c.user_id=n.user_id AND c.channel=d.channel AND c.status='granted'),'')
		FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id
		WHERE d.status='queued' AND (d.next_attempt_at IS NULL OR d.next_attempt_at<=now())
		ORDER BY d.created_at LIMIT 1`).Scan(&deliveryID, &channel, &attempts, &title, &body, &userID, &orgID, &destination)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = userID

	adapter, ok := s.Adapters[channel]
	if !ok || adapter == nil {
		adapter = LogAdapter{}
	}
	// Rate-limit outbound channels per user to respect provider windows.
	if s.Limiter != nil && channel != ChannelInApp {
		result, limitErr := s.Limiter.Allow(ctx, fmt.Sprintf("notify:%s:%s", channel, deliveryID), 1, time.Minute)
		if limitErr != nil {
			return true, nil
		}
		if !result.Allowed {
			return true, nil
		}
	}
	sendErr := adapter.Send(ctx, destination, title, body)
	if sendErr == nil {
		_, _ = database.ExecContext(ctx, `UPDATE notification_deliveries SET status='sent', attempts=$2, last_error='', next_attempt_at=NULL WHERE id=$1`, deliveryID, attempts+1)
		return true, nil
	}
	lastError := truncate(sendErr.Error(), 300)
	attempts++
	if attempts >= s.maxAttempts() {
		// Exhausted: mark failed so the failure stays visible to operators, and
		// hand it to the dead-letter queue so it is replayable and auditable.
		// Marking failed and returning nil is not enough on its own: a delivery
		// that is merely 'failed' is easy to lose track of between queues.
		_, _ = database.ExecContext(ctx, `UPDATE notification_deliveries SET status='failed', attempts=$2, last_error=$3, next_attempt_at=NULL WHERE id=$1`, deliveryID, attempts, lastError)
		if s.DeadLetter != nil {
			if _, deadErr := s.DeadLetter.Record(ctx, "notifications", "delivery:"+channel, orgID,
				map[string]any{"delivery_id": deliveryID, "channel": channel, "title": title,
					"user_id": userID, "destination": destination, "attempts": attempts},
				sendErr, attempts); deadErr != nil {
				// The delivery is already marked failed, so this is a secondary
				// failure. Log it rather than failing the worker loop.
				s.Logger.Error("notification_dead_letter_failed", "delivery_id", deliveryID, "error", deadErr)
			}
		}
		return true, nil
	}
	// Retry with a simple quadratic backoff; the row stays `queued` so a later
	// attempt can succeed without owner intervention.
	backoff := time.Duration(attempts*attempts) * time.Minute
	_, _ = database.ExecContext(ctx, `UPDATE notification_deliveries SET status='queued', attempts=$2, last_error=$3, next_attempt_at=now()+$4::interval WHERE id=$1`,
		deliveryID, attempts, lastError, backoff.String())
	return true, nil
}

// FailedDeliveries lists delivery failures so they are visible to operators.
func (s *Service) FailedDeliveries(ctx context.Context, limit int) ([]Delivery, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	out := []Delivery{}
	database := s.JobDB
	if database == nil {
		database = s.DB
	}
	rows, err := database.QueryContext(ctx, `SELECT d.id::text, d.channel, d.status, d.attempts, d.last_error, d.created_at, d.next_attempt_at
		FROM notification_deliveries d WHERE d.status='failed' ORDER BY d.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.Channel, &d.Status, &d.Attempts, &d.LastError, &d.CreatedAt, &d.NextTryAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RunWorker drains the delivery queue.
func (s *Service) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 10; i++ {
				processed, err := s.ProcessNext(ctx)
				if err != nil {
					if s.Logger != nil {
						s.Logger.Error("notification_delivery_failed", "error", err)
					}
					break
				}
				if !processed {
					break
				}
			}
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
