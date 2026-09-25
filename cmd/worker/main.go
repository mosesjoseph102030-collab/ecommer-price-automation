package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"automation/internal/announcements"
	"automation/internal/billing"
	"automation/internal/competitor"
	"automation/internal/config"
	"automation/internal/db"
	"automation/internal/notifications"
	"automation/internal/observability"
	"automation/internal/pricing"
	"automation/internal/ratelimit"
	"automation/internal/recommendation"
	"automation/internal/reliability"
	"automation/internal/woocommerce"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		observability.NewLogger().Error("invalid_configuration", "error", err)
		os.Exit(1)
	}
	logger := observability.NewLogger()
	database, err := db.Open(cfg.WorkerDatabaseURL)
	if err != nil {
		logger.Error("database_connection_failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	key, err := woocommerce.DecodeKey(cfg.CredentialEncryptionKey)
	if err != nil {
		logger.Error("credential_encryption_key_invalid", "error", err)
		os.Exit(1)
	}
	crypter, err := woocommerce.NewCrypter(key)
	if err != nil {
		logger.Error("credential_encrypter_init_failed", "error", err)
		os.Exit(1)
	}
	// One circuit breaker per outbound dependency. The worker owns these because
	// publishing is a background job: a dead store must not consume the whole
	// worker's time budget in retries.
	breakers := reliability.NewRegistry(5, 60*time.Second, database)
	queueMonitor := &reliability.QueueMonitor{DB: database, StuckAfter: 15 * time.Minute}
	deadLetter := &reliability.DeadLetter{DB: database}
	retention := &reliability.RetentionJob{DB: database, Logger: logger,
		WebhookAttemptDays: 90, DeadLetterDays: 30, WebhookEventDays: 90}
	service := &woocommerce.Service{
		DB: database, JobDB: database, DiagnosticsDB: database, Crypter: crypter,
		HTTPClient: woocommerce.NewHTTPClient(cfg.WooAllowPrivateNetworks), Logger: logger,
		AllowInsecure: cfg.WooAllowInsecure, AllowPrivate: cfg.WooAllowPrivateNetworks,
		WebhookBaseURL: cfg.WooWebhookBaseURL, ReconcileInterval: time.Duration(cfg.ReconciliationIntervalMinutes) * time.Minute,
		Breakers: breakers,
	}
	competitorService := &competitor.Service{DB: database, JobDB: database, Provider: competitor.NewProvider(cfg.CompetitorAllowedDomains, cfg.CompetitorAllowPrivateNetworks), Logger: logger,
		CheckInterval: time.Duration(cfg.CompetitorCheckIntervalMinutes) * time.Minute, Freshness: time.Duration(cfg.CompetitorFreshnessMinutes) * time.Minute}
	recommendationService := &recommendation.Service{
		DB: database, JobDB: database, Pricing: &pricing.Service{DB: database}, Woo: service, Logger: logger,
		Interval:          time.Duration(cfg.RecommendationIntervalMinutes) * time.Minute,
		RecommendationTTL: time.Duration(cfg.RecommendationTTLMinutes) * time.Minute,
	}
	limiter := ratelimit.New(database, cfg.Env != "development")
	notificationService := &notifications.Service{DB: database, JobDB: database, Logger: logger, Limiter: limiter,
		Adapters:   map[string]notifications.Adapter{notifications.ChannelInApp: notifications.LogAdapter{}},
		DeadLetter: deadLetter}
	announcementService := &announcements.Service{DB: database, Admin: database}
	// The worker runs dunning because read-only mode is enforced from stored
	// state: without a loop, a lapsed tenant's grace period would never expire
	// and it would keep publishing indefinitely.
	billingService := &billing.Service{
		DB: database, Admin: database,
		GracePeriod:     time.Duration(cfg.BillingGraceDays) * 24 * time.Hour,
		RetentionPeriod: time.Duration(cfg.BillingRetentionDays) * 24 * time.Hour,
		TrialPlanCode:   "trial", Logger: logger,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go competitorService.RunWorker(ctx)
	go recommendationService.RunWorker(ctx)
	go notificationService.RunWorker(ctx)
	go announcementService.RunWorker(ctx)
	go billingService.RunDunningWorker(ctx, logger, time.Duration(cfg.BillingDunningMinutes)*time.Minute)
	go reliability.RunQueueMonitor(ctx, queueMonitor, logger, 5*time.Minute)
	go reliability.RunRetentionJob(ctx, retention, logger, 24*time.Hour)
	logger.Info("woocommerce_worker_started")
	service.RunWorker(ctx)
	logger.Info("woocommerce_worker_stopped")
}
