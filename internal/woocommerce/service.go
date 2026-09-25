package woocommerce

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/db"
	"automation/internal/reliability"
)

type Service struct {
	DB                *sql.DB
	JobDB             *sql.DB
	DiagnosticsDB     *sql.DB
	Crypter           *Crypter
	HTTPClient        *http.Client
	Logger            *slog.Logger
	AllowInsecure     bool
	AllowPrivate      bool
	WebhookBaseURL    string
	ReconcileInterval time.Duration
	// Breakers holds one circuit breaker per store host. A nil registry
	// disables circuit breaking entirely, which is a valid configuration for a
	// single-tenant install but not for a shared one.
	Breakers *reliability.Registry
}

type Credentials struct {
	ConsumerKey    string `json:"consumer_key"`
	ConsumerSecret string `json:"consumer_secret"`
	WebhookSecret  string `json:"webhook_secret"`
}

type Connection struct {
	ID               string     `json:"id"`
	StoreID          string     `json:"store_id"`
	StoreName        string     `json:"store_name"`
	StoreURL         string     `json:"store_url"`
	APIVersion       string     `json:"api_version"`
	Status           string     `json:"status"`
	WebhookStatus    string     `json:"webhook_status"`
	LastSyncAt       *time.Time `json:"last_sync_at"`
	LastError        string     `json:"last_error"`
	RateLimitedUntil *time.Time `json:"rate_limited_until"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ConnectInput struct {
	StoreName      string
	StoreURL       string
	ConsumerKey    string
	ConsumerSecret string
}

type CatalogStats struct {
	Products int64 `json:"products"`
	Variants int64 `json:"variants"`
}

type Health struct {
	Connection       Connection   `json:"connection"`
	LatestSyncRunID  string       `json:"latest_sync_run_id"`
	LatestSyncStatus string       `json:"latest_sync_status"`
	Catalog          CatalogStats `json:"catalog"`
	WebhookEvents    int64        `json:"webhook_events"`
}

func (s *Service) Connect(ctx context.Context, orgID, actorUserID string, in ConnectInput) (Connection, string, error) {
	in.StoreName = strings.TrimSpace(in.StoreName)
	in.ConsumerKey = strings.TrimSpace(in.ConsumerKey)
	in.ConsumerSecret = strings.TrimSpace(in.ConsumerSecret)
	if in.StoreName == "" {
		return Connection{}, "", errors.New("store name is required")
	}
	if len(in.ConsumerKey) < 8 || len(in.ConsumerSecret) < 8 {
		return Connection{}, "", errors.New("WooCommerce API key and secret are required")
	}
	baseURL, err := NormalizeStoreURL(in.StoreURL, s.AllowInsecure, s.AllowPrivate)
	if err != nil {
		return Connection{}, "", err
	}
	client := NewClient(baseURL, in.ConsumerKey, in.ConsumerSecret, s.HTTPClient)
	if err := client.Test(ctx); err != nil {
		return Connection{}, "", mapConnectionError(err)
	}
	webhookSecret, err := randomSecret()
	if err != nil {
		return Connection{}, "", err
	}
	credJSON, _ := json.Marshal(Credentials{ConsumerKey: in.ConsumerKey, ConsumerSecret: in.ConsumerSecret, WebhookSecret: webhookSecret})
	enc, err := s.Crypter.Encrypt(credJSON)
	if err != nil {
		return Connection{}, "", err
	}

	var conn Connection
	err = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var storeID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO stores (organization_id, name, url) VALUES ($1,$2,$3)
			ON CONFLICT (organization_id, url) DO UPDATE SET name=EXCLUDED.name, updated_at=now()
			RETURNING id`, orgID, in.StoreName, baseURL).Scan(&storeID)
		if err != nil {
			return err
		}
		var connectionID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO store_connections (organization_id, store_id, api_version, status, webhook_status)
			VALUES ($1,$2,'v3','connected','pending')
			ON CONFLICT (organization_id, store_id) DO UPDATE SET status='connected', api_version='v3', last_error='', updated_at=now()
			RETURNING id`, orgID, storeID).Scan(&connectionID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO store_connection_credentials (organization_id, connection_id, ciphertext, nonce, key_version)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (connection_id) DO UPDATE SET ciphertext=EXCLUDED.ciphertext, nonce=EXCLUDED.nonce,
			key_version=EXCLUDED.key_version, updated_at=now()`, orgID, connectionID, enc.Data, enc.Nonce, enc.KeyVersion)
		if err != nil {
			return err
		}
		if err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+` WHERE sc.id=$1`, connectionID), &conn); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{
			OrgID: orgID, ActorUserID: actorUserID, Action: "woocommerce.connected",
			ResourceType: "store_connection", ResourceID: conn.ID,
			NewState: map[string]any{"store_name": conn.StoreName, "store_url": conn.StoreURL},
		})
	})
	if err != nil {
		return Connection{}, "", err
	}

	webhookStatus := "disabled"
	if s.WebhookBaseURL != "" {
		callback := strings.TrimRight(s.WebhookBaseURL, "/") + "/api/v1/webhooks/woocommerce"
		if err := client.CreateWebhook(ctx, callback, "product.updated,product.created,product.deleted", webhookSecret); err != nil {
			s.markConnectionError(ctx, orgID, conn.ID, err)
			webhookStatus = "error"
		} else {
			webhookStatus = "active"
			_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `UPDATE store_connections SET webhook_status='active', updated_at=now() WHERE id=$1`, conn.ID)
				return err
			})
		}
	}
	conn.WebhookStatus = webhookStatus
	runID, err := s.EnqueueSync(ctx, orgID, conn.StoreID, "initial", actorUserID)
	if err != nil {
		return conn, "", err
	}
	return conn, runID, nil
}

