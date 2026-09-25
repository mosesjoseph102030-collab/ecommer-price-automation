package httpapi

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"automation/internal/audit"
	automationdb "automation/internal/db"
	"automation/internal/themes"
)

// UpdateTheme handles PATCH /api/v1/app/{slug}/theme: owner picks a curated theme.
func UpdateTheme(db *sql.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(orgKey).(string)
		var req struct {
			ThemeID string `json:"theme_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		theme, err := themes.Get(req.ThemeID)
		if err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", err.Error()+".", RequestIDFromContext(r.Context()))
			return
		}
		// Contrast gate: primary-on-white must meet 3:1 minimum for large text.
		if ratio, err := themes.ContrastRatio(theme.Primary, "#FFFFFF"); err != nil || ratio < 3.0 {
			writeErr(w, 400, "VALIDATION_ERROR", "Theme fails contrast requirements.", RequestIDFromContext(r.Context()))
			return
		}
		err = automationdb.WithTenant(r.Context(), db, orgID, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(r.Context(),
				`INSERT INTO organization_theme_settings (organization_id, theme_id) VALUES ($1,$2)
				 ON CONFLICT (organization_id) DO UPDATE SET theme_id=$2, updated_at=now()`, orgID, theme.ID); e != nil {
				return e
			}
			return audit.Append(r.Context(), tx, audit.Event{
				OrgID: orgID, ActorUserID: UserIDFromContext(r.Context()), Action: "theme.updated",
				ResourceType: "organization", ResourceID: orgID, RequestID: RequestIDFromContext(r.Context()),
				NewState: map[string]string{"theme_id": theme.ID},
			})
		})
		if err != nil {
			logger.Error("theme_update", "error", err)
			writeErr(w, 500, "INTERNAL_ERROR", "Could not save theme. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, map[string]any{"theme": theme})
	}
}

// ListAudit handles GET /api/v1/app/{slug}/audit-logs (permission-gated).
func ListAudit(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(orgKey).(string)
		out := []map[string]any{}
		err := automationdb.WithTenant(r.Context(), db, orgID, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(r.Context(),
				`SELECT action, resource_type, resource_id, request_id, occurred_at FROM audit_events
				 WHERE organization_id=$1 ORDER BY occurred_at DESC LIMIT 50`, orgID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var action, rtype, rid, reqID string
				var at string
				if err := rows.Scan(&action, &rtype, &rid, &reqID, &at); err != nil {
					return err
				}
				out = append(out, map[string]any{
					"action": action, "resource_type": rtype, "resource_id": rid,
					"request_id": reqID, "occurred_at": at,
				})
			}
			return rows.Err()
		})
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not load audit logs.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 200, out)
	}
}

// InviteMember handles POST /api/v1/app/{slug}/team/invite (owner/manager only via route wiring).
func InviteMember(db *sql.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, _ := r.Context().Value(orgKey).(string)
		var req struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		if !strings.Contains(req.Email, "@") {
			writeErr(w, 400, "VALIDATION_ERROR", "Enter a valid email address.", RequestIDFromContext(r.Context()))
			return
		}
		switch req.Role {
		case "pricing_manager", "analyst", "staff", "viewer":
		default:
			writeErr(w, 400, "VALIDATION_ERROR", "Role must be pricing_manager, analyst, staff, or viewer.", RequestIDFromContext(r.Context()))
			return
		}
		var invitedUserID string
		err := db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE email=$1`, req.Email).Scan(&invitedUserID)
		if err != nil {
			// Invite-only: create a placeholder membership row keyed by email via audit note.
			// Phase 1 keeps it simple: require the user to sign up first.
			writeErr(w, 404, "RESOURCE_NOT_FOUND", "No account with this email yet. Ask them to sign up first.", RequestIDFromContext(r.Context()))
			return
		}
		err = automationdb.WithTenant(r.Context(), db, orgID, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(r.Context(),
				`INSERT INTO memberships (user_id, organization_id, status) VALUES ($1,$2,'active')
				 ON CONFLICT (user_id, organization_id) DO UPDATE SET status='active', updated_at=now()`,
				invitedUserID, orgID); e != nil {
				return e
			}
			if _, e := tx.ExecContext(r.Context(),
				`INSERT INTO member_role_assignments (user_id, organization_id, role_id, granted_by_user_id)
				 VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
				invitedUserID, orgID, req.Role, UserIDFromContext(r.Context())); e != nil {
				return e
			}
			return audit.Append(r.Context(), tx, audit.Event{
				OrgID: orgID, ActorUserID: UserIDFromContext(r.Context()), Action: "team.invited",
				ResourceType: "membership", ResourceID: invitedUserID, RequestID: RequestIDFromContext(r.Context()),
				NewState: map[string]string{"role": req.Role, "email": req.Email},
			})
		})
		if err != nil {
			logger.Error("invite_member", "error", err)
			writeErr(w, 500, "INTERNAL_ERROR", "Could not invite. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		writeJSON(w, 201, map[string]any{"user_id": invitedUserID, "role": req.Role})
	}
}
