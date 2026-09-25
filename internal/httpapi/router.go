package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"automation/internal/ai"
	"automation/internal/announcements"
	"automation/internal/billing"
	"automation/internal/community"
	"automation/internal/competitor"
	"automation/internal/db"
	"automation/internal/feedback"
	"automation/internal/notifications"
	"automation/internal/observability"
	"automation/internal/permissions"
	"automation/internal/pricing"
	"automation/internal/recommendation"
	"automation/internal/reliability"
	"automation/internal/reporting"
	"automation/internal/woocommerce"
)

// NewRouter builds the API for all implemented phases.
func NewRouter(logger *slog.Logger, database, adminDatabase *sql.DB, corsOrigin string, jwtSecret []byte,
	woo *woocommerce.Service, price *pricing.Service, competitors *competitor.Service, recs *recommendation.Service,
	reports *reporting.Service, notify *notifications.Service, announce *announcements.Service,
	feedbackSvc *feedback.Service, communitySvc *community.Service, aiSvc *ai.Service,
	billingSvc *billing.Service, paystackSecret string, healthChecker *reliability.Health,
	queueMonitor *reliability.QueueMonitor, deadLetter *reliability.DeadLetter) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "alive"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if database == nil {
			writeJSON(w, 200, map[string]string{"status": "ready", "database": "not-configured"})
			return
		}
		if err := database.Ping(); err != nil {
			writeErr(w, 503, "INTERNAL_ERROR", "Database unavailable.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})
	// Deep health with per-dependency results, for load balancers and operators.
	if healthChecker != nil {
		mux.HandleFunc("GET /health/details", func(w http.ResponseWriter, r *http.Request) {
			report := healthChecker.Run(r.Context())
			status := 200
			if report.Status == "unhealthy" {
				status = 503
			}
			writeJSON(w, status, report)
		})
	}

	// Public auth.
	mux.HandleFunc("POST /api/v1/auth/signup", Register(database, jwtSecret, logger))
	mux.HandleFunc("POST /api/v1/auth/signin", Login(database, jwtSecret, logger))
	mux.HandleFunc("POST /api/v1/auth/logout", Logout(database))
	// The public status page is unauthenticated by design: an incident feed
	// behind a login cannot be used by a customer's status page.
	mux.HandleFunc("GET /api/v1/status", StatusPage(database))
	// Operational queue backlog is public for the same reason, but carries no
	// tenant identity: it exposes queue names and counts only.
	mux.HandleFunc("GET /api/v1/status/queues", QueueBacklog(queueMonitor))
	if billingSvc != nil && paystackSecret != "" {
		// The webhook is authenticated by signature, not by session, so it is
		// registered outside the authenticated mux exactly like the Woo webhook.
		mux.HandleFunc("POST /api/v1/webhooks/paystack", PaystackWebhook(billingSvc, paystackSecret))
	}
	if woo != nil {
		mux.HandleFunc("POST /api/v1/webhooks/woocommerce", ReceiveWooWebhook(woo))
	}

	if database != nil {
		protected := http.NewServeMux()
		protected.HandleFunc("GET /api/v1/auth/me", Me(database))
		protected.HandleFunc("GET /api/v1/orgs", ListOrganizations(adminDatabase))
		protected.HandleFunc("POST /api/v1/orgs", CreateOrg(database, logger, billingSvc))

		// Tenant-scoped routes behind slug resolution.
		protected.Handle("GET /api/v1/app/{slug}", OrgFromSlug(database,
			RequirePermission(database, permissions.PStoreView, http.HandlerFunc(GetOrg(database)))))
		protected.Handle("PATCH /api/v1/app/{slug}/theme", OrgFromSlug(database,
			RequirePermission(database, permissions.PStoreSettingsUpd, http.HandlerFunc(UpdateTheme(database, logger)))))
		protected.Handle("GET /api/v1/app/{slug}/audit-logs", OrgFromSlug(database,
			RequirePermission(database, permissions.PPlatformAuditView, http.HandlerFunc(ListAudit(database)))))
		// Audit for store owners too: allow store.view to read own audit trail.
		protected.Handle("GET /api/v1/app/{slug}/audit", OrgFromSlug(database,
			RequirePermission(database, permissions.PStoreView, http.HandlerFunc(ListAudit(database)))))
		protected.Handle("POST /api/v1/app/{slug}/team/invite", OrgFromSlug(database,
			RequirePermission(database, permissions.PStoreTeamInvite, http.HandlerFunc(InviteMember(database, logger)))))
		if woo != nil {
			protected.Handle("POST /api/v1/app/{slug}/integrations/woocommerce/connect", OrgFromSlug(database,
				RequirePermission(database, permissions.PStoreIntegration, http.HandlerFunc(ConnectWooCommerce(woo, logger)))))
			protected.Handle("GET /api/v1/app/{slug}/integrations/woocommerce/status", OrgFromSlug(database,
				RequirePermission(database, permissions.PStoreView, http.HandlerFunc(WooCommerceStatus(woo)))))
			protected.Handle("POST /api/v1/app/{slug}/integrations/woocommerce/test", OrgFromSlug(database,
				RequirePermission(database, permissions.PStoreIntegration, http.HandlerFunc(TestWooCommerce(woo)))))
			protected.Handle("POST /api/v1/app/{slug}/integrations/woocommerce/disconnect", OrgFromSlug(database,
				RequirePermission(database, permissions.PStoreIntegration, http.HandlerFunc(DisconnectWooCommerce(woo)))))
			protected.Handle("POST /api/v1/app/{slug}/integrations/woocommerce/sync", OrgFromSlug(database,
				RequirePermission(database, permissions.PStoreIntegration, http.HandlerFunc(StartWooSync(woo)))))
			protected.Handle("GET /api/v1/app/{slug}/integrations/woocommerce/sync-runs", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(ListWooSyncRuns(woo)))))
			protected.Handle("GET /api/v1/app/{slug}/integrations/woocommerce/sync-runs/{id}", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(GetWooSyncRun(woo)))))
			protected.Handle("GET /api/v1/app/{slug}/integrations/woocommerce/sync-runs/{id}/report", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(DownloadWooImportReport(woo)))))
			protected.Handle("POST /api/v1/app/{slug}/integrations/woocommerce/sync-runs/{id}/retry", OrgFromSlug(database,
				RequirePermission(database, permissions.PStoreIntegration, http.HandlerFunc(RetryWooSync(woo)))))
			protected.Handle("GET /api/v1/app/{slug}/products", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(ListWooProducts(woo)))))
			protected.Handle("GET /api/v1/app/{slug}/products/{id}", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(GetWooProduct(woo)))))
		}
		if price != nil {
			protected.Handle("GET /api/v1/app/{slug}/costs", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(ListProductCosts(price)))))
			protected.Handle("POST /api/v1/app/{slug}/costs/import", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductCostUpdate, http.HandlerFunc(ImportProductCosts(price)))))
			protected.Handle("GET /api/v1/app/{slug}/cost-alerts", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(ListCostImpactAlerts(price)))))
			protected.Handle("GET /api/v1/app/{slug}/pricing-rules", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(ListPricingRules(price)))))
			protected.Handle("POST /api/v1/app/{slug}/pricing-rules", OrgFromSlug(database,
				RequirePermission(database, permissions.PRuleCreate, http.HandlerFunc(CreatePricingRule(price)))))
			protected.Handle("PATCH /api/v1/app/{slug}/pricing-rules/{id}", OrgFromSlug(database,
				RequirePermission(database, permissions.PRuleUpdate, http.HandlerFunc(UpdatePricingRule(price)))))
			protected.Handle("DELETE /api/v1/app/{slug}/pricing-rules/{id}", OrgFromSlug(database,
				RequirePermission(database, permissions.PRuleUpdate, http.HandlerFunc(DeletePricingRule(price)))))
			protected.Handle("POST /api/v1/app/{slug}/pricing-rules/simulate", OrgFromSlug(database,
				RequirePermission(database, permissions.PRuleCreate, http.HandlerFunc(SimulatePricingRules(price)))))
			protected.Handle("PUT /api/v1/app/{slug}/price-policy", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductPriceManage, http.HandlerFunc(UpdatePricePolicy(price)))))
			protected.Handle("GET /api/v1/app/{slug}/products/{id}/cost-history", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(ListProductCostHistory(price)))))
			protected.Handle("GET /api/v1/app/{slug}/products/{id}/cost", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(GetProductCost(price)))))
			protected.Handle("PUT /api/v1/app/{slug}/products/{id}/cost", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductCostUpdate, http.HandlerFunc(SetProductCost(price)))))
			protected.Handle("GET /api/v1/app/{slug}/products/{id}/pricing", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductView, http.HandlerFunc(EvaluateProductPrice(price)))))
			protected.Handle("PUT /api/v1/app/{slug}/products/{id}/price-lock", OrgFromSlug(database,
				RequirePermission(database, permissions.PProductPriceManage, http.HandlerFunc(UpdatePriceLock(price)))))
		}
		if competitors != nil {
			protected.Handle("GET /api/v1/app/{slug}/competitors", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorView, http.HandlerFunc(ListCompetitors(competitors)))))
			protected.Handle("POST /api/v1/app/{slug}/competitors", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorCreate, http.HandlerFunc(CreateCompetitor(competitors)))))
			protected.Handle("GET /api/v1/app/{slug}/competitor-products", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorView, http.HandlerFunc(ListCompetitorProducts(competitors)))))
			protected.Handle("POST /api/v1/app/{slug}/competitor-products", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorCreate, http.HandlerFunc(AddCompetitorProduct(competitors)))))
			protected.Handle("POST /api/v1/app/{slug}/competitor-products/{id}/refresh", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorCreate, http.HandlerFunc(RefreshCompetitorProduct(competitors)))))
			protected.Handle("POST /api/v1/app/{slug}/competitor-products/{id}/review", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorMatch, http.HandlerFunc(ReviewCompetitorMatch(competitors)))))
			protected.Handle("GET /api/v1/app/{slug}/competitor-products/{id}/history", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorView, http.HandlerFunc(CompetitorHistory(competitors)))))
			protected.Handle("GET /api/v1/app/{slug}/competitor-alerts", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorView, http.HandlerFunc(ListCompetitorAlerts(competitors)))))
			protected.Handle("GET /api/v1/app/{slug}/competitor-health", OrgFromSlug(database, RequirePermission(database, permissions.PCompetitorView, http.HandlerFunc(CompetitorHealth(competitors)))))
		}
		if recs != nil {
			protected.Handle("GET /api/v1/app/{slug}/recommendations", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationView, http.HandlerFunc(ListRecommendations(recs)))))
			protected.Handle("POST /api/v1/app/{slug}/recommendations/generate", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationView, http.HandlerFunc(GenerateRecommendations(recs)))))
			protected.Handle("POST /api/v1/app/{slug}/recommendations/{id}/submit", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationApprove, http.HandlerFunc(SubmitRecommendation(recs)))))
			protected.Handle("GET /api/v1/app/{slug}/price-changes", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationView, http.HandlerFunc(ListPriceChangeRequests(recs)))))
			protected.Handle("POST /api/v1/app/{slug}/price-changes/{id}/approve", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationApprove, http.HandlerFunc(ApprovePriceChange(recs)))))
			protected.Handle("POST /api/v1/app/{slug}/price-changes/{id}/publish", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationPublish, http.HandlerFunc(PublishPriceChange(recs)))))
			protected.Handle("POST /api/v1/app/{slug}/price-changes/{id}/rollback", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationRollback, http.HandlerFunc(RollbackPriceChange(recs)))))
			protected.Handle("GET /api/v1/app/{slug}/pricing-kill-switch", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationPublish, http.HandlerFunc(GetKillSwitch(recs)))))
			protected.Handle("PUT /api/v1/app/{slug}/pricing-kill-switch", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationPublish, http.HandlerFunc(SetTenantKillSwitch(recs)))))
			protected.Handle("GET /api/v1/app/{slug}/approval-limits", OrgFromSlug(database, RequirePermission(database, permissions.PRecommendationView, http.HandlerFunc(ListApprovalLimits(recs)))))
		}

		// ── Phase 6: reports, notification centre, announcements, feedback, community ──
		if reports != nil {
			protected.Handle("GET /api/v1/app/{slug}/reports/kinds", OrgFromSlug(database, RequirePermission(database, permissions.PReportView, http.HandlerFunc(ListReportKinds(reports)))))
			protected.Handle("GET /api/v1/app/{slug}/reports", OrgFromSlug(database, RequirePermission(database, permissions.PReportView, http.HandlerFunc(BuildReport(reports)))))
			protected.Handle("GET /api/v1/app/{slug}/reports/export.csv", OrgFromSlug(database, RequirePermission(database, permissions.PReportExport, http.HandlerFunc(ExportReport(reports, billingSvc)))))
		}
		if notify != nil {
			protected.Handle("GET /api/v1/app/{slug}/notifications", OrgFromSlug(database, RequirePermission(database, permissions.PStoreView, http.HandlerFunc(ListNotifications(notify)))))
			protected.Handle("POST /api/v1/app/{slug}/notifications/{id}/read", OrgFromSlug(database, RequirePermission(database, permissions.PStoreView, http.HandlerFunc(MarkNotificationRead(notify)))))
			protected.Handle("GET /api/v1/app/{slug}/notification-preferences", OrgFromSlug(database, RequirePermission(database, permissions.PStoreView, http.HandlerFunc(ListNotificationPreferences(notify)))))
			protected.Handle("PUT /api/v1/app/{slug}/notification-preferences", OrgFromSlug(database, RequirePermission(database, permissions.PStoreView, http.HandlerFunc(SetNotificationPreference(notify)))))
			protected.Handle("GET /api/v1/app/{slug}/notification-consents", OrgFromSlug(database, RequirePermission(database, permissions.PStoreView, http.HandlerFunc(ListNotificationConsents(notify)))))
			protected.Handle("PUT /api/v1/app/{slug}/notification-consents", OrgFromSlug(database, RequirePermission(database, permissions.PStoreView, http.HandlerFunc(SetNotificationConsent(notify)))))
		}
		if announce != nil {
			protected.Handle("GET /api/v1/app/{slug}/announcements", OrgFromSlug(database, RequirePermission(database, permissions.PAnnouncementRead, http.HandlerFunc(AnnouncementInbox(announce)))))
			protected.Handle("POST /api/v1/app/{slug}/announcements/{id}/read", OrgFromSlug(database, RequirePermission(database, permissions.PAnnouncementRead, http.HandlerFunc(MarkAnnouncementRead(announce)))))
		}
		if feedbackSvc != nil {
			protected.Handle("GET /api/v1/app/{slug}/feedback", OrgFromSlug(database, RequirePermission(database, permissions.PFeedbackCreate, http.HandlerFunc(ListFeedbackTickets(feedbackSvc)))))
			protected.Handle("POST /api/v1/app/{slug}/feedback", OrgFromSlug(database, RequirePermission(database, permissions.PFeedbackCreate, http.HandlerFunc(CreateFeedbackTicket(feedbackSvc)))))
			protected.Handle("GET /api/v1/app/{slug}/feedback/{id}", OrgFromSlug(database, RequirePermission(database, permissions.PFeedbackCreate, http.HandlerFunc(GetFeedbackTicket(feedbackSvc)))))
			protected.Handle("POST /api/v1/app/{slug}/feedback/{id}/replies", OrgFromSlug(database, RequirePermission(database, permissions.PFeedbackCreate, http.HandlerFunc(ReplyToFeedbackTicket(feedbackSvc)))))
		}
		if communitySvc != nil {
			protected.Handle("GET /api/v1/community/rooms", RequireAuthOnly(http.HandlerFunc(ListCommunityRooms(communitySvc))))
			protected.Handle("GET /api/v1/community/profile", RequireAuthOnly(http.HandlerFunc(GetCommunityProfile(communitySvc))))
			protected.Handle("PUT /api/v1/community/profile", RequireAuthOnly(http.HandlerFunc(UpsertCommunityProfile(communitySvc))))
			protected.Handle("GET /api/v1/community/rooms/{id}/posts", RequireAuthOnly(http.HandlerFunc(ListCommunityPosts(communitySvc))))
			protected.Handle("POST /api/v1/community/rooms/{id}/posts", RequireAuthOnly(http.HandlerFunc(CreateCommunityPost(communitySvc))))
			protected.Handle("GET /api/v1/community/posts/{id}/replies", RequireAuthOnly(http.HandlerFunc(ListCommunityReplies(communitySvc))))
			protected.Handle("POST /api/v1/community/posts/{id}/replies", RequireAuthOnly(http.HandlerFunc(CreateCommunityReply(communitySvc))))
			protected.Handle("POST /api/v1/community/react", RequireAuthOnly(http.HandlerFunc(ReactToCommunityContent(communitySvc))))
			protected.Handle("POST /api/v1/community/report", RequireAuthOnly(http.HandlerFunc(ReportCommunityContent(communitySvc))))
		}

		if aiSvc != nil {
			protected.Handle("POST /api/v1/app/{slug}/ai/generate", OrgFromSlug(database, RequirePermission(database, permissions.PAIUse, http.HandlerFunc(GenerateAI(aiSvc)))))
			protected.Handle("GET /api/v1/app/{slug}/ai/drafts", OrgFromSlug(database, RequirePermission(database, permissions.PAIUse, http.HandlerFunc(ListAIDrafts(aiSvc)))))
			protected.Handle("POST /api/v1/app/{slug}/ai/drafts/{id}/decide", OrgFromSlug(database, RequirePermission(database, permissions.PAIApprove, http.HandlerFunc(DecideAIDraft(aiSvc)))))
			protected.Handle("GET /api/v1/app/{slug}/ai/quota", OrgFromSlug(database, RequirePermission(database, permissions.PAIUse, http.HandlerFunc(GetAIQuota(aiSvc)))))
			// Disabling AI is a safety right that a store owner must always have,
			// so the tenant kill switch is gated on store settings, not on the
			// platform-only AI configuration permission. Enabling a feature
			// remains platform-governed via PAIConfigure.
			protected.Handle("PUT /api/v1/app/{slug}/ai/kill-switch", OrgFromSlug(database, RequirePermission(database, permissions.PStoreSettingsUpd, http.HandlerFunc(SetAIKillSwitch(aiSvc)))))
			protected.Handle("PUT /api/v1/app/{slug}/ai/features", OrgFromSlug(database, RequirePermission(database, permissions.PAIConfigure, http.HandlerFunc(SetAIFeatureFlag(aiSvc)))))
		}

		if billingSvc != nil {
			// The plan catalogue is not tenant-scoped, so it is deliberately
			// mounted outside the /app/{slug} tree: a pricing page is rendered
			// before a store exists.
			protected.Handle("GET /api/v1/plans", RequireAuthOnly(http.HandlerFunc(ListPlans(billingSvc))))
			protected.Handle("GET /api/v1/app/{slug}/billing", OrgFromSlug(database, RequirePermission(database, permissions.PBillingView, http.HandlerFunc(BillingStatus(billingSvc)))))
			protected.Handle("GET /api/v1/app/{slug}/billing/payments", OrgFromSlug(database, RequirePermission(database, permissions.PBillingView, http.HandlerFunc(ListPayments(billingSvc)))))
			protected.Handle("POST /api/v1/app/{slug}/billing/checkout", OrgFromSlug(database, RequirePermission(database, permissions.PBillingManage, http.HandlerFunc(StartCheckout(billingSvc)))))
			protected.Handle("POST /api/v1/app/{slug}/billing/confirm", OrgFromSlug(database, RequirePermission(database, permissions.PBillingManage, http.HandlerFunc(ConfirmCheckout(billingSvc)))))
			protected.Handle("POST /api/v1/app/{slug}/billing/cancel", OrgFromSlug(database, RequirePermission(database, permissions.PBillingManage, http.HandlerFunc(CancelSubscription(billingSvc)))))
			protected.Handle("POST /api/v1/app/{slug}/billing/reactivate", OrgFromSlug(database, RequirePermission(database, permissions.PBillingManage, http.HandlerFunc(ReactivateSubscription(billingSvc)))))
		}

		// Admin shell (platform perms).
		protected.Handle("GET /api/v1/admin/stores", RequirePlatform(database, adminDatabase, http.HandlerFunc(AdminStores(adminDatabase))))
		protected.Handle("GET /api/v1/admin/audit-logs", RequirePlatform(database, adminDatabase, http.HandlerFunc(AdminAudit(adminDatabase))))
		if recs != nil {
			protected.Handle("GET /api/v1/admin/integrations/pricing-kill-switch", RequirePlatform(database, adminDatabase, http.HandlerFunc(GetPlatformKillSwitch(recs))))
			protected.Handle("PUT /api/v1/admin/integrations/pricing-kill-switch", RequirePlatform(database, adminDatabase, http.HandlerFunc(SetPlatformKillSwitch(recs))))
		}
		if notify != nil {
			protected.Handle("GET /api/v1/admin/notifications/failed", RequirePlatform(database, adminDatabase, http.HandlerFunc(AdminFailedDeliveries(notify))))
		}
		if announce != nil {
			protected.Handle("GET /api/v1/admin/announcements", RequirePlatform(database, adminDatabase, http.HandlerFunc(ListAnnouncements(announce))))
			protected.Handle("POST /api/v1/admin/announcements", RequirePlatform(database, adminDatabase, http.HandlerFunc(CreateAnnouncement(announce))))
			protected.Handle("PATCH /api/v1/admin/announcements/{id}", RequirePlatform(database, adminDatabase, http.HandlerFunc(UpdateAnnouncement(announce))))
			protected.Handle("POST /api/v1/admin/announcements/{id}/archive", RequirePlatform(database, adminDatabase, http.HandlerFunc(ArchiveAnnouncement(announce))))
			protected.Handle("GET /api/v1/admin/announcements/{id}/versions", RequirePlatform(database, adminDatabase, http.HandlerFunc(AnnouncementVersions(announce))))
		}
		if feedbackSvc != nil {
			protected.Handle("POST /api/v1/admin/feedback/{id}/transition", RequirePlatform(database, adminDatabase, http.HandlerFunc(TransitionFeedbackTicket(feedbackSvc))))
			protected.Handle("POST /api/v1/admin/feedback/{id}/replies", RequirePlatform(database, adminDatabase, http.HandlerFunc(AdminReplyToFeedbackTicket(feedbackSvc))))
		}
		if communitySvc != nil {
			protected.Handle("GET /api/v1/admin/community/moderation", RequirePlatform(database, adminDatabase, http.HandlerFunc(ModerationQueue(communitySvc))))
			protected.Handle("POST /api/v1/admin/community/reports/{id}/moderate", RequirePlatform(database, adminDatabase, http.HandlerFunc(ModerateCommunityContent(communitySvc))))
			protected.Handle("POST /api/v1/admin/community/mute", RequirePlatform(database, adminDatabase, http.HandlerFunc(MuteCommunityUser(communitySvc))))
		}
		if woo != nil {
			protected.Handle("GET /api/v1/admin/integrations/woocommerce", RequirePlatform(database, adminDatabase, http.HandlerFunc(AdminWooDiagnostics(woo))))
		}
		if competitors != nil {
			protected.Handle("GET /api/v1/admin/integrations/competitors", RequirePlatform(database, adminDatabase, http.HandlerFunc(AdminCompetitorHealth(competitors))))
		}
		if aiSvc != nil {
			protected.Handle("PUT /api/v1/admin/integrations/ai-kill-switch", RequirePlatform(database, adminDatabase, http.HandlerFunc(SetPlatformAIKillSwitch(aiSvc))))
		}
		// ── Phase 8 operations ──
		if deadLetter != nil {
			// A tenant sees only its own failed jobs; a platform operator sees all.
			protected.Handle("GET /api/v1/app/{slug}/dead-letters", OrgFromSlug(database,
				RequirePermission(database, permissions.PStoreView, http.HandlerFunc(ListDeadLetters(deadLetter, false)))))
			protected.Handle("GET /api/v1/admin/dead-letters", RequirePlatform(database, adminDatabase,
				http.HandlerFunc(ListDeadLetters(deadLetter, true))))
			protected.Handle("POST /api/v1/admin/dead-letters/{id}/resolve", RequirePlatform(database, adminDatabase,
				http.HandlerFunc(ResolveDeadLetter(deadLetter))))
		}
		if queueMonitor != nil {
			protected.Handle("GET /api/v1/admin/queues", RequirePlatform(database, adminDatabase,
				http.HandlerFunc(QueueBacklog(queueMonitor))))
		}
		if database != nil {
			protected.Handle("GET /api/v1/admin/circuit-breakers", RequirePlatform(database, adminDatabase,
				http.HandlerFunc(ListCircuitBreakers(database))))
			protected.Handle("POST /api/v1/admin/circuit-breakers/{name}/reset", RequirePlatform(database, adminDatabase,
				http.HandlerFunc(ResetCircuitBreaker(database))))
			protected.Handle("POST /api/v1/admin/incidents", RequirePlatform(database, adminDatabase,
				http.HandlerFunc(CreateIncident(database))))
			protected.Handle("POST /api/v1/admin/incidents/{id}/resolve", RequirePlatform(database, adminDatabase,
				http.HandlerFunc(ResolveIncident(database))))
			protected.Handle("GET /api/v1/admin/retention-runs", RequirePlatform(database, adminDatabase,
				http.HandlerFunc(ListRetentionRuns(database))))
		}

		// The billing read-only guard sits above every tenant route so it cannot
		// be forgotten when a route is added, and below auth so it only ever
		// evaluates a resolved, authenticated request.
		mux.Handle("/api/", RequireAuth(database, jwtSecret, GuardBillingWrite(billingSvc, database, protected)))
	}

	_ = db.Open
	_ = observability.NewLogger
	return corsMiddleware(corsOrigin, withRequestID(requestLogger(logger, mux)))
}

