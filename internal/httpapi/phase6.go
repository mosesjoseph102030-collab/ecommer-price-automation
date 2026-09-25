package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"automation/internal/announcements"
	"automation/internal/billing"
	"automation/internal/community"
	"automation/internal/feedback"
	"automation/internal/notifications"
	"automation/internal/reporting"
)

func phase6Error(w http.ResponseWriter, err error) {
	requestID := ""
	message := err.Error()
	switch {
	case errors.Is(err, reporting.ErrUnknownReport):
		writeErr(w, 400, "VALIDATION_ERROR", "Unknown report kind.", requestID)
	case errors.Is(err, announcements.ErrNotFound), errors.Is(err, feedback.ErrNotFound), errors.Is(err, community.ErrNotFound):
		writeErr(w, 404, "RESOURCE_NOT_FOUND", "Record not found.", requestID)
	case errors.Is(err, notifications.ErrCriticalMute):
		writeErr(w, 422, "CRITICAL_ALERT_PROTECTED", err.Error(), requestID)
	case errors.Is(err, notifications.ErrConsentRequired):
		writeErr(w, 422, "CONSENT_REQUIRED", err.Error(), requestID)
	case errors.Is(err, community.ErrBlocked):
		writeErr(w, 422, "COMMUNITY_SAFETY_BLOCK", err.Error(), requestID)
	case errors.Is(err, community.ErrRateLimited):
		writeErr(w, 429, "RATE_LIMITED", err.Error(), requestID)
	case errors.Is(err, community.ErrMuted):
		writeErr(w, 403, "COMMUNITY_MUTED", err.Error(), requestID)
	case errors.Is(err, community.ErrNotVerified):
		writeErr(w, 403, "VERIFIED_OWNER_REQUIRED", err.Error(), requestID)
	case errors.Is(err, announcements.ErrAudience):
		writeErr(w, 422, "VALIDATION_ERROR", err.Error(), requestID)
	case strings.Contains(message, "cannot move a ticket"):
		writeErr(w, 409, "INVALID_TRANSITION", err.Error(), requestID)
	default:
		writeErr(w, 400, "VALIDATION_ERROR", message, requestID)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(target); err != nil {
		writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
		return false
	}
	return true
}

// ── Reports ──

func ListReportKinds(service *reporting.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"kinds": service.KindList()})
	}
}

func BuildReport(service *reporting.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		report, err := service.Build(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("kind"), limit)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, report)
	}
}

// exportRowCap is the largest export the API will build regardless of plan. The
// plan limit is enforced on top of this, so a generous plan still cannot ask the
// database for an unbounded result set.
const exportRowCap = 2000

// ExportReport renders a CSV export.
//
// A read-only store keeps this access deliberately: limiting a tenant to
// read-only is about not changing prices, not about making its data disappear.
// Exports are charged against the plan's monthly row allowance, checked for
// remaining allowance first and then recorded with the actual row count.
func ExportReport(service *reporting.Service, billingSvc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := OrgIDFromContext(r.Context())
		kind := r.URL.Query().Get("kind")
		if billingSvc != nil {
			if err := billingSvc.CheckRemaining(r.Context(), orgID, billing.EntExportRows); err != nil {
				writeBillingError(w, err, RequestIDFromContext(r.Context()))
				return
			}
		}
		report, err := service.Build(r.Context(), orgID, kind, exportRowCap)
		if err != nil {
			phase6Error(w, err)
			return
		}
		// Never emit more rows than the plan allows, even if the plan changed
		// between the check above and here.
		if billingSvc != nil {
			limit, _, allowed, limitErr := billingSvc.Allowance(r.Context(), orgID, billing.EntExportRows)
			if limitErr != nil {
				writeBillingError(w, limitErr, RequestIDFromContext(r.Context()))
				return
			}
			if allowed && int64(report.RowCount) > limit {
				writeErr(w, 402, "PLAN_LIMIT_EXCEEDED",
					fmt.Sprintf("This export has %d rows but your plan allows %d per month. Narrow the date range or filters.", report.RowCount, limit),
					RequestIDFromContext(r.Context()))
				return
			}
		}
		payload, err := report.CSV()
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not render the export.", RequestIDFromContext(r.Context()))
			return
		}
		if _, err := service.RecordExport(r.Context(), orgID, UserIDFromContext(r.Context()), kind, report.RowCount); err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not record the export.", RequestIDFromContext(r.Context()))
			return
		}
		if billingSvc != nil && report.RowCount > 0 {
			if _, err := billingSvc.IncrementUsage(r.Context(), orgID, billing.EntExportRows, int64(report.RowCount)); err != nil {
				// The export already succeeded and was already audited. Failing
				// it here would take away data the tenant is entitled to over a
				// counter write, so the drift is logged instead.
				slog.Warn("export_usage_meter_failed", "organization_id", orgID, "kind", kind, "error", err)
			}
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", kind+"-report.csv"))
		w.WriteHeader(200)
		_, _ = w.Write(payload)
	}
}

