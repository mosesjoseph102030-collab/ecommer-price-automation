package main

import (
	"database/sql"
	"log/slog"

	"automation/internal/ai"
	"automation/internal/billing"
	"automation/internal/config"
	"automation/internal/ratelimit"
)

// buildAIService wires the AI provider. When no provider credentials are
// configured the service falls back to a static provider, which keeps every
// governance path (kill switch, feature flags, quotas, invocation logging,
// draft lifecycle) exercisable in development and CI without any outbound call.
//
// billing is passed as the usage Meter so AI calls are charged against the
// tenant's plan allowance. It is optional: nil means billing is not configured on
// this server, and unmetered AI is the correct behaviour there, not a failure.
func buildAIService(cfg config.Config, database, adminDatabase *sql.DB, logger *slog.Logger, limiter *ratelimit.Limiter, billingSvc *billing.Service) *ai.Service {
	var provider ai.Provider
	if cfg.AIProviderKey != "" && cfg.AIProviderURL != "" {
		httpProvider, err := ai.NewHTTPProvider(cfg.AIProviderURL, cfg.AIProviderKey, cfg.AIModel, cfg.Env == "development")
		if err != nil {
			logger.Error("ai_provider_invalid", "error", err)
		} else {
			provider = httpProvider
			logger.Info("ai_provider_configured", "model", cfg.AIModel)
		}
	}
	if provider == nil {
		provider = &ai.StaticProvider{}
		logger.Warn("ai_provider_not_configured_using_static", "note", "set AI_PROVIDER_URL and AI_PROVIDER_API_KEY to enable a real model")
	}
	service := &ai.Service{
		DB: database, Admin: adminDatabase, Provider: provider, Limiter: limiter, Logger: logger,
		RequestsPerMinute: cfg.AIRequestsPerMin,
	}
	if billingSvc != nil {
		service.Meter = billingSvc
	}
	return service
}