const connectionSelect = `
SELECT sc.id, sc.store_id, st.name, st.url, sc.api_version, sc.status, sc.webhook_status,
       sc.last_sync_at, sc.last_error, sc.rate_limited_until, sc.updated_at
FROM store_connections sc JOIN stores st ON st.id=sc.store_id`

type rowScanner interface{ Scan(...any) error }

func scanConnection(row rowScanner, out *Connection) error {
	return row.Scan(&out.ID, &out.StoreID, &out.StoreName, &out.StoreURL, &out.APIVersion, &out.Status,
		&out.WebhookStatus, &out.LastSyncAt, &out.LastError, &out.RateLimitedUntil, &out.UpdatedAt)
}

func (s *Service) AdminDiagnostics(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	database := s.DiagnosticsDB
	if database == nil {
		database = s.DB
	}
	rows, err := database.QueryContext(ctx, `SELECT sc.organization_id, st.name, st.url, sc.status, sc.webhook_status,
		sc.last_sync_at, sc.last_error, sc.rate_limited_until, sc.updated_at
		FROM store_connections sc JOIN stores st ON st.id=sc.store_id
		ORDER BY sc.updated_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var orgID, name, url, status, webhook, lastError string
		var lastSync, rateUntil sql.NullTime
		var updated time.Time
		if err := rows.Scan(&orgID, &name, &url, &status, &webhook, &lastSync, &lastError, &rateUntil, &updated); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"organization_id": orgID, "store_name": name, "store_url": url,
			"status": status, "webhook_status": webhook, "last_sync_at": lastSync, "last_error": lastError,
			"rate_limited_until": rateUntil, "updated_at": updated})
	}
	return out, rows.Err()
}

func (s *Service) Status(ctx context.Context, orgID string) (*Health, error) {
	var health Health
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+` ORDER BY sc.updated_at DESC LIMIT 1`), &health.Connection)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_ = tx.QueryRowContext(ctx, `SELECT id, status FROM store_sync_runs WHERE store_id=$1 ORDER BY created_at DESC LIMIT 1`, health.Connection.StoreID).Scan(&health.LatestSyncRunID, &health.LatestSyncStatus)
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE store_id=$1 AND status <> 'deleted'`, health.Connection.StoreID).Scan(&health.Catalog.Products)
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_variants pv JOIN products p ON p.id=pv.product_id WHERE p.store_id=$1`, health.Connection.StoreID).Scan(&health.Catalog.Variants)
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_events WHERE store_connection_id=$1`, health.Connection.ID).Scan(&health.WebhookEvents)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &health, nil
}

func (s *Service) Disconnect(ctx context.Context, orgID, connectionID, reason string) error {
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE store_connections SET status='disconnected', webhook_status='disabled', last_error=$2, updated_at=now() WHERE id=$1`, connectionID, reason)
		return err
	})
}

func (s *Service) TestConnection(ctx context.Context, orgID string) error {
	conn, _, client, err := s.load(ctx, orgID, "")
	if err != nil {
		return err
	}
	if conn == nil {
		return errors.New("no WooCommerce store is connected")
	}
	if err := client.Test(ctx); err != nil {
		mapped := mapConnectionError(err)
		s.markConnectionError(ctx, orgID, conn.ID, err)
		return mapped
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE store_connections SET status='connected', last_error='', rate_limited_until=NULL, updated_at=now() WHERE id=$1`, conn.ID)
		return err
	})
}

