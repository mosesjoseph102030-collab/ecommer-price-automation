package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"automation/internal/billing"
	"automation/internal/reliability"
)

func writeBillingError(w http.ResponseWriter, err error, rid string) {
	message := err.Error()
	switch {
	case errors.Is(err, billing.ErrReadOnly):
		writeErr(w, 402, "STORE_READ_ONLY", "This store is read-only because payment failed and the grace period has ended. Your data is retained; update payment to restore access.", rid)
	case errors.Is(err, billing.ErrLimitExceeded):
		writeErr(w, 402, "PLAN_LIMIT_EXCEEDED", message, rid)
	case errors.Is(err, billing.ErrNoSubscription):
		writeErr(w, 404, "NO_SUBSCRIPTION", "This store has no subscription yet.", rid)
	case errors.Is(err, billing.ErrInvalidPlan):
		writeErr(w, 400, "INVALID_PLAN", "Unknown or inactive plan.", rid)
	case errors.Is(err, billing.ErrSignatureInvalid):
		writeErr(w, 401, "INVALID_SIGNATURE", "Webhook signature verification failed.", rid)
	case errors.Is(err, billing.ErrUnavailable):
		writeErr(w, 502, "PAYMENT_PROVIDER_UNAVAILABLE", "The payment provider is unavailable. Try again shortly.", rid)
	case errors.Is(err, billing.ErrNotFound):
		writeErr(w, 404, "NOT_FOUND", "Payment record not found.", rid)
	default:
		writeErr(w, 400, "VALIDATION_ERROR", message, rid)
	}
}

func ListPlans(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		plans, err := svc.ListPlans(r.Context())
		if err != nil {
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"plans": plans})
	}
}

func BillingStatus(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := svc.Status(r.Context(), OrgIDFromContext(r.Context()))
		if err != nil {
			if errors.Is(err, billing.ErrNoSubscription) {
				// A store created before billing existed is not bricked: report a
				// free trial posture instead of an error.
				writeJSON(w, 200, map[string]any{"subscription": nil, "entitlements": map[string]int64{},
					"usage": map[string]int64{}, "read_only": false})
				return
			}
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, status)
	}
}

// StartCheckout begins a Paystack transaction for a plan.
//
// This handler deliberately does NOT call GuardWrite. A tenant becomes read-only
// because payment failed, so the one action that must remain available is paying.
// GuardBillingWrite exempts /billing/* for the same reason; a guard here would
// have locked a lapsed tenant out of ever restoring its own access.
func StartCheckout(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PlanCode string `json:"plan_code"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		if input.PlanCode == "" {
			writeErr(w, 400, "VALIDATION_ERROR", "plan_code is required.", RequestIDFromContext(r.Context()))
			return
		}
		email := EmailFromContext(r.Context())
		session, err := svc.StartCheckout(r.Context(), OrgIDFromContext(r.Context()), UserIDFromContext(r.Context()), input.PlanCode, email)
		if err != nil {
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 201, session)
	}
}

// ConfirmCheckout verifies a payment server-side. The browser callback is never
// trusted on its own.
func ConfirmCheckout(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Reference string `json:"reference"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		sub, err := svc.ConfirmPayment(r.Context(), OrgIDFromContext(r.Context()), input.Reference)
		if err != nil {
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, sub)
	}
}

// PaystackWebhook handles the provider callback. It reads the raw body because
// the signature is computed over the exact bytes Paystack sent.
func PaystackWebhook(svc *billing.Service, secretKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Bound the body before reading; Paystack payloads are small.
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Could not read the webhook body.", RequestIDFromContext(r.Context()))
			return
		}
		signature := r.Header.Get("x-paystack-signature")
		if _, err := svc.ProcessWebhook(r.Context(), secretKey, raw, signature); err != nil {
			// A bad signature is an authentication failure, not a processing error.
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]bool{"received": true})
	}
}

