package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/auth"
	"automation/internal/permissions"
)

type ctxKey string

const (
	userKey  ctxKey = "user_id"
	emailKey ctxKey = "email"
	reqIDKey ctxKey = "request_id"
	orgKey   ctxKey = "org_id"
	rolesKey ctxKey = "roles"
)

// RequestIDFromContext returns the request ID for audit/error correlation.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(reqIDKey).(string); ok {
		return v
	}
	return ""
}

// UserIDFromContext returns the authenticated user ID.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userKey).(string); ok {
		return v
	}
	return ""
}

// EmailFromContext returns the authenticated user's email. Used for billing so
// the caller never supplies its own billing address.
func EmailFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(emailKey).(string); ok {
		return v
	}
	return ""
}

// OrgIDFromContext returns the server-resolved organization. Browser-provided tenant IDs are ignored.
func OrgIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(orgKey).(string); ok {
		return v
	}
	return ""
}

// RequireAuth validates the Bearer token (session token or JWT) and stores claims.
func RequireAuth(db *sql.DB, jwtSecret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := tokenFromRequest(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Sign in required.", RequestIDFromContext(r.Context()))
			return
		}
		// Prefer JWT; fall back to opaque session token.
		if claims, err := auth.ValidateToken(jwtSecret, token); err == nil {
			ctx := context.WithValue(r.Context(), userKey, claims.UserID)
			ctx = context.WithValue(ctx, emailKey, claims.Email)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		var userID string
		err := db.QueryRow(`SELECT user_id FROM sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > now()`,
			hashToken(token)).Scan(&userID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Session expired. Sign in again.", RequestIDFromContext(r.Context()))
			return
		}
		ctx := context.WithValue(r.Context(), userKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Register handles POST /api/v1/auth/signup: account + profile + verification token.
func Register(db *sql.DB, jwtSecret []byte, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email     string `json:"email"`
			Password  string `json:"password"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		if !strings.Contains(req.Email, "@") || req.Email == "" {
			writeErr(w, 400, "VALIDATION_ERROR", "Enter a valid email address.", RequestIDFromContext(r.Context()))
			return
		}
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", err.Error()+".", RequestIDFromContext(r.Context()))
			return
		}
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create account. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		defer tx.Rollback()
		var userID string
		err = tx.QueryRowContext(r.Context(),
			`INSERT INTO users (email, password_hash) VALUES ($1,$2) RETURNING id`,
			req.Email, hash).Scan(&userID)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				writeErr(w, 409, "CONFLICT", "An account with this email already exists. Try signing in.", RequestIDFromContext(r.Context()))
				return
			}
			logger.Error("signup_insert", "error", err)
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create account. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO user_profiles (user_id, first_name, last_name) VALUES ($1,$2,$3)`,
			userID, strings.TrimSpace(req.FirstName), strings.TrimSpace(req.LastName)); err != nil {
			logger.Error("signup_profile", "error", err)
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create account. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		verifyTok, _ := auth.NewToken()
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO verification_tokens (user_id, token_hash, expires_at) VALUES ($1,$2,now()+interval '24 hours')`,
			userID, hashToken(verifyTok)); err != nil {
			logger.Error("signup_verify", "error", err)
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create account. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		if err := tx.Commit(); err != nil {
			writeErr(w, 500, "INTERNAL_ERROR", "Could not create account. Try again.", RequestIDFromContext(r.Context()))
			return
		}
		_ = audit.Append(r.Context(), db, audit.Event{
			ActorUserID: userID, Action: "user.registered",
			ResourceType: "user", ResourceID: userID, RequestID: RequestIDFromContext(r.Context()),
		})
		token, _ := auth.IssueToken(jwtSecret, userID, req.Email)
		sessionToken, _ := auth.NewToken()
		_, _ = db.ExecContext(r.Context(),
			`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1,$2,now()+interval '24 hours')`,
			userID, hashToken(sessionToken))
		setSessionCookie(w, r, sessionToken, time.Now().Add(auth.SessionLifetime))
		writeJSON(w, 201, map[string]any{"token": token, "user_id": userID, "email": req.Email})
	}
}

// Login handles POST /api/v1/auth/signin with brute-force-safe generic errors.
func Login(db *sql.DB, jwtSecret []byte, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "VALIDATION_ERROR", "Invalid request body.", RequestIDFromContext(r.Context()))
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		var id, hash string
		err := db.QueryRowContext(r.Context(),
			`SELECT id, password_hash FROM users WHERE email=$1`, req.Email).Scan(&id, &hash)
		if err != nil || !auth.CheckPassword(req.Password, hash) {
			// Generic: no account enumeration.
			writeErr(w, 401, "AUTHENTICATION_REQUIRED", "Email or password is incorrect.", RequestIDFromContext(r.Context()))
			return
		}
		sessTok, _ := auth.NewToken()
		if _, err := db.ExecContext(r.Context(),
			`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1,$2,now()+interval '24 hours')`,
			id, hashToken(sessTok)); err != nil {
			logger.Error("login_session", "error", err)
		}
		token, _ := auth.IssueToken(jwtSecret, id, req.Email)
		setSessionCookie(w, r, sessTok, time.Now().Add(auth.SessionLifetime))
		_ = audit.Append(r.Context(), db, audit.Event{
			ActorUserID: id, Action: "user.login",
			ResourceType: "user", ResourceID: id, RequestID: RequestIDFromContext(r.Context()),
		})
		writeJSON(w, 200, map[string]any{"token": token, "user_id": id, "email": req.Email})
	}
}

// Logout revokes opaque sessions when present and always clears the browser cookie.
func Logout(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := tokenFromRequest(r)
		if token != "" {
			_, _ = db.ExecContext(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1`, hashToken(token))
		}
		clearSessionCookie(w, r)
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

// Me returns the authenticated profile.
func Me(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := UserIDFromContext(r.Context())
		var email, first, last string
		_ = db.QueryRowContext(r.Context(),
			`SELECT u.email, COALESCE(p.first_name,''), COALESCE(p.last_name,'') FROM users u
			 LEFT JOIN user_profiles p ON p.user_id=u.id WHERE u.id=$1`, uid).Scan(&email, &first, &last)
		writeJSON(w, 200, map[string]any{"user_id": uid, "email": email, "first_name": first, "last_name": last})
	}
}

// RequirePermission gates a handler on one store permission (server-side, every action).
func RequirePermission(db *sql.DB, perm string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roles, _ := r.Context().Value(rolesKey).([]string)
		if !permissions.Has(roles, perm) {
			writeErr(w, 403, "PERMISSION_DENIED", "You do not have access. Contact your store owner.", RequestIDFromContext(r.Context()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), reqIDKey, id)))
	})
}

var _ = time.Now
