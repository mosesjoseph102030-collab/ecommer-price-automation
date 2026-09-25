package permissions

// Phase 1 roles only. No pricing/publishing permissions (Phase 5) live here.
const (
	// Store roles
	RoleStoreOwner     = "store_owner"
	RolePricingManager = "pricing_manager"
	RoleAnalyst        = "analyst"
	RoleStaff          = "staff"
	RoleViewer         = "viewer"

	// Platform roles
	RoleSuperAdmin    = "super_admin"
	RolePlatformAdmin = "platform_admin"
	RoleProductAdmin  = "product_admin"
	RoleCommunityMgr  = "community_manager"
	RoleSupportAgent  = "support_agent"
	RoleFinanceAdmin  = "finance_admin"
)

// Store permission strings. AI and billing permissions remain absent.
const (
	PStoreView              = "store.view"
	PStoreSettingsUpd       = "store.settings.update"
	PStoreIntegration       = "store.integration.connect"
	PStoreTeamInvite        = "team.invite"
	PProductView            = "product.view"
	PProductCostUpdate      = "product.cost.update"
	PProductPriceManage     = "product.price.manage"
	PRuleCreate             = "pricing_rule.create"
	PRuleUpdate             = "pricing_rule.update"
	PCompetitorView         = "competitor.view"
	PCompetitorCreate       = "competitor.create"
	PCompetitorMatch        = "competitor.match.review"
	PRecommendationView     = "recommendation.view"
	PRecommendationApprove  = "recommendation.approve"
	PRecommendationPublish  = "recommendation.publish"
	PRecommendationRollback = "recommendation.rollback"
	PReportView             = "report.view"
	PReportExport           = "report.export"
	PNotificationManage     = "notification.manage"
	PAnnouncementRead       = "announcement.read"
	PFeedbackCreate         = "feedback.create"
	PFeedbackTriage         = "feedback.triage"
	PCommunityCreate        = "community.message.create"
	PCommunityModerate      = "community.moderate"
	PCommunityPost          = "community.post"
	PAIUse                  = "ai.use"
	PAIApprove              = "ai.approve"
	PAIConfigure            = "ai.configure"
	PBillingView            = "billing.view"
	PBillingManage          = "billing.manage"

	PPlatformTenantManage = "platform.tenant.manage"
	PPlatformThemePublish = "platform.theme.publish"
	PPlatformAuditView    = "platform.audit.view"
	// Announcement authoring is platform-only; store roles get PAnnouncementRead.
	PPlatformAnnouncement = "platform.announcement.manage"
)

// roleGrants maps a role to the permissions it holds.
var roleGrants = map[string][]string{
	RoleStoreOwner:     {PStoreView, PStoreSettingsUpd, PStoreIntegration, PStoreTeamInvite, PProductView, PProductCostUpdate, PProductPriceManage, PRuleCreate, PRuleUpdate, PCompetitorView, PCompetitorCreate, PCompetitorMatch, PRecommendationView, PRecommendationApprove, PRecommendationPublish, PRecommendationRollback, PReportView, PReportExport, PNotificationManage, PAnnouncementRead, PFeedbackCreate, PCommunityCreate, PCommunityPost, PAIUse, PAIApprove, PBillingView, PBillingManage},
	RolePricingManager: {PStoreView, PProductView, PProductCostUpdate, PProductPriceManage, PRuleCreate, PRuleUpdate, PCompetitorView, PCompetitorCreate, PCompetitorMatch, PRecommendationView, PRecommendationApprove, PRecommendationPublish, PReportView, PReportExport, PAnnouncementRead, PFeedbackCreate, PCommunityCreate, PCommunityPost},
	RoleAnalyst:        {PStoreView, PProductView, PCompetitorView, PRecommendationView, PReportView, PAnnouncementRead, PFeedbackCreate},
	RoleStaff:          {PStoreView, PProductView, PProductCostUpdate, PAnnouncementRead, PFeedbackCreate},
	RoleViewer:         {PStoreView, PAnnouncementRead},

	RoleSuperAdmin:    {PPlatformTenantManage, PPlatformThemePublish, PPlatformAuditView, PPlatformAnnouncement, PAnnouncementRead, PFeedbackCreate, PFeedbackTriage, PCommunityCreate, PCommunityModerate, PAIUse, PAIApprove, PAIConfigure},
	RolePlatformAdmin: {PPlatformTenantManage, PPlatformThemePublish, PPlatformAuditView, PPlatformAnnouncement, PAnnouncementRead, PAIUse, PAIConfigure},
	RoleProductAdmin:  {PAnnouncementRead, PFeedbackCreate, PFeedbackTriage, PPlatformAnnouncement, PAIUse, PAIApprove},
	RoleCommunityMgr:  {PCommunityCreate, PCommunityModerate, PAnnouncementRead},
	RoleSupportAgent:  {PAnnouncementRead, PFeedbackTriage},
	RoleFinanceAdmin:  {PAnnouncementRead},
}

// Has reports whether any of the member's roles grants perm.
// Platform super_admin bypasses store checks only for platform.* perms.
func Has(memberRoles []string, perm string) bool {
	for _, r := range memberRoles {
		for _, p := range roleGrants[r] {
			if p == perm {
				return true
			}
		}
	}
	return false
}

// OwnerRoles is assigned to the organization creator.
func OwnerRoles() []string { return []string{RoleStoreOwner} }