// RequirePlatform gates platform admin endpoints on platform.tenant.manage.
func RequirePlatform(database, adminDatabase *sql.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := UserIDFromContext(r.Context())
		roleDB := adminDatabase
		if roleDB == nil {
			roleDB = database
		}
		var n int
		_ = roleDB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM member_role_assignments WHERE user_id=$1 AND role_id IN ('super_admin','platform_admin')`, uid).Scan(&n)
		if n == 0 {
			writeErr(w, 403, "PERMISSION_DENIED", "Platform admin access required.", RequestIDFromContext(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AdminStores searches tenants (admin shell foundation).
func AdminStores(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := "%" + r.URL.Query().Get("q") + "%"
		rows, err := database.QueryContext(r.Context(),
			`SELECT id, name, slug, created_at FROM organizations WHERE name ILIKE $1 OR slug ILIKE $1 ORDER BY created_at DESC LIMIT 25`, q)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not search stores.", RequestIDFromContext(r.Context()))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, slugVal, created string
			_ = rows.Scan(&id, &name, &slugVal, &created)
			out = append(out, map[string]any{"id": id, "name": name, "slug": slugVal, "created_at": created})
		}
		writeJSON(w, 200, out)
	}
}

// AdminAudit views recent platform audit events.
func AdminAudit(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.QueryContext(r.Context(),
			`SELECT organization_id, action, resource_type, occurred_at FROM audit_events ORDER BY occurred_at DESC LIMIT 50`)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load audit logs.", RequestIDFromContext(r.Context()))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var org, action, rtype, at string
			var orgNull sql.NullString
			_ = rows.Scan(&orgNull, &action, &rtype, &at)
			if orgNull.Valid {
				org = orgNull.String
			}
			out = append(out, map[string]any{"organization_id": org, "action": action, "resource_type": rtype, "occurred_at": at})
		}
		writeJSON(w, 200, out)
	}
}

func corsMiddleware(origin string, next http.Handler) http.Handler {
	allowed := make(map[string]bool)
	for _, value := range strings.Split(origin, ",") {
		if value = strings.TrimSpace(value); value != "" {
			allowed[value] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestOrigin := r.Header.Get("Origin")
		if allowed[requestOrigin] {
			w.Header().Set("Access-Control-Allow-Origin", requestOrigin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
		} else if len(allowed) == 1 {
			for value := range allowed {
				w.Header().Set("Access-Control-Allow-Origin", value)
			}
		}
		// Every verb the API actually serves. PUT and DELETE were missing, which
		// a browser turns into a preflight failure rather than a visible bug: the
		// kill switch, notification preferences, price policy, and product costs
		// are all PUT, and rule deletion is DELETE, so those controls silently did
		// nothing from the dashboard while working fine from curl.
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, Idempotency-Key")
		// Browsers cap a preflight at 204 and never read a body, but the methods
		// header must still be correct on a direct OPTIONS from tooling.
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http_request", "method", r.Method, "path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFromContext(r.Context()))
	})
}
