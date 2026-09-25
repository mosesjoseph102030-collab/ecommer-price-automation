package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"automation/internal/billing"
	"automation/internal/config"
	"automation/internal/reliability"
)

// buildBillingService wires Paystack. When no secret key is configured the
// service still works for plans, entitlements, usage, and read-only gating, but
// checkout is disabled, so a missing key cannot be mistaken for a paid upgrade.
func buildBillingService(cfg config.Config, database, adminDatabase *sql.DB, logger *slog.Logger) *billing.Service {
	svc := &billing.Service{
		DB:              database,
		Admin:           adminDatabase,
		GracePeriod:     time.Duration(cfg.BillingGraceDays) * 24 * time.Hour,
		RetentionPeriod: time.Duration(cfg.BillingRetentionDays) * 24 * time.Hour,
		CallbackURL:     cfg.BillingCallbackURL,
		TrialPlanCode:   "trial",
		Logger:          logger,
	}
	if cfg.PaystackSecretKey != "" {
		// Private networks are allowed only in development, so a misconfigured
		// base URL cannot be pointed at an internal service to capture the key.
		provider, err := billing.NewProvider(cfg.PaystackSecretKey, cfg.Env == "development")
		if err != nil {
			logger.Error("paystack_provider_invalid", "error", err)
		} else {
			svc.Provider = provider
			logger.Info("paystack_configured")
		}
	} else {
		logger.Warn("paystack_not_configured",
			"note", "set PAYSTACK_SECRET_KEY to enable checkout; plans, entitlements, and read-only gating still work")
	}
	return svc
}

// buildHealthChecks wires the readiness probes.
//
// Criticality is a deliberate call, not a default: the database is critical
// because nothing works without it, while payments and queues are not, because a
// Paystack outage must degrade checkout rather than take the whole product
// offline for tenants who are reading, exporting, or publishing from
// already-licensed capacity.
func buildHealthChecks(database *sql.DB, svc *billing.Service, monitor *reliability.QueueMonitor, logger *slog.Logger) *reliability.Health {
	checks := []reliability.Check{
		{Name: "database", Critical: true, Run: func(ctx context.Context) error {
			return database.PingContext(ctx)
		}},
		{Name: "payments", Critical: false, Run: func(ctx context.Context) error {
			// Unconfigured is a deliberate state, not a failure: billing without
			// a provider still serves plans, entitlements, and read-only gating.
			if svc == nil || svc.Provider == nil {
				logger.Debug("payments_provider_unconfigured")
				return nil
			}
			// A configured provider must still answer. A failure here degrades
			// checkout only; it does not make the service unready.
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if _, err := svc.Provider.VerifyTransaction(ctx, "probe-not-a-transaction"); err != nil {
				// ErrNotFound is a valid answer from a reachable Paystack: it
				// proves the API is up and the key is accepted.
				if errors.Is(err, billing.ErrNotFound) {
					return nil
				}
				return err
			}
			return nil
		}},
	}
	if monitor != nil {
		checks = append(checks, reliability.Check{Name: "queues", Critical: false, Run: monitor.Check})
	}
	return &reliability.Health{Timeout: 5 * time.Second, Checks: checks}
}

// buildDeadLetter wires the dead-letter queue used when a job exhausts retries.
func buildDeadLetter(adminDatabase *sql.DB) *reliability.DeadLetter {
	return &reliability.DeadLetter{DB: adminDatabase}
}

// buildQueueMonitor wires backlog monitoring for the operational queues.
func buildQueueMonitor(adminDatabase *sql.DB) *reliability.QueueMonitor {
	return &reliability.QueueMonitor{DB: adminDatabase, StuckAfter: 15 * time.Minute}
}

// buildBreakers wires one circuit breaker per outbound dependency. State is
// persisted so a restart does not reset a breaker that is open for a good
// reason.
func buildBreakers(adminDatabase *sql.DB) *reliability.Registry {
	return reliability.NewRegistry(5, 60*time.Second, adminDatabase)
}

// buildRetention wires the data-retention job. It only prunes operational noise
// and never business data.
func buildRetention(adminDatabase *sql.DB, logger *slog.Logger) *reliability.RetentionJob {
	return &reliability.RetentionJob{DB: adminDatabase, Logger: logger,
		WebhookAttemptDays: 90, DeadLetterDays: 30, WebhookEventDays: 90}
}
