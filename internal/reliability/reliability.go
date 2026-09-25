// Package reliability provides the operational hardening for Phase 8: health
// checks, circuit breakers, a dead-letter queue, and graceful degradation.
//
// Circuit breakers persist their state in Postgres so a restart does not reset a
// tripped breaker, which is exactly when resetting would be most dangerous.
package reliability

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned to callers when a dependency is failing. The
// dependency is not called at all, which is what makes this graceful degradation
// rather than a retry storm.
var ErrCircuitOpen = errors.New("circuit breaker is open; dependency is temporarily disabled")

const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half_open"
)

// Breaker trips after N consecutive failures and stays open for the cooldown.
type Breaker struct {
	name         string
	threshold    int
	cooldown     time.Duration
	now          func() time.Time
	mu           sync.Mutex
	state        string
	failures     int
	openedAt     time.Time
	openUntil    time.Time
	lastErr      string
	persistence  *sql.DB
	persistEvery int
}

// NewBreaker builds a breaker. persistence may be nil, in which case state is
// process-local only.
func NewBreaker(name string, threshold int, cooldown time.Duration, persistence *sql.DB) *Breaker {
	if threshold < 1 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &Breaker{name: name, threshold: threshold, cooldown: cooldown,
		now: func() time.Time { return time.Now().UTC() }, state: StateClosed,
		persistence: persistence}
}

// Allow reports whether the dependency may be called. It advances a breaker
// whose cooldown has elapsed from open to half_open.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateOpen && b.now().After(b.openUntil) {
		b.state = StateHalfOpen
	}
	return b.state != StateOpen
}

// Record notes the outcome of a call.
func (b *Breaker) Record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		b.failures = 0
		b.state = StateClosed
		b.lastErr = ""
		b.persist()
		return
	}
	b.failures++
	b.lastErr = err.Error()
	if b.state == StateHalfOpen || b.failures >= b.threshold {
		b.state = StateOpen
		b.openedAt = b.now()
		b.openUntil = b.openedAt.Add(b.cooldown)
	}
	b.persist()
}

// Do wraps a call with breaker semantics. A tripped breaker short-circuits.
func (b *Breaker) Do(ctx context.Context, fn func(context.Context) error) error {
	if !b.Allow() {
		return ErrCircuitOpen
	}
	err := fn(ctx)
	b.Record(err)
	return err
}

// State returns a snapshot for diagnostics.
func (b *Breaker) State() (string, int, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state, b.failures, b.lastErr
}

func (b *Breaker) persist() {
	if b.persistence == nil {
		return
	}
	// Best effort: a breaker must never fail the request it is protecting.
	_, _ = b.persistence.ExecContext(context.Background(), `INSERT INTO circuit_breaker_state
		(name, state, consecutive_failures, opened_at, open_until, last_error, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (name) DO UPDATE SET state=EXCLUDED.state,
		consecutive_failures=EXCLUDED.consecutive_failures, opened_at=EXCLUDED.opened_at,
		open_until=EXCLUDED.open_until, last_error=EXCLUDED.last_error, updated_at=now()`,
		b.name, b.state, b.failures, nullTime(b.openedAt), nullTime(b.openUntil), truncate(b.lastErr, 300))
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// ---------------------------------------------------------------------------
// Dead-letter queue
// ---------------------------------------------------------------------------

// DeadLetter records a job that exhausted its retries. Nothing is silently
// dropped: a failed publish or notification ends up here and is visible.
type DeadLetter struct {
	DB *sql.DB
}

// Record writes a failed job to the dead-letter queue.
func (d *DeadLetter) Record(ctx context.Context, queue, kind, orgID string, payload any, cause error, attempts int) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte("{}")
	}
	var id string
	// dead_letter_jobs is operational and written through the admin role, so this
	// uses the pool directly rather than a tenant-scoped transaction.
	err = d.DB.QueryRowContext(ctx, `INSERT INTO dead_letter_jobs (queue, job_kind, organization_id, payload, last_error, attempts)
		VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6) RETURNING id::text`,
		queue, kind, orgID, string(body), truncate(safeMessage(cause), 1000), attempts).Scan(&id)
	return id, err
}

// List returns open dead-letter entries, optionally for one tenant.
func (d *DeadLetter) List(ctx context.Context, orgID string, limit int) ([]map[string]any, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT id::text, queue, job_kind, COALESCE(organization_id::text,''),
		payload, last_error, attempts, created_at FROM dead_letter_jobs
		WHERE status='open' AND ($1='' OR organization_id::text=$1)
		ORDER BY created_at DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, queue, kind, org, payload, lastErr string
		var attempts int
		var created time.Time
		if err := rows.Scan(&id, &queue, &kind, &org, &payload, &lastErr, &attempts, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "queue": queue, "job_kind": kind, "organization_id": org,
			"payload": json.RawMessage(payload), "last_error": lastErr, "attempts": attempts, "created_at": created})
	}
	return out, rows.Err()
}

// Resolve marks an entry handled after an operator replays or discards it.
func (d *DeadLetter) Resolve(ctx context.Context, id, resolution string) error {
	_, err := d.DB.ExecContext(ctx, `UPDATE dead_letter_jobs SET status='resolved', resolved_at=now(),
		last_error=$2 WHERE id::text=$1 AND status='open'`, id, truncate(resolution, 1000))
	return err
}

func safeMessage(err error) string {
	if err == nil {
		return "unknown failure"
	}
	return err.Error()
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

type Check struct {
	Name string
	// Critical checks gate readiness; non-critical failures degrade only.
	Critical bool
	Run      func(ctx context.Context) error
}

type Report struct {
	Status   string            `json:"status"`
	Checks   map[string]string `json:"checks"`
	Critical bool              `json:"degraded"`
	At       time.Time         `json:"checked_at"`
}

// Health runs checks with a per-check timeout so one slow dependency cannot hang
// the whole probe.
type Health struct {
	Checks  []Check
	Timeout time.Duration
}

func (h *Health) Run(ctx context.Context) Report {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	report := Report{Status: "ok", Checks: map[string]string{}, At: time.Now().UTC()}
	for _, check := range h.Checks {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		err := check.Run(checkCtx)
		cancel()
		if err != nil {
			report.Checks[check.Name] = truncate(err.Error(), 200)
			if check.Critical {
				report.Status = "unhealthy"
				report.Critical = true
			} else if report.Status == "ok" {
				report.Status = "degraded"
			}
		} else {
			report.Checks[check.Name] = "ok"
		}
	}
	return report
}
