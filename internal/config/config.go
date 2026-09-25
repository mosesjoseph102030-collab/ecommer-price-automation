package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds application settings for the phases currently implemented.
type Config struct {
	Env                            string
	HTTPPort                       string
	DatabaseURL                    string
	MigrationDatabaseURL           string
	WorkerDatabaseURL              string
	AdminDatabaseURL               string
	JWTSecret                      string
	CredentialEncryptionKey        string
	CORSAllowedOrigin              string
	FrontendDir                    string
	WooAllowInsecure               bool
	WooAllowPrivateNetworks        bool
	WooWebhookBaseURL              string
	ReconciliationIntervalMinutes  int
	CompetitorAllowedDomains       []string
	CompetitorCheckIntervalMinutes int
	CompetitorFreshnessMinutes     int
	CompetitorAllowPrivateNetworks bool
	RecommendationIntervalMinutes  int
	RecommendationTTLMinutes       int
	// AI provider settings. The key is read from the environment and is never
	// persisted, logged, or returned to a client.
	AIProviderURL    string
	AIProviderKey    string
	AIModel          string
	AIRequestsPerMin int
	// Paystack. The secret key is read from the environment only; it is never
	// stored, logged, or returned to a client.
	PaystackSecretKey    string
	BillingGraceDays     int
	BillingRetentionDays int
	BillingCallbackURL   string
	// BillingDunningMinutes is how often the worker expires grace periods and
	// lapsed trials. It is configuration rather than a constant because a
	// staging environment needs to observe the transition quickly, and a
	// production one does not want a 15-minute loop competing with publishing.
	BillingDunningMinutes int
}

// Load reads environment with safe defaults for local development.
func Load() (Config, error) {
	env := getenv("APP_ENV", "development")
	c := Config{
		Env:                            env,
		HTTPPort:                       getenv("HTTP_PORT", "8080"),
		DatabaseURL:                    os.Getenv("DATABASE_URL"),
		MigrationDatabaseURL:           os.Getenv("MIGRATION_DATABASE_URL"),
		WorkerDatabaseURL:              os.Getenv("WORKER_DATABASE_URL"),
		AdminDatabaseURL:               os.Getenv("ADMIN_DATABASE_URL"),
		JWTSecret:                      os.Getenv("JWT_SECRET"),
		CredentialEncryptionKey:        os.Getenv("CREDENTIAL_ENCRYPTION_KEY"),
		CORSAllowedOrigin:              getenv("CORS_ALLOWED_ORIGIN", "http://localhost:3000"),
		FrontendDir:                    getenv("FRONTEND_DIR", "apps/web-public/out"),
		WooAllowInsecure:               getenvBool("WOOCOMMERCE_ALLOW_INSECURE", env == "development"),
		WooAllowPrivateNetworks:        getenvBool("WOOCOMMERCE_ALLOW_PRIVATE_NETWORKS", env == "development"),
		WooWebhookBaseURL:              os.Getenv("WOOCOMMERCE_WEBHOOK_BASE_URL"),
		ReconciliationIntervalMinutes:  getenvInt("RECONCILIATION_INTERVAL_MINUTES", 15),
		CompetitorAllowedDomains:       getenvList("COMPETITOR_ALLOWED_DOMAINS"),
		CompetitorCheckIntervalMinutes: getenvInt("COMPETITOR_CHECK_INTERVAL_MINUTES", 360),
		CompetitorFreshnessMinutes:     getenvInt("COMPETITOR_FRESHNESS_MINUTES", 720),
		CompetitorAllowPrivateNetworks: getenvBool("COMPETITOR_ALLOW_PRIVATE_NETWORKS", false),
		RecommendationIntervalMinutes:  getenvInt("RECOMMENDATION_INTERVAL_MINUTES", 15),
		RecommendationTTLMinutes:       getenvInt("RECOMMENDATION_TTL_MINUTES", 30),
		AIProviderURL:                  os.Getenv("AI_PROVIDER_URL"),
		AIProviderKey:                  os.Getenv("AI_PROVIDER_API_KEY"),
		AIModel:                        getenv("AI_MODEL", "claude-sonnet-5"),
		AIRequestsPerMin:               getenvInt("AI_REQUESTS_PER_MINUTE", 10),
		PaystackSecretKey:              os.Getenv("PAYSTACK_SECRET_KEY"),
		BillingGraceDays:               getenvInt("BILLING_GRACE_DAYS", 7),
		BillingRetentionDays:           getenvInt("BILLING_RETENTION_DAYS", 90),
		BillingCallbackURL:             getenv("BILLING_CALLBACK_URL", "http://localhost:3001/app"),
		BillingDunningMinutes:          getenvInt("BILLING_DUNNING_INTERVAL_MINUTES", 15),
	}
	if c.JWTSecret != "" && len(c.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	if c.DatabaseURL != "" && c.CredentialEncryptionKey == "" {
		return Config{}, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY is required when DATABASE_URL is configured")
	}
	if c.DatabaseURL != "" {
		if c.MigrationDatabaseURL == "" {
			c.MigrationDatabaseURL = c.DatabaseURL
		}
		if c.WorkerDatabaseURL == "" {
			c.WorkerDatabaseURL = c.DatabaseURL
		}
		if c.AdminDatabaseURL == "" {
			c.AdminDatabaseURL = c.WorkerDatabaseURL
		}
	}
	if c.ReconciliationIntervalMinutes < 1 {
		return Config{}, fmt.Errorf("RECONCILIATION_INTERVAL_MINUTES must be at least 1")
	}
	if c.CompetitorCheckIntervalMinutes < 1 || c.CompetitorFreshnessMinutes < 1 {
		return Config{}, fmt.Errorf("competitor intervals must be at least 1 minute")
	}
	if c.RecommendationIntervalMinutes < 1 || c.RecommendationTTLMinutes < 1 {
		return Config{}, fmt.Errorf("recommendation intervals must be at least 1 minute")
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "TRUE"
}

func getenvList(k string) []string {
	value := strings.TrimSpace(os.Getenv(k))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}