// ── Notification centre ──

func ListNotifications(service *notifications.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := service.List(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()),
			r.URL.Query().Get("unread") == "true", limit)
		if err != nil {
			phase6Error(w, err)
			return
		}
		unread, _ := service.UnreadCount(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()))
		writeJSON(w, 200, map[string]any{"notifications": items, "unread": unread})
	}
}

func MarkNotificationRead(service *notifications.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := service.MarkRead(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func SetNotificationPreference(service *notifications.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var pref notifications.Preference
		if !decodeJSON(w, r, 8<<10, &pref) {
			return
		}
		value, err := service.SetPreference(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), pref)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, value)
	}
}

func ListNotificationPreferences(service *notifications.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListPreferences(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"preferences": values})
	}
}

func SetNotificationConsent(service *notifications.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Channel     string `json:"channel"`
			Status      string `json:"status"`
			Destination string `json:"destination"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		value, err := service.SetConsent(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input.Channel, input.Status, input.Destination)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, value)
	}
}

func ListNotificationConsents(service *notifications.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListConsents(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"consents": values})
	}
}

func AdminFailedDeliveries(service *notifications.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		values, err := service.FailedDeliveries(r.Context(), limit)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"failed_deliveries": values})
	}
}

// ── Announcements ──

func CreateAnnouncement(service *announcements.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in announcements.Input
		if !decodeJSON(w, r, 256<<10, &in) {
			return
		}
		value, err := service.Create(r.Context(), UserIDFromContext(r.Context()), in)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 201, value)
	}
}

func ListAnnouncements(service *announcements.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.List(r.Context(), r.URL.Query().Get("status"))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"announcements": values})
	}
}

func UpdateAnnouncement(service *announcements.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in announcements.Input
		if !decodeJSON(w, r, 256<<10, &in) {
			return
		}
		value, err := service.Update(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id"), in)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, value)
	}
}

func ArchiveAnnouncement(service *announcements.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := service.Archive(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id")); err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"archived": true})
	}
}

func AnnouncementVersions(service *announcements.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.Versions(r.Context(), r.PathValue("id"))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"versions": values})
	}
}

func AnnouncementInbox(service *announcements.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.Inbox(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"announcements": values})
	}
}

func MarkAnnouncementRead(service *announcements.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Acknowledge bool `json:"acknowledge"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<10)).Decode(&input)
		if err := service.MarkRead(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id"), input.Acknowledge); err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

// ── Feedback ──

func CreateFeedbackTicket(service *feedback.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in feedback.CreateInput
		if !decodeJSON(w, r, 256<<10, &in) {
			return
		}
		value, err := service.Create(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), in)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 201, value)
	}
}

func ListFeedbackTickets(service *feedback.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.List(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("status"))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"tickets": values})
	}
}

func GetFeedbackTicket(service *feedback.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Get(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, value)
	}
}

func ReplyToFeedbackTicket(service *feedback.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body     string `json:"body"`
			Internal bool   `json:"internal"`
		}
		if !decodeJSON(w, r, 64<<10, &input) {
			return
		}
		value, err := service.Reply(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"), input.Body, false, input.Internal)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 201, value)
	}
}

