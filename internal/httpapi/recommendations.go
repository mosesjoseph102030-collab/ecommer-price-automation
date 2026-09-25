package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"automation/internal/recommendation"
)

func ListRecommendations(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		limit, _ := strconv.Atoi(query.Get("limit"))
		filter := recommendation.InboxFilter{
			Urgency:    query.Get("urgency"),
			State:      query.Get("state"),
			MarginRisk: query.Get("margin_risk"),
			CategoryID: query.Get("category_id"),
			ProductID:  query.Get("product_id"),
			Limit:      limit,
		}
		values, err := service.List(r.Context(), OrgIDFromContext(r.Context()), filter)
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"recommendations": values})
	}
}

func GenerateRecommendations(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := OrgIDFromContext(r.Context())
		count, err := service.Generate(r.Context(), orgID)
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"generated": count})
	}
}

func SubmitRecommendation(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, err := service.SubmitForApproval(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 201, request)
	}
}

func ApprovePriceChange(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Decision string `json:"decision"`
			Note     string `json:"note"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		roles, _ := r.Context().Value(rolesKey).([]string)
		request, err := service.Approve(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"), input.Decision, input.Note, roles)
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, request)
	}
}

func ListPriceChangeRequests(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListRequests(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("status"))
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"requests": values})
	}
}

func PublishPriceChange(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ScheduledFor *time.Time `json:"scheduled_for"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&input)
		}
		execution, err := service.QueuePublish(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"), input.ScheduledFor)
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 202, execution)
	}
}

func RollbackPriceChange(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rollback, err := service.QueueRollback(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 202, rollback)
	}
}

func GetKillSwitch(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := service.GetKillSwitch(r.Context(), OrgIDFromContext(r.Context()))
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, value)
	}
}

func SetTenantKillSwitch(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled bool   `json:"enabled"`
			Reason  string `json:"reason"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		value, err := service.SetTenantKillSwitch(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input.Enabled, input.Reason)
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, value)
	}
}

func SetPlatformKillSwitch(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled bool   `json:"enabled"`
			Reason  string `json:"reason"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		value, err := service.SetPlatformKillSwitch(r.Context(), UserIDFromContext(r.Context()), input.Enabled, input.Reason)
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, value)
	}
}

func GetPlatformKillSwitch(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := service.GetPlatformKillSwitch(r.Context())
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, value)
	}
}

func ListApprovalLimits(service *recommendation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListApprovalLimits(r.Context(), OrgIDFromContext(r.Context()))
		if err != nil {
			writeRecommendationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"limits": values})
	}
}

func writeRecommendationError(w http.ResponseWriter, err error, requestID string) {
	message := err.Error()
	switch {
	case errors.Is(err, recommendation.ErrNotFound):
		writeErr(w, 404, "RESOURCE_NOT_FOUND", "Record not found.", requestID)
	case errors.Is(err, recommendation.ErrKillSwitchOn):
		writeErr(w, 409, "KILL_SWITCH_ACTIVE", "Price publishing is disabled by the kill switch.", requestID)
	case errors.Is(err, recommendation.ErrStale):
		writeErr(w, 409, "RECOMMENDATION_STALE", "Recommendation expired. Recompute before acting.", requestID)
	case errors.Is(err, recommendation.ErrNotActionable):
		writeErr(w, 422, "NOT_ACTIONABLE", "This recommendation state cannot be submitted for approval.", requestID)
	case errors.Is(err, recommendation.ErrBelowFloor):
		writeErr(w, 422, "BELOW_FLOOR", "Requested price is below the minimum profitable price.", requestID)
	case errors.Is(err, recommendation.ErrAboveCeiling):
		writeErr(w, 422, "ABOVE_CEILING", "Requested price exceeds the configured maximum price.", requestID)
	case errors.Is(err, recommendation.ErrRoleLimit):
		writeErr(w, 403, "APPROVAL_LIMIT_EXCEEDED", "This change exceeds your role approval limit.", requestID)
	case errors.Is(err, recommendation.ErrActiveExecution):
		writeErr(w, 409, "CONFLICT", "An active publish already exists for this request.", requestID)
	case strings.Contains(message, "already in progress"):
		writeErr(w, 409, "CONFLICT", message, requestID)
	case strings.Contains(message, "not awaiting approval"), strings.Contains(message, "only an approved"):
		writeErr(w, 409, "INVALID_STATE", message, requestID)
	case strings.Contains(message, "sale price"), strings.Contains(message, "changed externally"):
		writeErr(w, 409, "CONFLICT", message, requestID)
	default:
		writeErr(w, 400, "VALIDATION_ERROR", message, requestID)
	}
}
