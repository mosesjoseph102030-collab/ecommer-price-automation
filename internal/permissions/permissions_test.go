package permissions

import "testing"

func TestOwnerHasStorePerms(t *testing.T) {
	if !Has(OwnerRoles(), PStoreView) {
		t.Error("owner should have store.view")
	}
	if !Has(OwnerRoles(), PStoreTeamInvite) {
		t.Error("owner should have team.invite")
	}
	if !Has(OwnerRoles(), PStoreIntegration) {
		t.Error("owner should have store.integration.connect")
	}
	if Has([]string{RoleAnalyst}, PStoreSettingsUpd) {
		t.Error("analyst must NOT have store.settings.update")
	}
	if Has([]string{RoleViewer}, PProductView) {
		t.Error("viewer must NOT have product.view")
	}
	if Has([]string{RoleViewer, RoleAnalyst, RoleStaff}, PStoreIntegration) {
		t.Error("only store owners may connect integrations")
	}
	if !Has([]string{RolePricingManager}, PProductCostUpdate) || !Has([]string{RolePricingManager}, PRuleCreate) {
		t.Error("pricing manager should manage costs and rules")
	}
	if Has([]string{RoleAnalyst, RoleViewer}, PProductCostUpdate) || Has([]string{RoleStaff}, PRuleUpdate) {
		t.Error("analyst/viewer cannot edit costs and staff cannot edit pricing rules")
	}
	if !Has([]string{RoleAnalyst}, PCompetitorView) || Has([]string{RoleAnalyst}, PCompetitorMatch) {
		t.Error("analyst should view but not confirm competitor matches")
	}
	if !Has([]string{RolePricingManager}, PCompetitorCreate) || !Has([]string{RolePricingManager}, PCompetitorMatch) {
		t.Error("pricing manager should manage and review competitors")
	}
}

// Phase 6 permissions must respect the documented role boundaries: reports are
// role-gated, community posting requires a store role, and announcement
// management plus community moderation are platform-only.
func TestPhase6PermissionBoundaries(t *testing.T) {
	// Reports: viewers and staff must not see reports.
	if Has([]string{RoleViewer}, PReportView) || Has([]string{RoleStaff}, PReportView) {
		t.Error("viewer/staff must not have report.view")
	}
	if !Has([]string{RoleAnalyst}, PReportView) {
		t.Error("analyst should be able to view reports")
	}
	// Only owners and managers may export: export is data egress, so it is
	// deliberately narrower than read access.
	if Has([]string{RoleAnalyst}, PReportExport) {
		t.Error("analyst may view reports but must not export them")
	}
	if !Has([]string{RoleStoreOwner}, PReportExport) || !Has([]string{RolePricingManager}, PReportExport) {
		t.Error("owner and manager should be able to export reports")
	}
	if Has([]string{RoleStaff, RoleViewer}, PReportExport) {
		t.Error("staff/viewer must not export reports")
	}
	// Community posting is a store-role permission.
	if Has([]string{RoleAnalyst, RoleStaff, RoleViewer}, PCommunityPost) {
		t.Error("analyst/staff/viewer must not post to community")
	}
	if !Has([]string{RoleStoreOwner}, PCommunityPost) {
		t.Error("owner should be able to post to community")
	}
	// Announcements are platform-managed, not tenant-managed.
	for _, role := range []string{RoleStoreOwner, RolePricingManager, RoleAnalyst, RoleStaff, RoleViewer} {
		if Has([]string{role}, PPlatformAnnouncement) {
			t.Errorf("store role %s must not manage announcements", role)
		}
	}
	if !Has([]string{RoleSuperAdmin}, PPlatformAnnouncement) || !Has([]string{RoleProductAdmin}, PPlatformAnnouncement) {
		t.Error("platform roles should manage announcements")
	}
	// Community moderation is platform-only.
	for _, role := range []string{RoleStoreOwner, RolePricingManager, RoleViewer} {
		if Has([]string{role}, PCommunityModerate) {
			t.Errorf("store role %s must not moderate community", role)
		}
	}
	if !Has([]string{RoleCommunityMgr}, PCommunityModerate) {
		t.Error("community manager should moderate")
	}
	// Feedback triage is support/platform work.
	if Has([]string{RoleStoreOwner}, PFeedbackTriage) {
		t.Error("store owner must not triage feedback")
	}
	if !Has([]string{RoleSupportAgent}, PFeedbackTriage) {
		t.Error("support agent should triage feedback")
	}
}

// AI assistance must be opt-in and never grant a store role the ability to
// configure it. Approval of an AI draft is a human act and stays with owners.
func TestPhase7AIPermissions(t *testing.T) {
	if !Has(OwnerRoles(), PAIUse) || !Has(OwnerRoles(), PAIApprove) {
		t.Error("owner should be able to use AI and approve its drafts")
	}
	if Has(OwnerRoles(), PAIConfigure) {
		t.Error("store owner must not be able to configure AI or the kill switch")
	}
	for _, role := range []string{RoleAnalyst, RoleStaff, RoleViewer} {
		if Has([]string{role}, PAIUse) {
			t.Errorf("%s must not use AI features", role)
		}
	}
	if Has([]string{RolePricingManager}, PAIUse) {
		t.Error("pricing manager should not silently gain AI access")
	}
	// AI must never grant a role the ability to publish prices directly.
	if Has([]string{RoleSuperAdmin}, PRecommendationPublish) {
		t.Error("platform roles must not hold store price publishing")
	}
}

// Phase 5 publishing permissions must stay tightly scoped: analysts observe,
// managers approve and publish within limits, and only owners roll back.
func TestPhase5PublishingPermissions(t *testing.T) {
	if !Has(OwnerRoles(), PRecommendationView) || !Has(OwnerRoles(), PRecommendationApprove) ||
		!Has(OwnerRoles(), PRecommendationPublish) || !Has(OwnerRoles(), PRecommendationRollback) {
		t.Error("owner should have full recommendation control")
	}
	if Has(OwnerRoles(), PPlatformTenantManage) {
		t.Error("a store owner must not gain platform tenant management")
	}
	if !Has([]string{RolePricingManager}, PRecommendationApprove) || !Has([]string{RolePricingManager}, PRecommendationPublish) {
		t.Error("pricing manager should approve and publish within their limit")
	}
	if Has([]string{RolePricingManager}, PRecommendationRollback) {
		t.Error("pricing manager must not roll back; rollback is owner-only")
	}
	if Has([]string{RoleAnalyst}, PRecommendationApprove) || Has([]string{RoleAnalyst}, PRecommendationPublish) {
		t.Error("analyst must never approve or publish")
	}
	for _, role := range []string{RoleStaff, RoleViewer, RoleAnalyst} {
		if Has([]string{role}, PRecommendationPublish) {
			t.Errorf("%s must never publish prices", role)
		}
	}
	// No platform role may publish tenant prices through the store route.
	for _, role := range []string{RoleSuperAdmin, RolePlatformAdmin, RoleProductAdmin, RoleFinanceAdmin} {
		if Has([]string{role}, PRecommendationPublish) {
			t.Errorf("platform role %s must not hold store publish permission", role)
		}
	}
}

func TestNoBooleanAdminBypass(t *testing.T) {
	// A store viewer must not gain platform perms without an explicit platform role.
	if Has([]string{RoleViewer}, PPlatformTenantManage) {
		t.Error("viewer must NOT have platform.tenant.manage")
	}
}