func TransitionFeedbackTicket(service *feedback.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Status string `json:"status"`
			Note   string `json:"note"`
		}
		if !decodeJSON(w, r, 32<<10, &input) {
			return
		}
		value, err := service.Transition(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"), input.Status, input.Note)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, value)
	}
}

// AdminReplyToFeedbackTicket lets a support agent reply on any tenant's ticket
// and optionally record an internal note that the owner never sees.
func AdminReplyToFeedbackTicket(service *feedback.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body     string `json:"body"`
			Internal bool   `json:"internal"`
		}
		if !decodeJSON(w, r, 64<<10, &input) {
			return
		}
		// Support replies span tenants, so the ticket is resolved through the
		// admin role and the owning organization is read from the ticket itself.
		value, err := service.AdminReply(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id"), input.Body, input.Internal)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 201, value)
	}
}

// RequireAuthOnly gates a platform-shared route on authentication alone.
// Used for community, where access is decided by verified-owner membership in
// Go rather than by a tenant predicate.
func RequireAuthOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserIDFromContext(r.Context()) == "" {
			writeErr(w, 401, "UNAUTHENTICATED", "Sign in to continue.", RequestIDFromContext(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Community ──

func ListCommunityRooms(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListRooms(r.Context(), UserIDFromContext(r.Context()))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"rooms": values})
	}
}

func ListCommunityPosts(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		values, err := service.ListPosts(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id"), limit)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"posts": values})
	}
}

func CreateCommunityPost(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body string `json:"body"`
		}
		if !decodeJSON(w, r, 32<<10, &input) {
			return
		}
		value, err := service.CreatePost(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id"), input.Body)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 201, value)
	}
}

func ListCommunityReplies(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListReplies(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"replies": values})
	}
}

func CreateCommunityReply(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body string `json:"body"`
		}
		if !decodeJSON(w, r, 32<<10, &input) {
			return
		}
		value, err := service.CreateReply(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id"), input.Body)
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 201, value)
	}
}

func ReactToCommunityContent(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Kind       string `json:"kind"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		if err := service.React(r.Context(), UserIDFromContext(r.Context()), input.TargetType, input.TargetID, input.Kind); err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func ReportCommunityContent(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Reason     string `json:"reason"`
		}
		if !decodeJSON(w, r, 8<<10, &input) {
			return
		}
		if err := service.Report(r.Context(), UserIDFromContext(r.Context()), input.TargetType, input.TargetID, input.Reason); err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 201, map[string]bool{"reported": true})
	}
}

func ModerationQueue(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ModerationQueue(r.Context(), r.URL.Query().Get("status"))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": values})
	}
}

func ModerateCommunityContent(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Action     string `json:"action"`
			Resolution string `json:"resolution"`
		}
		if !decodeJSON(w, r, 8<<10, &input) {
			return
		}
		if err := service.Moderate(r.Context(), UserIDFromContext(r.Context()), r.PathValue("id"), input.Action, input.Resolution); err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func MuteCommunityUser(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			UserID     string     `json:"user_id"`
			RoomID     string     `json:"room_id"`
			MutedUntil *time.Time `json:"muted_until"`
			Reason     string     `json:"reason"`
		}
		if !decodeJSON(w, r, 8<<10, &input) {
			return
		}
		if err := service.Mute(r.Context(), UserIDFromContext(r.Context()), input.UserID, input.RoomID, input.MutedUntil, input.Reason); err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func UpsertCommunityProfile(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			DisplayName  string `json:"display_name"`
			Headline     string `json:"headline"`
			Bio          string `json:"bio"`
			PrivacyLevel string `json:"privacy_level"`
		}
		if !decodeJSON(w, r, 8<<10, &input) {
			return
		}
		if err := service.UpsertProfile(r.Context(), UserIDFromContext(r.Context()), input.DisplayName, input.Headline, input.Bio, input.PrivacyLevel); err != nil {
			phase6Error(w, err)
			return
		}
		value, err := service.GetProfile(r.Context(), UserIDFromContext(r.Context()))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, value)
	}
}

func GetCommunityProfile(service *community.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := service.GetProfile(r.Context(), UserIDFromContext(r.Context()))
		if err != nil {
			phase6Error(w, err)
			return
		}
		writeJSON(w, 200, value)
	}
}
