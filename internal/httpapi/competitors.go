package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"automation/internal/competitor"
)

func CreateCompetitor(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input competitor.CompetitorInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		value, err := service.CreateCompetitor(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input)
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 201, value)
	}
}
func ListCompetitors(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListCompetitors(r.Context(), OrgIDFromContext(r.Context()))
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"competitors": values})
	}
}
func AddCompetitorProduct(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input competitor.ProductInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		value, err := service.AddProduct(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input)
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 201, value)
	}
}
func ListCompetitorProducts(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListProducts(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("competitor_id"))
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"products": values})
	}
}
func ReviewCompetitorMatch(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ProductID string `json:"product_id"`
			State     string `json:"state"`
			Note      string `json:"note"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		if err := service.ReviewMatch(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"), input.ProductID, input.State, input.Note); err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]bool{"updated": true})
	}
}
func CompetitorHistory(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		values, err := service.History(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"), limit)
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"observations": values})
	}
}
func RefreshCompetitorProduct(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := service.QueueCheck(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 202, map[string]string{"run_id": id, "status": "queued"})
	}
}
func ListCompetitorAlerts(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.ListAlerts(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("unacknowledged") == "true")
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"alerts": values})
	}
}
func CompetitorHealth(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.Health(r.Context(), OrgIDFromContext(r.Context()))
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"sources": values})
	}
}
func AdminCompetitorHealth(service *competitor.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := service.AdminHealth(r.Context())
		if err != nil {
			writeCompetitorError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"sources": values})
	}
}

func writeCompetitorError(w http.ResponseWriter, err error, requestID string) {
	message := err.Error()
	switch {
	case errors.Is(err, competitor.ErrNotFound):
		writeErr(w, 404, "RESOURCE_NOT_FOUND", "Competitor record not found.", requestID)
	case strings.Contains(message, "allowlist") || strings.Contains(message, "approved source"):
		writeErr(w, 422, "VALIDATION_ERROR", message, requestID)
	case strings.Contains(message, "already queued"):
		writeErr(w, 409, "CONFLICT", message, requestID)
	default:
		writeErr(w, 400, "VALIDATION_ERROR", message, requestID)
	}
}