func ListPayments(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := svc.ListPayments(r.Context(), OrgIDFromContext(r.Context()), 0)
		if err != nil {
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"payments": rows})
	}
}

func CancelSubscription(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Reason string `json:"reason"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		if err := svc.Cancel(r.Context(), OrgIDFromContext(r.Context()), input.Reason); err != nil {
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{
			"cancelled":          true,
			"data_retention_end": "business data is retained; nothing is deleted",
		})
	}
}

func ReactivateSubscription(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := svc.Reactivate(r.Context(), OrgIDFromContext(r.Context())); err != nil {
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]bool{"reactivated": true})
	}
}

// ------------------------------------------------------------------ read-only

// readOnlyExemptSuffixes are routes a read-only store must still reach.
//
// This is the difference between read-only mode being a safety mechanism and it
// being a trap. A store in read-only mode exists precisely because payment
// failed, so it must be able to run checkout and confirm the payment that fixes
// it. Likewise, switching publishing or AI off must always work: a tenant losing
// the ability to stop a bad automation is worse than the automation itself.
var readOnlyExemptSuffixes = []string{
	"/billing", "/billing/checkout", "/billing/confirm", "/billing/cancel",
	"/billing/reactivate", "/billing/payments",
	"/pricing-kill-switch", "/ai/kill-switch",
}

// mutatingMethods are the requests a read-only store may not perform. Reads and
// exports stay available so a lapsed tenant keeps its visibility and its data.
var mutatingMethods = map[string]bool{
	http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// GuardBillingWrite blocks business writes for a read-only store.
//
// It runs outside the per-route handler chains, so it is applied once rather
// than to thirty routes, which is why it reads the slug from the path instead of
// the request context. It is deliberately method-based: a GET to the same route
// is unaffected, so a lapsed tenant can still read, export, and inspect.
func GuardBillingWrite(svc *billing.Service, database *sql.DB, next http.Handler) http.Handler {
	if svc == nil || database == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mutatingMethods[r.Method] {
			next.ServeHTTP(w, r)
			return
		}
		// /api/v1/app/{slug}/... is the only tenant-scoped shape. Platform admin
		// and community routes are not billed per store and are left alone.
		slugVal, ok := tenantSlugFromPath(r.URL.Path)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		// The billing endpoints themselves are always reachable, otherwise a
		// lapsed tenant could not pay and would stay locked out forever.
		for _, suffix := range readOnlyExemptSuffixes {
			if strings.HasSuffix(r.URL.Path, suffix) {
				next.ServeHTTP(w, r)
				return
			}
		}
		orgID, err := resolveOrgID(r.Context(), database, slugVal)
		if err != nil {
			// Let the route's own resolution produce the 404 rather than
			// guessing a tenant here.
			next.ServeHTTP(w, r)
			return
		}
		if err := svc.GuardWrite(r.Context(), orgID); err != nil {
			writeBillingError(w, err, RequestIDFromContext(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tenantSlugFromPath extracts {slug} from /api/v1/app/{slug}/... . It reports
// false for any other path shape, including /api/v1/app with no slug.
func tenantSlugFromPath(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "app" {
		return "", false
	}
	if parts[3] == "" {
		return "", false
	}
	return parts[3], true
}

// ---------------------------------------------------------------- operations

// ListDeadLetters shows jobs that exhausted their retries. Tenants see only
// their own; the platform role sees everything.
func ListDeadLetters(dead *reliability.DeadLetter, platform bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if dead == nil {
			writeErr(w, 503, "NOT_CONFIGURED", "The dead-letter queue is not configured.", RequestIDFromContext(r.Context()))
			return
		}
		orgID := ""
		if !platform {
			orgID = OrgIDFromContext(r.Context())
		}
		rows, err := dead.List(r.Context(), orgID, 100)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load the dead-letter queue.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"dead_letters": rows})
	}
}

// ResolveDeadLetter closes a dead-letter entry after an operator has replayed or
// consciously discarded it. The resolution text is kept so the decision is
// auditable.
func ResolveDeadLetter(dead *reliability.DeadLetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if dead == nil {
			writeErr(w, 503, "NOT_CONFIGURED", "The dead-letter queue is not configured.", RequestIDFromContext(r.Context()))
			return
		}
		var input struct {
			Resolution string `json:"resolution"`
		}
		if !decodeJSON(w, r, 4<<10, &input) {
			return
		}
		if strings.TrimSpace(input.Resolution) == "" {
			writeErr(w, 400, "VALIDATION_ERROR", "A resolution note is required.", RequestIDFromContext(r.Context()))
			return
		}
		if err := dead.Resolve(r.Context(), r.PathValue("id"), input.Resolution); err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not resolve the dead-letter entry.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]bool{"resolved": true})
	}
}

// QueueBacklog reports per-queue depth. This is the endpoint an on-call engineer
// looks at first when publishing has stopped.
func QueueBacklog(monitor *reliability.QueueMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if monitor == nil {
			writeErr(w, 503, "NOT_CONFIGURED", "Queue monitoring is not configured.", RequestIDFromContext(r.Context()))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		writeJSON(w, 200, map[string]any{"queues": monitor.Snapshot(ctx)})
	}
}

// StatusPage is the public, unauthenticated incident feed. It deliberately
// exposes only severity, status, and the public note: no tenant identifiers, no
// internal detail, no stack traces.
func StatusPage(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if database == nil {
			writeJSON(w, 200, map[string]any{"status": "operational", "incidents": []any{}})
			return
		}
		rows, err := database.QueryContext(r.Context(),
			`SELECT severity, status, title, public_note, started_at, resolved_at
			 FROM incidents WHERE status <> 'resolved' OR resolved_at > now() - interval '7 days'
			 ORDER BY started_at DESC LIMIT 25`)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load incidents.", RequestIDFromContext(r.Context()))
			return
		}
		defer rows.Close()
		type incident struct {
			Severity   string     `json:"severity"`
			Status     string     `json:"status"`
			Title      string     `json:"title"`
			PublicNote string     `json:"public_note"`
			StartedAt  time.Time  `json:"started_at"`
			ResolvedAt *time.Time `json:"resolved_at,omitempty"`
		}
		incidents := []incident{}
		for rows.Next() {
			var item incident
			var resolved sql.NullTime
			if err := rows.Scan(&item.Severity, &item.Status, &item.Title, &item.PublicNote, &item.StartedAt, &resolved); err != nil {
				continue
			}
			if resolved.Valid {
				item.ResolvedAt = &resolved.Time
			}
			incidents = append(incidents, item)
		}
		overall := "operational"
		for _, item := range incidents {
			if item.Status != "resolved" && (item.Severity == "major" || item.Severity == "critical") {
				overall = "degraded"
				break
			}
		}
		writeJSON(w, 200, map[string]any{"status": overall, "incidents": incidents})
	}
}

// CreateIncident opens an incident. Platform-only, and the public note is
// separate from the internal detail so an operator cannot leak internals to the
// public feed by writing one field.
func CreateIncident(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Severity   string `json:"severity"`
			Title      string `json:"title"`
			Detail     string `json:"detail"`
			PublicNote string `json:"public_note"`
		}
		if !decodeJSON(w, r, 8<<10, &input) {
			return
		}
		switch input.Severity {
		case "info", "warning", "major", "critical":
		default:
			writeErr(w, 400, "VALIDATION_ERROR", "severity must be info, warning, major, or critical.", RequestIDFromContext(r.Context()))
			return
		}
		if strings.TrimSpace(input.Title) == "" {
			writeErr(w, 400, "VALIDATION_ERROR", "An incident title is required.", RequestIDFromContext(r.Context()))
			return
		}
		var id string
		err := database.QueryRowContext(r.Context(), `INSERT INTO incidents (severity, status, title, detail, public_note)
			VALUES ($1,'open',$2,$3,$4) RETURNING id::text`,
			input.Severity, input.Title, input.Detail, input.PublicNote).Scan(&id)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not open the incident.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 201, map[string]any{"incident_id": id, "status": "open"})
	}
}

// ResolveIncident closes an incident and stamps the public note.
func ResolveIncident(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PublicNote string `json:"public_note"`
		}
		if !decodeJSON(w, r, 8<<10, &input) {
			return
		}
		result, err := database.ExecContext(r.Context(), `UPDATE incidents SET status='resolved', resolved_at=now(),
			public_note=COALESCE(NULLIF($2,''), public_note) WHERE id::text=$1 AND status<>'resolved'`,
			r.PathValue("id"), input.PublicNote)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not resolve the incident.", RequestIDFromContext(r.Context()))
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			writeErr(w, 404, "NOT_FOUND", "No open incident with that id.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]bool{"resolved": true})
	}
}

// ListCircuitBreakers reports breaker state. Persisted state is read from the
// database so the view survives the process that set it.
func ListCircuitBreakers(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.QueryContext(r.Context(),
			`SELECT name, state, consecutive_failures, opened_at, open_until, last_error, updated_at
			 FROM circuit_breaker_state ORDER BY name`)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load circuit breaker state.", RequestIDFromContext(r.Context()))
			return
		}
		defer rows.Close()
		type breaker struct {
			Name      string     `json:"name"`
			State     string     `json:"state"`
			Failures  int        `json:"consecutive_failures"`
			OpenedAt  *time.Time `json:"opened_at,omitempty"`
			OpenUntil *time.Time `json:"open_until,omitempty"`
			LastError string     `json:"last_error,omitempty"`
			UpdatedAt time.Time  `json:"updated_at"`
		}
		out := []breaker{}
		for rows.Next() {
			var item breaker
			var opened, until sql.NullTime
			if err := rows.Scan(&item.Name, &item.State, &item.Failures, &opened, &until, &item.LastError, &item.UpdatedAt); err != nil {
				continue
			}
			if opened.Valid {
				item.OpenedAt = &opened.Time
			}
			if until.Valid {
				item.OpenUntil = &until.Time
			}
			out = append(out, item)
		}
		writeJSON(w, 200, map[string]any{"breakers": out})
	}
}

// ResetCircuitBreaker clears a tripped breaker. The route exists because an
// operator who has just fixed a dependency must not have to wait out a cooldown,
// but it is platform-only for the same reason.
func ResetCircuitBreaker(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := database.ExecContext(r.Context(),
			`UPDATE circuit_breaker_state SET state='closed', consecutive_failures=0,
			 opened_at=NULL, open_until=NULL, last_error='', updated_at=now() WHERE name=$1`,
			r.PathValue("name"))
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not reset the breaker.", RequestIDFromContext(r.Context()))
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			writeErr(w, 404, "NOT_FOUND", "No circuit breaker with that name.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"reset": true})
	}
}

// ListRetentionRuns shows when retention last ran and what it removed, so a
// retention policy is observable rather than assumed.
func ListRetentionRuns(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.QueryContext(r.Context(),
			`SELECT job_name, rows_affected, detail, started_at, finished_at
			 FROM retention_runs ORDER BY started_at DESC LIMIT 50`)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load retention runs.", RequestIDFromContext(r.Context()))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var job, detail string
			var affected int
			var started time.Time
			var finished sql.NullTime
			if err := rows.Scan(&job, &affected, &detail, &started, &finished); err != nil {
				continue
			}
			row := map[string]any{"job_name": job, "rows_affected": affected, "detail": detail, "started_at": started}
			if finished.Valid {
				row["finished_at"] = finished.Time
			}
			out = append(out, row)
		}
		writeJSON(w, 200, map[string]any{"runs": out})
	}
}
