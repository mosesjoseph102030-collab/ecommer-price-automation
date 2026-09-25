package flags

// Phase 1 flags only. AI, publishing automation, and billing flags arrive in later phases.
const (
	FlagNewOnboarding = "new_onboarding_checklist"
	FlagEmailNotif    = "email_notifications"
	FlagMaintenance   = "maintenance_mode" // kill switch
)

// IsEnabled resolves: kill switch wins, then org/user override, then default.
func IsEnabled(flagID string, defaultOn bool, killSwitch bool, override *bool) bool {
	if killSwitch && flagID != FlagMaintenance {
		return false
	}
	if override != nil {
		return *override
	}
	return defaultOn
}
