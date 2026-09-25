// Package ratelimit provides a small database-backed limiter. Phase 6 needs
// anti-spam limits for community posts and throttling for outbound WhatsApp.
// It is deliberately simple and dependency-free; Phase 8 may replace it with a
// dedicated limiter if required.
package ratelimit

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Limiter struct {
	DB      *sql.DB
	Clock   func() time.Time
	Enabled bool
}

type Result struct {
	Allowed   bool
	Remaining int
	RetryIn   time.Duration
}

func New(database *sql.DB, enabled bool) *Limiter {
	return &Limiter{DB: database, Enabled: enabled, Clock: func() time.Time { return time.Now().UTC() }}
}

// Allow consumes one unit from a fixed window bucket. It is safe for concurrent
// use because the increment is a single atomic UPSERT.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error) {
	if l == nil || l.DB == nil {
		// Fail closed for anti-abuse purposes: an unconfigured limiter blocks.
		return Result{Allowed: false}, fmt.Errorf("rate limiter is not configured")
	}
	if !l.Enabled {
		return Result{Allowed: true, Remaining: limit}, nil
	}
	if limit < 1 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	now := l.Clock()
	cutoff := now.Add(-window)

	var hits int
	var windowStart time.Time
	err := l.DB.QueryRowContext(ctx, `
		INSERT INTO rate_limit_buckets (bucket_key, window_started_at, hits)
		VALUES ($1, $2, 1)
		ON CONFLICT (bucket_key) DO UPDATE SET
			hits = CASE WHEN rate_limit_buckets.window_started_at < $3 THEN 1 ELSE rate_limit_buckets.hits + 1 END,
			window_started_at = CASE WHEN rate_limit_buckets.window_started_at < $3 THEN $2 ELSE rate_limit_buckets.window_started_at END
		RETURNING hits, window_started_at`, key, now, cutoff).Scan(&hits, &windowStart)
	if err != nil {
		return Result{Allowed: false}, err
	}
	if hits <= limit {
		return Result{Allowed: true, Remaining: limit - hits}, nil
	}
	retry := windowStart.Add(window).Sub(now)
	if retry < 0 {
		retry = 0
	}
	return Result{Allowed: false, Remaining: 0, RetryIn: retry}, nil
}

// Reset clears a bucket (used after a successful moderation action or consent change).
func (l *Limiter) Reset(ctx context.Context, key string) error {
	if l == nil || l.DB == nil {
		return nil
	}
	_, err := l.DB.ExecContext(ctx, `DELETE FROM rate_limit_buckets WHERE bucket_key=$1`, key)
	return err
}

// Cleanup removes expired buckets so the table cannot grow without bound.
func (l *Limiter) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	if l == nil || l.DB == nil {
		return 0, nil
	}
	res, err := l.DB.ExecContext(ctx, `DELETE FROM rate_limit_buckets WHERE window_started_at < $1`, l.Clock().Add(-olderThan))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
