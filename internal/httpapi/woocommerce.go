package httpapi

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"automation/internal/audit"
	"automation/internal/db"
	"automation/internal/woocommerce"
)

type wooConnectRequest struct {
	StoreName      string `json:"store_name"`
	StoreURL       string `json:"store_url"`
	ConsumerKey    string `json:"consumer_key"`
	ConsumerSecret string `json:"consumer_secret"`
}

func ConnectWooCommerce(service *woocommerce.Service, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req wooConnectRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		conn, runID, err := service.Connect(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), woocommerce.ConnectInput{
			StoreName: req.StoreName, StoreURL: req.StoreURL, ConsumerKey: req.ConsumerKey, ConsumerSecret: req.ConsumerSecret,
		})
		if err != nil {
			writeIntegrationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 201, map[string]any{"connection": conn, "sync_run_id": runID})
	}
}

func WooCommerceStatus(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h, err := service.Status(r.Context(), OrgIDFromContext(r.Context()))
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load connector status.", RequestIDFromContext(r.Context()))
			return
		}
		if h == nil || h.Connection.ID == "" {
			writeJSON(w, 200, map[string]any{"connected": false})
			return
		}
		writeJSON(w, 200, map[string]any{"connected": h.Connection.Status == "connected", "health": h})
	}
}

func TestWooCommerce(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := service.TestConnection(r.Context(), OrgIDFromContext(r.Context())); err != nil {
			writeIntegrationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "message": "WooCommerce connection is healthy."})
	}
}

func DisconnectWooCommerce(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := OrgIDFromContext(r.Context())
		h, err := service.Status(r.Context(), orgID)
		if err != nil || h == nil || h.Connection.ID == "" {
			writeErr(w, 404, "RESOURCE_NOT_FOUND", "No WooCommerce connection found.", RequestIDFromContext(r.Context()))
			return
		}
		if err := service.Disconnect(r.Context(), orgID, h.Connection.ID, "Disconnected by store owner"); err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not disconnect store.", RequestIDFromContext(r.Context()))
			return
		}
		_ = db.WithTenant(r.Context(), service.DB, orgID, func(tx *sql.Tx) error {
			return audit.Append(r.Context(), tx, audit.Event{OrgID: orgID, ActorUserID: UserIDFromContext(r.Context()), Action: "woocommerce.disconnected", ResourceType: "store_connection", ResourceID: h.Connection.ID, RequestID: RequestIDFromContext(r.Context())})
		})
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

func StartWooSync(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := OrgIDFromContext(r.Context())
		h, err := service.Status(r.Context(), orgID)
		if err != nil || h == nil || h.Connection.StoreID == "" {
			writeErr(w, 404, "RESOURCE_NOT_FOUND", "Connect WooCommerce before syncing.", RequestIDFromContext(r.Context()))
			return
		}
		runID, err := service.EnqueueSync(r.Context(), orgID, h.Connection.StoreID, "reconcile", UserIDFromContext(r.Context()))
		if err != nil {
			writeIntegrationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 202, map[string]any{"sync_run_id": runID, "status": "queued"})
	}
}

func ListWooSyncRuns(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		runs, err := service.ListSyncRuns(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("store_id"), limit)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load sync history.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"runs": runs})
	}
}

func GetWooSyncRun(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		run, items, err := service.GetSyncRun(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			writeErr(w, 404, "RESOURCE_NOT_FOUND", "Sync run not found.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"run": run, "items": items})
	}
}

func RetryWooSync(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runID, err := service.RetrySync(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"), UserIDFromContext(r.Context()))
		if err != nil {
			writeIntegrationError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 202, map[string]any{"sync_run_id": runID, "status": "queued"})
	}
}

func ListWooProducts(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit < 1 || limit > 100 {
			limit = 25
		}
		page, err := service.ListProducts(r.Context(), OrgIDFromContext(r.Context()), r.URL.Query().Get("q"), r.URL.Query().Get("status"), r.URL.Query().Get("after"), limit)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load products.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, page)
	}
}

func GetWooProduct(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := service.GetProduct(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			writeErr(w, 404, "RESOURCE_NOT_FOUND", "Product not found.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, p)
	}
}

func DownloadWooImportReport(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		run, items, err := service.GetSyncRun(r.Context(), OrgIDFromContext(r.Context()), r.PathValue("id"))
		if err != nil {
			writeErr(w, 404, "RESOURCE_NOT_FOUND", "Sync run not found.", RequestIDFromContext(r.Context()))
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="woocommerce-sync-%s.csv"`, run.ID))
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"external_id", "sku", "status", "error"})
		for _, item := range items {
			_ = cw.Write([]string{item.ExternalID, item.SKU, item.Status, item.Error})
		}
		cw.Flush()
	}
}

func ReceiveWooWebhook(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			writeErr(w, 413, "VALIDATION_ERROR", "Webhook body is too large.", RequestIDFromContext(r.Context()))
			return
		}
		result, err := service.ReceiveWebhook(r.Context(), r.Header.Get("X-WC-Webhook-Source"), r.Header.Get("X-WC-Webhook-Signature"), r.Header.Get("X-WC-Webhook-ID"), r.Header.Get("X-WC-Webhook-Topic"), body)
		if err != nil {
			writeErr(w, 401, "INTEGRATION_AUTH_FAILED", err.Error(), RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 202, result)
	}
}

func AdminWooDiagnostics(service *woocommerce.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		rows, err := service.AdminDiagnostics(r.Context(), limit)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load connector diagnostics.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"connections": rows})
	}
}

func writeIntegrationError(w http.ResponseWriter, err error, requestID string) {
	msg := err.Error()
	switch msg {
	case "SYNC_IN_PROGRESS":
		writeErr(w, 409, "SYNC_IN_PROGRESS", "A catalog sync is already in progress.", requestID)
	case "not-found":
		writeErr(w, 404, "RESOURCE_NOT_FOUND", "WooCommerce connection not found.", requestID)
	default:
		if strings.Contains(msg, "API key") || strings.Contains(msg, "credentials") || strings.Contains(msg, "reconnect") {
			writeErr(w, 422, "INTEGRATION_AUTH_FAILED", msg, requestID)
		} else if strings.Contains(msg, "rate limit") {
			writeErr(w, 429, "RATE_LIMITED", msg, requestID)
		} else if strings.Contains(msg, "could not reach") || strings.Contains(msg, "connection failed") {
			writeErr(w, 502, "INTEGRATION_UNAVAILABLE", msg, requestID)
		} else {
			writeErr(w, 400, "VALIDATION_ERROR", msg, requestID)
		}
	}
}
