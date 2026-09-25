package woocommerce

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"automation/internal/db"
)

type WebhookIngress struct {
	Accepted       bool   `json:"accepted"`
	Duplicate      bool   `json:"duplicate"`
	EventID        string `json:"event_id"`
	ReconcileRunID string `json:"reconcile_run_id,omitempty"`
}

// ReceiveWebhook authenticates by HMAC, stores the raw event before processing,
// deduplicates by provider ID (or body hash), then queues reconciliation.
// The webhook payload is never trusted to select an organization.
func (s *Service) ReceiveWebhook(ctx context.Context, source, signature, providerID, topic string, body []byte) (WebhookIngress, error) {
	if len(body) > 2<<20 {
		return WebhookIngress{}, errors.New("webhook body is too large")
	}
	baseURL, err := NormalizeStoreURL(source, s.AllowInsecure, s.AllowPrivate)
	if err != nil {
		return WebhookIngress{}, errors.New("webhook source is invalid")
	}
	var orgID, connectionID, storeID string
	var ciphertext, nonce []byte
	var keyVersion int
	lookupDB := s.DiagnosticsDB
	if lookupDB == nil {
		lookupDB = s.DB
	}
	err = lookupDB.QueryRowContext(ctx, `
		SELECT sc.organization_id, sc.id, sc.store_id, scc.ciphertext, scc.nonce, scc.key_version
		FROM store_connections sc
		JOIN stores st ON st.id=sc.store_id
		JOIN store_connection_credentials scc ON scc.connection_id=sc.id
		WHERE st.url=$1 AND sc.status IN ('connected','error') LIMIT 1`, baseURL).
		Scan(&orgID, &connectionID, &storeID, &ciphertext, &nonce, &keyVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return WebhookIngress{}, errors.New("webhook connection not found")
	}
	if err != nil {
		return WebhookIngress{}, err
	}
	plain, err := s.Crypter.Decrypt(Ciphertext{Data: ciphertext, Nonce: nonce, KeyVersion: keyVersion})
	if err != nil {
		return WebhookIngress{}, errors.New("webhook authentication failed")
	}
	var creds Credentials
	if json.Unmarshal(plain, &creds) != nil || creds.WebhookSecret == "" {
		return WebhookIngress{}, errors.New("webhook authentication failed")
	}
	if !VerifyWebhookSignature([]byte(creds.WebhookSecret), body, signature) {
		return WebhookIngress{}, errors.New("webhook signature is invalid")
	}
	if providerID == "" {
		h := sha256.Sum256(body)
		providerID = "sha256:" + hex.EncodeToString(h[:])
	}
	// Source header is also included so two providers sending an identical body are distinct.
	dedupeID := providerID + ":" + shortHash(baseURL)
	eventID := ""
	inserted := false
	err = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
			INSERT INTO webhook_events (organization_id, store_connection_id, provider_event_id, topic, raw_body, signature, status)
			VALUES ($1,$2,$3,$4,$5,$6,'received')
			ON CONFLICT (store_connection_id, provider_event_id) DO UPDATE SET provider_event_id=webhook_events.provider_event_id
			RETURNING id, (xmax = 0) AS inserted`, orgID, connectionID, dedupeID, topic, body, signature).Scan(&eventID, &inserted)
	})
	if err != nil {
		return WebhookIngress{}, err
	}
	if !inserted {
		_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `INSERT INTO webhook_delivery_attempts (organization_id, webhook_event_id, attempt, status, error) VALUES ($1,$2,1,'skipped','duplicate')`, orgID, eventID)
			return e
		})
		return WebhookIngress{Accepted: true, Duplicate: true, EventID: eventID}, nil
	}

	if strings.Contains(strings.ToLower(topic), "deleted") {
		var deleted Product
		if json.Unmarshal(body, &deleted) == nil && deleted.ID > 0 {
			_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `UPDATE products SET status='deleted', last_seen_at=now(), updated_at=now()
					WHERE organization_id=$1 AND store_id=$2 AND external_id=$3`, orgID, storeID, fmt.Sprint(deleted.ID))
				if err != nil {
					return err
				}
				_, err = tx.ExecContext(ctx, `UPDATE product_external_mappings SET conflict='upstream_deleted', updated_at=now()
					WHERE organization_id=$1 AND external_product_id=$2`, orgID, fmt.Sprint(deleted.ID))
				return err
			})
		}
	}
	runID, err := s.EnqueueSync(ctx, orgID, storeID, "webhook", "")
	if err != nil && !strings.Contains(err.Error(), "SYNC_IN_PROGRESS") {
		_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `UPDATE webhook_events SET status='failed' WHERE id=$1`, eventID)
			return e
		})
		return WebhookIngress{}, err
	}
	_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE webhook_events SET status='processed' WHERE id=$1`, eventID)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO webhook_delivery_attempts (organization_id, webhook_event_id, attempt, status) VALUES ($1,$2,1,'processed')`, orgID, eventID)
		return e
	})
	return WebhookIngress{Accepted: true, EventID: eventID, ReconcileRunID: runID}, nil
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

func (s *Service) RetrySync(ctx context.Context, orgID, runID, actor string) (string, error) {
	var newID string
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var storeID, status string
		if err := tx.QueryRowContext(ctx, `SELECT store_id, status FROM store_sync_runs WHERE organization_id=$1 AND id=$2`, orgID, runID).Scan(&storeID, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("sync run not found")
			}
			return err
		}
		if status == "queued" || status == "running" {
			return errors.New("SYNC_IN_PROGRESS")
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_sync_runs WHERE store_id=$1 AND status IN ('queued','running')`, storeID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return errors.New("SYNC_IN_PROGRESS")
		}
		return tx.QueryRowContext(ctx, `INSERT INTO store_sync_runs (organization_id, store_id, kind, status, requested_by_user_id, next_attempt_at) VALUES ($1,$2,'retry','queued',NULLIF($3,'')::uuid,now()) RETURNING id`, orgID, storeID, actor).Scan(&newID)
	})
	return newID, err
}

func (s *Service) ReconcileDue(ctx context.Context) error {
	jobDB := s.JobDB
	if jobDB == nil {
		jobDB = s.DB
	}
	rows, err := jobDB.QueryContext(ctx, `
		SELECT sc.organization_id, sc.store_id
		FROM store_connections sc
		WHERE sc.status='connected'
		  AND (sc.last_sync_at IS NULL OR sc.last_sync_at < now() - ($1 * interval '1 minute'))
		LIMIT 25`, int(s.ReconcileInterval/time.Minute))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var orgID, storeID string
		if err := rows.Scan(&orgID, &storeID); err != nil {
			return err
		}
		_, _ = s.EnqueueSync(ctx, orgID, storeID, "reconcile", "")
	}
	return rows.Err()
}

func (s *Service) RunWorker(ctx context.Context) {
	queueTick := time.NewTicker(2 * time.Second)
	reconcileTick := time.NewTicker(s.ReconcileInterval)
	defer queueTick.Stop()
	defer reconcileTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcileTick.C:
			if err := s.ReconcileDue(ctx); err != nil {
				s.Logger.Error("reconciliation_enqueue_failed", "error", err)
			}
		case <-queueTick.C:
			for i := 0; i < 5; i++ {
				processed, err := s.ProcessNext(ctx)
				if err != nil {
					s.Logger.Error("sync_worker_failed", "error", err)
					break
				}
				if !processed {
					break
				}
			}
		}
	}
}

var _ = http.StatusOK
var _ = time.Second
