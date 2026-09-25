package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"automation/internal/audit"
	"automation/internal/billing"
	automationdb "automation/internal/db"
	"automation/internal/permissions"
	"automation/internal/slug"
	"automation/internal/tenancy"
	"automation/internal/themes"
)

// CreateOrg handles POST /api/v1/orgs: name -> slug -> org + owner membership + defaults.
// Idempotent via Idempotency-Key header for safe retries.
//
// billingSvc is optional; when set, a new store is enrolled on its plan's trial
// so its entitlements resolve immediately rather than on first billing call.
func CreateOrg(db *sql.DB, logger *slog.Logger, billingSvc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := UserIDFromContext(r.Context())
		var req struct {
			Name             string `json:"name"`
			BusinessCategory string `json:"business_category"`
			Currency         string `json:"currency"`
			Timezone         string `json:"timezone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			writeErr(w, 400, "VALIDATION_ERROR", "Store name is required.", RequestIDFromContext(r.Context()))
			return
		}
		base := slug.Generate(name)
		if base == "" {
			base = "store"
		}
		if err := slug.Validate(base); err != nil {
			// Reserved word: append suffix rather than failing hard.
			base += "-store"
			if err := slug.Validate(base); err != nil {
				writeErr(w, 400, "VALIDATION_ERROR", "Choose a different store name.", RequestIDFromContext(r.Context()))
				return
			}
		}
		cur := strings.ToUpper(strings.TrimSpace(req.Currency))
		if cur == "" {
			cur = "NGN"
		}
		if len(cur) != 3 {
			writeErr(w, 400, "VALIDATION_ERROR", "Currency must be a 3-letter code.", RequestIDFromContext(r.Context()))
			return
		}
		tz := strings.TrimSpace(req.Timezone)
		if tz == "" {
			tz = "Africa/Lagos"
		}

		// Idempotency: return the original response for a repeated key.
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			var code int
			var body json.RawMessage
			err := db.QueryRowContext(r.Context(),
				`SELECT status_code, response_body FROM api_idempotency_keys WHERE key=$1`, key).Scan(&code, &body)
			if err == nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(code)
				_, _ = w.Write(body)
				return
			}
		}

		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create store. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		defer tx.Rollback()

		finalSlug := base
		var orgID string
		for i := 0; i < 5; i++ {
			err = tx.QueryRowContext(r.Context(),
				`INSERT INTO organizations (name, slug, business_category, currency, timezone, created_by_user_id)
				 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
				name, finalSlug, req.BusinessCategory, cur, tz, uid).Scan(&orgID)
			if err == nil {
				break
			}
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				finalSlug = base + "-2"
				if i > 0 {
					finalSlug = base + "-" + itoa(i+2)
				}
				continue
			}
			logger.Error("org_insert", "error", err)
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create store. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		if orgID == "" {
			writeErr(w, 409, "CONFLICT", "That store URL is taken. Try another name.", RequestIDFromContext(r.Context()))
			return
		}
		if err := automationdb.SetTenantContext(tx, orgID); err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create store. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO memberships (user_id, organization_id, status) VALUES ($1,$2,'active')`, uid, orgID); err != nil {
			logger.Error("org_membership", "error", err)
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create store. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		for _, role := range permissions.OwnerRoles() {
			if _, err := tx.ExecContext(r.Context(),
				`INSERT INTO member_role_assignments (user_id, organization_id, role_id, granted_by_user_id)
				 VALUES ($1,$2,$3,$4)`, uid, orgID, role, uid); err != nil {
				logger.Error("org_role", "error", err)
				writeErr(w, 500, "INTERNAL_ERROR", "Could not create store. Try again.", RequestIDFromContext(r.Context()))
				return
			}
		}
		_, _ = tx.ExecContext(r.Context(), `INSERT INTO organization_settings (organization_id) VALUES ($1) ON CONFLICT DO NOTHING`, orgID)
		// Seed default per-role approval limits so a new store can approve price
		// changes immediately. Owners get a wider band than pricing managers.
		_, _ = tx.ExecContext(r.Context(), `INSERT INTO approval_limits (organization_id, role_id, maximum_change_bps) VALUES
			($1,'store_owner',3000), ($1,'pricing_manager',1000) ON CONFLICT DO NOTHING`, orgID)
		_, _ = tx.ExecContext(r.Context(), `INSERT INTO organization_onboarding (organization_id, state) VALUES ($1,'created') ON CONFLICT DO NOTHING`, orgID)
		_, _ = tx.ExecContext(r.Context(), `INSERT INTO organization_theme_settings (organization_id, theme_id) VALUES ($1,'teal-ledger') ON CONFLICT DO NOTHING`, orgID)
		if err := audit.Append(r.Context(), tx, audit.Event{
			OrgID: orgID, ActorUserID: uid, Action: "org.created",
			ResourceType: "organization", ResourceID: orgID, RequestID: RequestIDFromContext(r.Context()),
			NewState: map[string]string{"slug": finalSlug, "name": name},
		}); err != nil {
			logger.Error("org_audit", "error", err)
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create store. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		if err := tx.Commit(); err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create store. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		// A new store starts on its plan's trial. This is deliberately after the
		// commit: the subscription is written through the tenant-scoped path, and
		// a store that exists without a subscription is a recoverable state,
		// whereas a store that exists but was rolled back is not.
		//
		// A failure here is logged, not fatal. Refusing to create the store
		// because billing had a bad day would be worse than creating it and
		// letting the dunning worker reconcile it.
		if billingSvc != nil {
			if err := billingSvc.EnsureSubscription(r.Context(), orgID); err != nil {
				logger.Error("org_trial_subscription_failed", "organization_id", orgID, "error", err)
			}
		}
		resp := map[string]any{"id": orgID, "slug": finalSlug, "name": name}
		raw, _ := json.Marshal(resp)
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			_, _ = db.ExecContext(r.Context(),
				`INSERT INTO api_idempotency_keys (key, user_id, status_code, response_body) VALUES ($1,$2,201,$3) ON CONFLICT DO NOTHING`,
				key, uid, string(raw))
		}
		writeJSON(w, 201, resp)
	}
}

// ListOrganizations returns only organizations where the authenticated user has
// an active membership. The privileged lookup is constrained server-side.
func ListOrganizations(membershipDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := UserIDFromContext(r.Context())
		rows, err := membershipDB.QueryContext(r.Context(), `
			SELECT o.id, o.name, o.slug FROM organizations o
			JOIN memberships m ON m.organization_id=o.id
			WHERE m.user_id=$1 AND m.status='active' ORDER BY o.created_at`, uid)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load stores.", RequestIDFromContext(r.Context()))
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, name, storeSlug string
			if err := rows.Scan(&id, &name, &storeSlug); err != nil {
				writeErr(w, 500, "INTERNAL_ERROR", "Could not load stores.", RequestIDFromContext(r.Context()))
				return
			}
			out = append(out, map[string]any{"id": id, "name": name, "slug": storeSlug})
		}
		writeJSON(w, 200, map[string]any{"organizations": out})
	}
}

// resolveOrgID maps a store slug to its organization, honouring slug redirects.
// It is the single implementation of that lookup so the billing guard cannot
// resolve a different tenant than the route it is protecting.
func resolveOrgID(ctx context.Context, db *sql.DB, slugVal string) (string, error) {
	var orgID string
	err := db.QueryRowContext(ctx,
		`SELECT organization_id FROM slug_redirects WHERE old_slug=$1`, slugVal).Scan(&orgID)
	if err != nil {
		err = db.QueryRowContext(ctx,
			`SELECT id FROM organizations WHERE slug=$1`, slugVal).Scan(&orgID)
	}
	return orgID, err
}

// OrgFromSlug resolves /app/{slug} routes: slug -> org, membership check, tenant context.
func OrgFromSlug(db *sql.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slugVal := r.PathValue("slug")
		if slugVal == "" {
			writeErr(w, 400, "VALIDATION_ERROR", "Store URL is required.", RequestIDFromContext(r.Context()))
			return
		}
		orgID, err := resolveOrgID(r.Context(), db, slugVal)
		if err != nil {
			writeErr(w, 404, "RESOURCE_NOT_FOUND", "Store not found.", RequestIDFromContext(r.Context()))
			return
		}
		uid := UserIDFromContext(r.Context())
		roles, err := tenancy.Resolve(r.Context(), db, uid, orgID)
		if err != nil {
			writeErr(w, 403, "TENANT_ACCESS_DENIED", "You do not have access to this store.", RequestIDFromContext(r.Context()))
			return
		}
		ctx := context.WithValue(r.Context(), orgKey, orgID)
		ctx = context.WithValue(ctx, rolesKey, roles)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetOrg returns store profile + theme for the resolved tenant.
func GetOrg(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(orgKey).(string)
		var name, slugVal, cat, cur, tz, themeID string
		err := automationdb.WithTenant(r.Context(), db, orgID, func(tx *sql.Tx) error {
			return tx.QueryRowContext(r.Context(),
				`SELECT o.name, o.slug, o.business_category, o.currency, o.timezone, COALESCE(t.theme_id,'teal-ledger')
				 FROM organizations o LEFT JOIN organization_theme_settings t ON t.organization_id=o.id
				 WHERE o.id=$1`, orgID).Scan(&name, &slugVal, &cat, &cur, &tz, &themeID)
		})
		if err != nil {
			writeErr(w, 404, "RESOURCE_NOT_FOUND", "Store not found.", RequestIDFromContext(r.Context()))
			return
		}
		theme, _ := themes.Get(themeID)
		writeJSON(w, 200, map[string]any{
			"id": orgID, "slug": slugVal, "business_category": cat,
			"currency": cur, "timezone": tz, "theme": theme,
		})
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return "x"
}
