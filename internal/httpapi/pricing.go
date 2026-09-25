package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"automation/internal/pricing"
)

func SetProductCost(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input pricing.CostInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		input.ProductID = r.PathValue("id")
		cost, err := service.SetCost(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input)
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		if result, evalErr := service.Evaluate(r.Context(), OrgIDFromContext(r.Context()), input.ProductID, input.VariantID); evalErr == nil {
			_ = service.RecordImpact(r.Context(), OrgIDFromContext(r.Context()), input.ProductID, input.VariantID, result)
		}
		writeJSON(w, 200, map[string]any{"cost": cost})
	}
}

func ListProductCostHistory(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		versions, err := service.ListCostVersions(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"), r.URL.Query().Get("variant_id"), limit)
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"versions": versions})
	}
}

func GetProductCost(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cost, err := service.GetCost(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"), r.URL.Query().Get("variant_id"))
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, cost)
	}
}

func ListProductCosts(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		rows, err := service.ListCosts(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("q"), limit)
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"costs": rows})
	}
}

func EvaluateProductPrice(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := service.Evaluate(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"), r.URL.Query().Get("variant_id"))
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, result)
	}
}

func ImportProductCosts(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
		file, header, err := r.FormFile("file")
		if err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Attach a CSV file in the file field.", RequestIDFromContext(r.Context()))
			return
		}
		defer file.Close()
		if header.Size > 5<<20 {
			writeErr(w, 413, "VALIDATION_ERROR", "CSV must be 5MB or smaller.", RequestIDFromContext(r.Context()))
			return
		}
		if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
			writeErr(w, 400, "VALIDATION_ERROR", "Only .csv files are accepted.", RequestIDFromContext(r.Context()))
			return
		}
		result, err := service.ImportCostsCSV(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), file)
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, result)
	}
}

func CreatePricingRule(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input pricing.RuleInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		rule, err := service.CreateRule(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input)
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 201, rule)
	}
}

func ListPricingRules(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rules, err := service.ListRules(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("active") == "true")
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"rules": rules})
	}
}

func UpdatePricingRule(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input pricing.RuleInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		rule, err := service.UpdateRule(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"), input)
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, rule)
	}
}

func DeletePricingRule(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := service.DeleteRule(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id")); err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]bool{"deleted": true})
	}
}

func SimulatePricingRules(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ProductIDs []string      `json:"product_ids"`
			VariantID  string        `json:"variant_id,omitempty"`
			Rule       *pricing.Rule `json:"rule,omitempty"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&input); err != nil || len(input.ProductIDs) == 0 || len(input.ProductIDs) > 100 {
			writeErr(w, 400, "VALIDATION_ERROR", "Provide 1-100 product_ids.", RequestIDFromContext(r.Context()))
			return
		}
		results := make([]any, 0, len(input.ProductIDs))
		for _, id := range input.ProductIDs {
			var result pricing.Result
			var err error
			if input.Rule != nil {
				result, err = service.SimulateDraft(r.Context(), OrgIDFromContext(r.Context()), id, input.VariantID, *input.Rule)
			} else {
				result, err = service.Evaluate(r.Context(), OrgIDFromContext(r.Context()), id, input.VariantID)
			}
			if err != nil {
				results = append(results, map[string]any{"product_id": id, "status": "error", "reason": err.Error()})
				continue
			}
			results = append(results, result)
		}
		writeJSON(w, 200, map[string]any{"results": results})
	}
}

func UpdatePricePolicy(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input pricing.PolicyInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		policy, err := service.UpsertPolicy(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input)
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, policy)
	}
}

func UpdatePriceLock(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			VariantID string `json:"variant_id,omitempty"`
			Locked    bool   `json:"locked"`
			Reason    string `json:"reason"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&input); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		lock, err := service.SetLock(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), r.PathValue("id"), input.VariantID, input.Locked, input.Reason)
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, lock)
	}
}

func ListCostImpactAlerts(service *pricing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alerts, err := service.ListImpactAlerts(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("unacknowledged") == "true")
		if err != nil {
			writePricingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"alerts": alerts})
	}
}

func writePricingError(w http.ResponseWriter, err error, requestID string) {
	switch {
	case errors.Is(err, pricing.ErrNotFound):
		writeErr(w, 404, "RESOURCE_NOT_FOUND", "Pricing record not found.", requestID)
	case errors.Is(err, pricing.ErrMissingCost):
		writeErr(w, 422, "VALIDATION_ERROR", "Cost is required before calculating a price floor.", requestID)
	case errors.Is(err, pricing.ErrInvalidMargin):
		writeErr(w, 400, "VALIDATION_ERROR", err.Error(), requestID)
	case errors.Is(err, pricing.ErrPriceGuardrail), errors.Is(err, pricing.ErrFloorAboveCeiling):
		writeErr(w, 422, "PRICE_GUARDRAIL_VIOLATION", err.Error(), requestID)
	default:
		writeErr(w, 400, "VALIDATION_ERROR", err.Error(), requestID)
	}
}
