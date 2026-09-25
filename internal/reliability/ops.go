package reliability

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// QueueDepth is a snapshot of one queue's backlog.
//
// Depth alone is not health: a permanently idle queue and a permanently
// saturated one look identical on a single sample. The age of the oldest
// unfinished job is what separates "nothing to do" from "stuck", so it is
// reported alongside the count.
type QueueDepth struct {
	Queue       string         `json:"queue"`
	Pending     int64          `json:"pending"`
	Failed      int64          `json:"failed"`
	OldestAge   time.Duration  `json:"oldest_age_seconds"`
	Degraded    bool           `json:"degraded"`
	ByStatus    map[string]int `json:"by_status,omitempty"`
	checkedTime time.Time
}

// QueueMonitor reports backlog for the queues that can silently wedge a tenant.
type QueueMonitor struct {
	DB *sql.DB
	// StuckAfter is the age past which a pending job is treated as wedged. A
	// stuck job is a signal to page, not merely to look at.
	StuckAfter time.Duration
}

// queries is deliberately a fixed allowlist. Building these from a caller-
// supplied table name would be an injection vector, and a caller should not be
// able to point the monitor at an arbitrary table anyway.
var queueQueries = []struct {
	name  string
	table string
}{
	{"notifications", "notification_deliveries"},
	{"webhooks", "webhook_delivery_attempts"},
	{"competitor_checks", "competitor_source_runs"},
	{"store_syncs", "store_sync_runs"},
}

// Statuses are the per-queue status values recognised for backlog reporting.
var queueStatuses = []string{"queued", "pending", "running", "failed", "succeeded", "sent", "completed", "cancelled"}

// Snapshot measures every known queue.
//
// Each queue is measured in its own statement with a short per-queue timeout so
// one locked table cannot stall the health endpoint, which is exactly when the
// endpoint matters most.
func (m *QueueMonitor) Snapshot(ctx context.Context) []QueueDepth {
	stuckAfter := m.StuckAfter
	if stuckAfter <= 0 {
		stuckAfter = 15 * time.Minute
	}
	now := time.Now().UTC()
	out := make([]QueueDepth, 0, len(queueQueries))
	for _, spec := range queueQueries {
		queueCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		depth := QueueDepth{Queue: spec.name, ByStatus: map[string]int{}}
		query := fmt.Sprintf(`SELECT COALESCE(status,'unknown'), COUNT(*),
			COALESCE(EXTRACT(EPOCH FROM (now() - MIN(created_at))),0)
			FROM %s GROUP BY status`, spec.table)
		rows, err := m.DB.QueryContext(queueCtx, query)
		if err != nil {
			cancel()
			// A missing or unreadable table degrades this one queue only. The
			// health endpoint must still answer for everything else.
			out = append(out, QueueDepth{Queue: spec.name, Degraded: true,
				ByStatus: map[string]int{"unavailable": 1}})
			continue
		}
		for rows.Next() {
			var status string
			var count int64
			var oldest float64
			if err := rows.Scan(&status, &count, &oldest); err != nil {
				continue
			}
			depth.ByStatus[status] = int(count)
			switch status {
			case "queued", "pending", "running":
				depth.Pending += count
				if time.Duration(oldest*float64(time.Second)) > time.Duration(depth.OldestAge) {
					depth.OldestAge = time.Duration(oldest * float64(time.Second))
				}
			case "failed":
				depth.Failed += count
			}
		}
		rows.Close()
		cancel()
		// A job older than the stuck threshold with work still pending means
		// the worker is not draining this queue.
		depth.Degraded = depth.Failed > 0 || (depth.Pending > 0 && depth.OldestAge > stuckAfter)
		depth.checkedTime = now
		out = append(out, depth)
	}
	return out
}

// Check implements the health Check contract so queue backlog feeds readiness.
func (m *QueueMonitor) Check(ctx context.Context) error {
	for _, depth := range m.Snapshot(ctx) {
		if depth.Degraded {
			// The message names the queue and the numbers, because "queue
			// unhealthy" alone is not actionable at 3am.
			return fmt.Errorf("queue %s: %d pending (oldest %s), %d failed", depth.Queue,
				depth.Pending, depth.OldestAge.Round(time.Second), depth.Failed)
		}
	}
	return nil
}

