package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"automation/internal/ai"
	"automation/internal/announcements"
	"automation/internal/billing"
	"automation/internal/community"
	"automation/internal/competitor"
	"automation/internal/config"
	"automation/internal/db"
	"automation/internal/feedback"
	"automation/internal/httpapi"
	"automation/internal/notifications"
	"automation/internal/observability"
	"automation/internal/pricing"
	"automation/internal/ratelimit"
	"automation/internal/recommendation"
	"automation/internal/reliability"
	"automation/internal/reporting"
	"automation/internal/woocommerce"

	"github.com/joho/godotenv"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-migrate" {
		// Migrations-only mode: useful for CI and for verifying a migration
		// against a scratch database without starting the HTTP server.
		_ = godotenv.Load()
		cfg, err := config.Load()
		if err != nil {
			observability.NewLogger().Error("invalid_configuration", "error", err)
			os.Exit(1)
		}
		if cfg.MigrationDatabaseURL == "" {
			observability.NewLogger().Error("migration_database_url_required")
			os.Exit(1)
		}
		migrationDatabase, err := db.Open(cfg.MigrationDatabaseURL)
		if err != nil {
			observability.NewLogger().Error("migration_database_connection_failed", "error", err)
			os.Exit(1)
		}
		defer migrationDatabase.Close()
		if err := db.RunMigrations(migrationDatabase, "migrations"); err != nil {
			observability.NewLogger().Error("database_migrations_failed", "error", err)
			os.Exit(1)
		}
		observability.NewLogger().Info("migrations_applied")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		res, err := http.Get("http://127.0.0.1:8080/health/live")
		if err != nil || res.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		res.Body.Close()
		return
	}
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		observability.NewLogger().Error("invalid_configuration", "error", err)
		os.Exit(1)
	}
	logger := observability.NewLogger()

	var database *sql.DB
	var pricingService *pricing.Service
	var competitorService *competitor.Service
	var recommendationService *recommendation.Service
	var reportingService *reporting.Service
	var notificationService *notifications.Service
	var announcementService *announcements.Service
	var feedbackService *feedback.Service
	var communityService *community.Service
	var aiService *ai.Service
	var billingService *billing.Service
	var healthChecker *reliability.Health
	var queueMonitor *reliability.QueueMonitor
	var deadLetter *reliability.DeadLetter
	var limiter *ratelimit.Limiter
	var workerDatabase *sql.DB
	var adminDatabase *sql.DB
	var wooService *woocommerce.Service
	if cfg.DatabaseURL != "" {
		migrationDatabase, openErr := db.Open(cfg.MigrationDatabaseURL)
		if openErr != nil {
			logger.Error("migration_database_connection_failed", "error", openErr)
			os.Exit(1)
		}
		if err := db.RunMigrations(migrationDatabase, "migrations"); err != nil {
			logger.Error("database_migrations_failed", "error", err)
			migrationDatabase.Close()
			os.Exit(1)
		}
		migrationDatabase.Close()

		database, err = db.Open(cfg.DatabaseURL)
		if err != nil {
			logger.Error("database_connection_failed", "error", err)
			os.Exit(1)
		}
		defer database.Close()
		workerDatabase = database
		if cfg.WorkerDatabaseURL != cfg.DatabaseURL {
			workerDatabase, err = db.Open(cfg.WorkerDatabaseURL)
			if err != nil {
				logger.Error("worker_database_connection_failed", "error", err)
				os.Exit(1)
			}
			defer workerDatabase.Close()
		}
		adminDatabase = workerDatabase
		if cfg.AdminDatabaseURL != cfg.WorkerDatabaseURL {
			adminDatabase, err = db.Open(cfg.AdminDatabaseURL)
			if err != nil {
				logger.Error("admin_database_connection_failed", "error", err)
				os.Exit(1)
			}
			defer adminDatabase.Close()
		}
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
		pricingService = &pricing.Service{DB: database}
		competitorService = &competitor.Service{DB: database, JobDB: adminDatabase,
			Provider: competitor.NewProvider(cfg.CompetitorAllowedDomains, cfg.CompetitorAllowPrivateNetworks), Logger: logger,
			CheckInterval: time.Duration(cfg.CompetitorCheckIntervalMinutes) * time.Minute, Freshness: time.Duration(cfg.CompetitorFreshnessMinutes) * time.Minute}
		wooService = &woocommerce.Service{
			DB: database, JobDB: workerDatabase, DiagnosticsDB: adminDatabase,
			Crypter: crypter, HTTPClient: woocommerce.NewHTTPClient(cfg.WooAllowPrivateNetworks), Logger: logger,
			AllowInsecure: cfg.WooAllowInsecure, AllowPrivate: cfg.WooAllowPrivateNetworks,
			WebhookBaseURL: cfg.WooWebhookBaseURL, ReconcileInterval: time.Duration(cfg.ReconciliationIntervalMinutes) * time.Minute,
		}
		recommendationService = &recommendation.Service{
			DB: database, JobDB: adminDatabase, Pricing: pricingService, Woo: wooService, Logger: logger,
			Interval:          time.Duration(cfg.RecommendationIntervalMinutes) * time.Minute,
			RecommendationTTL: time.Duration(cfg.RecommendationTTLMinutes) * time.Minute,
		}
		limiter = ratelimit.New(database, cfg.Env != "development")
		notificationService = &notifications.Service{DB: database, JobDB: adminDatabase, Logger: logger, Limiter: limiter,
			Adapters: map[string]notifications.Adapter{notifications.ChannelInApp: notifications.LogAdapter{}}}
		announcementService = &announcements.Service{DB: database, Admin: adminDatabase}
		feedbackService = &feedback.Service{DB: database, Admin: adminDatabase}
		communityService = &community.Service{DB: database, JobDB: adminDatabase, Admin: adminDatabase, Limiter: limiter}
		reportingService = &reporting.Service{DB: database}
		// Billing is built before AI because AI meters its usage against the
		// tenant's plan allowance.
		billingService = buildBillingService(cfg, database, adminDatabase, logger)
		aiService = buildAIService(cfg, database, adminDatabase, logger, limiter, billingService)
		queueMonitor = buildQueueMonitor(adminDatabase)
		healthChecker = buildHealthChecks(database, billingService, queueMonitor, logger)
		deadLetter = buildDeadLetter(adminDatabase)
		logger.Info("database_ready")
	} else {
		logger.Info("database_not_configured")
	}

	srv := &http.Server{
		Addr: ":" + cfg.HTTPPort,
		Handler: httpapi.NewRouter(logger, database, adminDatabase, cfg.CORSAllowedOrigin, []byte(cfg.JWTSecret),
			wooService, pricingService, competitorService, recommendationService, reportingService,
			notificationService, announcementService, feedbackService, communityService, aiService,
			billingService, cfg.PaystackSecretKey, healthChecker, queueMonitor, deadLetter),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("http_server_starting", "port", cfg.HTTPPort, "env", cfg.Env)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http_server_failed", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	logger.Info("http_server_stopped")
}