func (s *Service) EnqueueSync(ctx context.Context, orgID, storeID, kind, actor string) (string, error) {
	if kind != "initial" && kind != "webhook" && kind != "reconcile" && kind != "retry" {
		return "", errors.New("invalid sync kind")
	}
	var id string
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var running int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_sync_runs WHERE store_id=$1 AND status IN ('queued','running')`, storeID).Scan(&running); err != nil {
			return err
		}
		if running > 0 {
			return errors.New("SYNC_IN_PROGRESS")
		}
		return tx.QueryRowContext(ctx, `INSERT INTO store_sync_runs (organization_id, store_id, kind, status, requested_by_user_id, next_attempt_at) VALUES ($1,$2,$3,'queued',NULLIF($4,'')::uuid,now()) RETURNING id`, orgID, storeID, kind, actor).Scan(&id)
	})
	return id, err
}

func (s *Service) load(ctx context.Context, orgID, connectionID string) (*Connection, *Credentials, *Client, error) {
	var conn Connection
	var ciphertext, nonce []byte
	var keyVersion int
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := connectionSelect + ` WHERE sc.organization_id=$1`
		args := []any{orgID}
		if connectionID != "" {
			query += ` AND sc.id=$2`
			args = append(args, connectionID)
		}
		query += ` ORDER BY sc.updated_at DESC LIMIT 1`
		if err := scanConnection(tx.QueryRowContext(ctx, query, args...), &conn); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT ciphertext, nonce, key_version FROM store_connection_credentials WHERE connection_id=$1`, conn.ID).Scan(&ciphertext, &nonce, &keyVersion)
	})
	if err != nil {
		return nil, nil, nil, err
	}
	plain, err := s.Crypter.Decrypt(Ciphertext{Data: ciphertext, Nonce: nonce, KeyVersion: keyVersion})
	if err != nil {
		return nil, nil, nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(plain, &creds); err != nil {
		return nil, nil, nil, errors.New("stored connector credentials are invalid")
	}
	client := NewClient(conn.StoreURL, creds.ConsumerKey, creds.ConsumerSecret, s.HTTPClient)
	// One breaker per store host, so an unreachable store pauses only itself.
	// Without this, one dead store would eventually trip a shared breaker and
	// stop publishing for every tenant.
	client.Breaker = s.Breakers.Get("woocommerce:" + strings.ToLower(conn.StoreURL))
	return &conn, &creds, client, nil
}

func (s *Service) markConnectionError(ctx context.Context, orgID, connectionID string, cause error) {
	status := "error"
	var until any
	if IsAuthError(cause) {
		status = "revoked"
	}
	var ae *APIError
	if errors.As(cause, &ae) && ae.Status == http.StatusTooManyRequests {
		if ae.RetryAfter > 0 {
			until = time.Now().UTC().Add(ae.RetryAfter)
		}
	}
	_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE store_connections SET status=$2, last_error=$3, rate_limited_until=$4, updated_at=now() WHERE id=$1`, connectionID, status, safeError(cause), until)
		return err
	})
}

func mapConnectionError(err error) error {
	if err == nil {
		return nil
	}
	var ae *APIError
	if errors.As(err, &ae) {
		switch {
		case ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden:
			return errors.New("WooCommerce rejected the API key or secret. Generate a new read/write key and reconnect.")
		case ae.Status == http.StatusTooManyRequests:
			return fmt.Errorf("WooCommerce rate limit reached. Retry after %s.", ae.RetryAfter)
		case ae.Status == http.StatusNotFound:
			return errors.New("WooCommerce REST API v3 was not found. Check the store URL and permalinks.")
		case ae.Malformed:
			return errors.New("WooCommerce returned an unexpected response. Contact support with the request ID.")
		}
	}
	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "Client.Timeout") {
		return errors.New("we could not reach this WooCommerce store. Check its URL, firewall, and SSL certificate")
	}
	return errors.New("WooCommerce connection failed. Verify the URL and API credentials")
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	m := mapConnectionError(err)
	if m == nil {
		return ""
	}
	return m.Error()
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