// RunQueueMonitor logs backlog on a timer. It is observability, not control: it
// never mutates a queue, so running it cannot change tenant-visible behaviour.
func RunQueueMonitor(ctx context.Context, monitor *QueueMonitor, logger *slog.Logger, every time.Duration) {
	if monitor == nil {
		return
	}
	if every <= 0 {
		every = 5 * time.Minute
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	logger.Info("queue_monitor_started", "interval", every.String())
	for {
		select {
		case <-ctx.Done():
			logger.Info("queue_monitor_stopped")
			return
		case <-ticker.C:
			for _, depth := range monitor.Snapshot(ctx) {
				if !depth.Degraded {
					continue
				}
				logger.Warn("queue_backlog", "queue", depth.Queue, "pending", depth.Pending,
					"failed", depth.Failed, "oldest_age_seconds", int(depth.OldestAge.Seconds()))
			}
		}
	}
}

// RetentionJob prunes data that is no longer needed, and records every run.
//
// It is intentionally conservative: it only deletes operational noise
// (expired webhook attempts, old dead letters, stale webhook event rows). It
// never touches business data, so a misconfigured retention window cannot
// destroy a tenant's products, costs, or history. That guarantee is the whole
// reason these are hard-coded rather than configurable per tenant.
type RetentionJob struct {
	DB     *sql.DB
	Logger *slog.Logger
	// WebhookAttemptDays bounds webhook_delivery_attempts history.
	WebhookAttemptDays int
	// DeadLetterDays bounds resolved dead-letter rows.
	DeadLetterDays int
	// WebhookEventDays bounds processed billing webhook events.
	WebhookEventDays int
}

func (r *RetentionJob) webhookDays() int {
	if r.WebhookAttemptDays > 0 {
		return r.WebhookAttemptDays
	}
	return 90
}

func (r *RetentionJob) deadLetterDays() int {
	if r.DeadLetterDays > 0 {
		return r.DeadLetterDays
	}
	return 30
}

func (r *RetentionJob) webhookEventDays() int {
	if r.WebhookEventDays > 0 {
		return r.WebhookEventDays
	}
	return 90
}

// Run performs one retention pass and records the outcome in retention_runs.
func (r *RetentionJob) Run(ctx context.Context) error {
	type prune struct {
		name string
		sql  string
	}
	targets := []prune{
		{"webhook_delivery_attempts", `DELETE FROM webhook_delivery_attempts
			WHERE created_at < now() - ($1 || ' days')::interval`},
		{"dead_letter_jobs", `DELETE FROM dead_letter_jobs
			WHERE status='resolved' AND resolved_at IS NOT NULL AND resolved_at < now() - ($1 || ' days')::interval`},
		{"billing_webhook_events", `DELETE FROM billing_webhook_events
			WHERE processed_at IS NOT NULL AND processed_at < now() - ($1 || ' days')::interval`},
	}
	windows := map[string]int{
		"webhook_delivery_attempts": r.webhookDays(),
		"dead_letter_jobs":          r.deadLetterDays(),
		"billing_webhook_events":    r.webhookEventDays(),
	}
	for _, target := range targets {
		if err := r.prune(ctx, target.name, target.sql, windows[target.name]); err != nil {
			return err
		}
	}
	return nil
}

func (r *RetentionJob) prune(ctx context.Context, name, statement string, days int) error {
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var runID string
	// Record the run first, so a failure mid-prune still leaves a trace that
	// something was attempted.
	if err := r.DB.QueryRowContext(runCtx, `INSERT INTO retention_runs (job_name) VALUES ($1) RETURNING id::text`,
		name).Scan(&runID); err != nil {
		return fmt.Errorf("record retention run %s: %w", name, err)
	}
	result, err := r.DB.ExecContext(runCtx, statement, days)
	if err != nil {
		_, _ = r.DB.ExecContext(runCtx, `UPDATE retention_runs SET detail=$2, finished_at=now() WHERE id=$1`,
			runID, truncate(err.Error(), 500))
		return fmt.Errorf("prune %s: %w", name, err)
	}
	affected := 0
	if result != nil {
		if n, rowsErr := result.RowsAffected(); rowsErr == nil {
			affected = int(n)
		}
	}
	if _, err := r.DB.ExecContext(runCtx, `UPDATE retention_runs SET rows_affected=$2, finished_at=now() WHERE id=$1`,
		runID, affected); err != nil {
		return fmt.Errorf("finish retention run %s: %w", name, err)
	}
	if affected > 0 && r.Logger != nil {
		r.Logger.Info("retention_pruned", "job", name, "rows", affected, "window_days", days)
	}
	return nil
}

// RunRetentionJob drives retention on a timer.
func RunRetentionJob(ctx context.Context, job *RetentionJob, logger *slog.Logger, every time.Duration) {
	if job == nil {
		return
	}
	if every <= 0 {
		every = 24 * time.Hour
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	logger.Info("retention_job_started", "interval", every.String())
	for {
		select {
		case <-ctx.Done():
			logger.Info("retention_job_stopped")
			return
		case <-ticker.C:
			if err := job.Run(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Error("retention_run_failed", "error", err)
			}
		}
	}
}
