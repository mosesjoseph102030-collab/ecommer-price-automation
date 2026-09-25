package recommendation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/db"
	"automation/internal/woocommerce"
)

const maxPublishAttempts = 3

type executionTask struct {
	ExecutionID string
	OrgID       string
	RequestID   string
	ProductID   string
	VariantID   string
	Before      int64
	Requested   int64
	Attempts    int
}

type rollbackTask struct {
	RollbackID  string
	OrgID       string
	ExecutionID string
	ProductID   string
	VariantID   string
	Restore     int64
	Expected    int64
	Attempts    int
}

// publishAction is the safe decision derived from the live WooCommerce state.
type publishAction string

const (
	actionPublish   publishAction = "publish"
	actionVerify    publishAction = "verify_already_applied"
	actionConflict  publishAction = "conflict"
	actionSaleGuard publishAction = "sale_price_conflict"
)

// classifyLive decides what to do given the current server-side price. This is
// deliberately pure so the safety rules can be proven by unit tests:
//   - an active sale price is never silently overwritten
//   - a price already at the target is treated as applied (lost-response case)
//   - any other unexpected price is a conflict, never an overwrite
func classifyLive(hasSalePrice bool, livePrice, before, requested int64) publishAction {
	switch {
	case hasSalePrice:
		return actionSaleGuard
	case livePrice == requested:
		return actionVerify
	case livePrice != before:
		return actionConflict
	default:
		return actionPublish
	}
}

// ProcessNextExecution publishes one due price change. It returns true when a
// job was processed. Every path is idempotent and conflict-safe.
func (s *Service) ProcessNextExecution(ctx context.Context) (bool, error) {
	task, err := s.claimExecution(ctx)
	if err != nil || task == nil {
		return false, err
	}
	s.runExecution(ctx, task)
	return true, nil
}

func (s *Service) claimExecution(ctx context.Context) (*executionTask, error) {
	jobDB := s.JobDB
	if jobDB == nil {
		jobDB = s.DB
	}
	tx, err := jobDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var executionID, orgID string
	err = tx.QueryRowContext(ctx, `SELECT id, organization_id FROM price_change_executions
		WHERE status='queued' AND (scheduled_for IS NULL OR scheduled_for<=now()) AND created_at > now() - interval '24 hours'
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&executionID, &orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_change_executions SET status='running', attempts=attempts+1, started_at=now(), updated_at=now() WHERE id=$1`, executionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	task := &executionTask{ExecutionID: executionID, OrgID: orgID}
	if err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT e.price_change_request_id, e.product_id, e.before_price_kobo, e.requested_price_kobo, e.attempts,
			COALESCE((SELECT variant_id::text FROM price_recommendations WHERE id=r.recommendation_id),'')
			FROM price_change_executions e JOIN price_change_requests r ON r.id=e.price_change_request_id
			WHERE e.organization_id=$1 AND e.id=$2`, orgID, executionID).
			Scan(&task.RequestID, &task.ProductID, &task.Before, &task.Requested, &task.Attempts, &task.VariantID)
	}); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Service) runExecution(ctx context.Context, task *executionTask) {
	// Re-check the kill switch immediately before any write.
	if enabled, err := s.killSwitchEnabledDB(ctx, task.OrgID); err == nil && enabled {
		s.finishExecution(ctx, task, "cancelled", 0, "publishing disabled by kill switch", nil)
		return
	}
	live, err := s.Woo.LoadProductLive(ctx, task.OrgID, task.ProductID, task.VariantID)
	if err != nil {
		s.handleExecutionError(ctx, task, err)
		return
	}
	// A live sale price would make a regular-price-only write change the
	// customer-visible price. Stop instead of overwriting silently.
	action := classifyLive(live.HasSalePrice, live.EffectivePriceKobo, task.Before, task.Requested)
	switch action {
	case actionSaleGuard:
		s.finishExecution(ctx, task, "conflict", 0, "product has an active sale price; resolve it before publishing", nil)
		return
	case actionVerify:
		// Lost-response reconciliation: the write may already have landed.
		s.finishExecution(ctx, task, "succeeded", task.Requested, "", &live)
		return
	case actionConflict:
		// Manual change conflict: the current price is neither what we recorded
		// nor what we intend to set.
		s.finishExecution(ctx, task, "conflict", live.EffectivePriceKobo,
			fmt.Sprintf("price changed externally to %s; resolve before publishing", formatMoney(live.EffectivePriceKobo)), nil)
		return
	}
	updated, err := s.Woo.PublishRegularPrice(ctx, task.OrgID, task.ProductID, task.VariantID, task.Requested)
	if err != nil {
		s.handleExecutionError(ctx, task, err)
		return
	}
	if updated.EffectivePriceKobo != task.Requested {
		s.finishExecution(ctx, task, "conflict", updated.EffectivePriceKobo, "verification failed after update", nil)
		return
	}
	// Independent read-back verification.
	verified, err := s.Woo.LoadProductLive(ctx, task.OrgID, task.ProductID, task.VariantID)
	if err != nil {
		s.handleExecutionError(ctx, task, err)
		return
	}
	if verified.EffectivePriceKobo != task.Requested {
		s.finishExecution(ctx, task, "conflict", verified.EffectivePriceKobo, "read-back verification did not match the requested price", nil)
		return
	}
	s.finishExecution(ctx, task, "succeeded", task.Requested, "", &verified)
}

func (s *Service) handleExecutionError(ctx context.Context, task *executionTask, cause error) {
	message := safePublishError(cause)
	switch {
	case errors.Is(cause, woocommerce.ErrProductGone):
		// Do not retry: the product must be remapped first.
		s.finishExecution(ctx, task, "failed", 0, message, nil)
	case woocommerce.IsAuthError(cause):
		// Credentials expired: stop and alert the owner.
		s.finishExecution(ctx, task, "failed", 0, message, nil)
	default:
		if task.Attempts >= maxPublishAttempts {
			s.finishExecution(ctx, task, "failed", 0, message, nil)
			return
		}
		// Safe retry: requeue and reconcile from WooCommerce on the next pass.
		_ = db.WithTenant(ctx, s.DB, task.OrgID, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE price_change_executions SET status='queued', last_error=$3,
				scheduled_for=now() + ($4 * interval '30 seconds'), updated_at=now()
				WHERE organization_id=$1 AND id=$2 AND status='running'`, task.OrgID, task.ExecutionID, message, task.Attempts)
			return err
		})
	}
}

func (s *Service) finishExecution(ctx context.Context, task *executionTask, status string, afterPrice int64, message string, live *woocommerce.LivePrice) {
	_ = db.WithTenant(ctx, s.DB, task.OrgID, func(tx *sql.Tx) error {
		var verified any
		if afterPrice > 0 {
			verified = afterPrice
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_change_executions SET status=$3, verified_after_price_kobo=$4,
			last_error=$5, finished_at=now(), updated_at=now() WHERE organization_id=$1 AND id=$2`,
			task.OrgID, task.ExecutionID, status, verified, message); err != nil {
			return err
		}
		requestStatus := "failed"
		switch status {
		case "succeeded":
			requestStatus = "published"
		case "conflict", "cancelled":
			requestStatus = "failed"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_change_requests SET status=$3, updated_at=now() WHERE organization_id=$1 AND id=$2`,
			task.OrgID, task.RequestID, requestStatus); err != nil {
			return err
		}
		if status == "succeeded" && live != nil {
			var previousPlatform sql.NullInt64
			if err := tx.QueryRowContext(ctx, `SELECT platform_price_kobo FROM products WHERE organization_id=$1 AND id=$2`, task.OrgID, task.ProductID).Scan(&previousPlatform); err != nil {
				return err
			}
			if previousPlatform.Valid && previousPlatform.Int64 != task.Requested {
				if _, err := tx.ExecContext(ctx, `INSERT INTO product_price_snapshots (organization_id, product_id, variant_id, price_kobo, source)
					VALUES ($1,$2,NULLIF($3,'')::uuid,$4,'platform')`, task.OrgID, task.ProductID, task.VariantID, previousPlatform.Int64); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE products SET price_kobo=$3, sale_price_kobo=NULL, platform_price_kobo=$3,
				price_conflict=false, price_conflict_detected_at=NULL, woo_updated_at=now(), updated_at=now()
				WHERE organization_id=$1 AND id=$2`, task.OrgID, task.ProductID, task.Requested); err != nil {
				return err
			}
			if task.VariantID != "" {
				if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET price_kobo=$3, sale_price_kobo=NULL, woo_updated_at=now()
					WHERE organization_id=$1 AND id=$2`, task.OrgID, task.VariantID, task.Requested); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO product_price_snapshots (organization_id, product_id, variant_id, price_kobo, source)
				VALUES ($1,$2,NULLIF($3,'')::uuid,$4,'platform')`, task.OrgID, task.ProductID, task.VariantID, task.Requested); err != nil {
				return err
			}
		}
		title := "Price change failed"
		body := message
		if status == "succeeded" {
			title = "Price change published"
			body = fmt.Sprintf("Price updated from %s to %s.", formatMoney(task.Before), formatMoney(task.Requested))
		} else if status == "conflict" {
			title = "Price change needs review"
		}
		if err := notifyOwners(ctx, tx, task.OrgID, "price_change."+status, title, body); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{OrgID: task.OrgID, Action: "price_change." + status,
			ResourceType: "price_change_execution", ResourceID: task.ExecutionID,
			OldState: map[string]any{"price_kobo": task.Before},
			NewState: map[string]any{"price_kobo": task.Requested, "verified_after_price_kobo": afterPrice, "error": message}})
	})
	if s.Logger != nil {
		s.Logger.Info("price_change_execution_finished", "execution_id", task.ExecutionID, "status", status, "error", message)
	}
}

// QueueRollback schedules restoration of the prior confirmed price.
func (s *Service) QueueRollback(ctx context.Context, orgID, actor, executionID string) (Rollback, error) {
	var out Rollback
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		enabled, err := s.killSwitchEnabled(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if enabled {
			return ErrKillSwitchOn
		}
		var requestID, productID string
		var before, requested int64
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT price_change_request_id, product_id, before_price_kobo, requested_price_kobo, status
			FROM price_change_executions WHERE organization_id=$1 AND id=$2 FOR UPDATE`, orgID, executionID).
			Scan(&requestID, &productID, &before, &requested, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != "succeeded" {
			return errors.New("only a succeeded publish can be rolled back")
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_rollbacks WHERE organization_id=$1 AND execution_id=$2 AND status IN ('queued','running')`, orgID, executionID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return errors.New("a rollback is already in progress")
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO price_rollbacks (organization_id, execution_id, product_id, restore_price_kobo, status, created_by_user_id)
			VALUES ($1,$2,$3,$4,'queued',NULLIF($5,'')::uuid) RETURNING id, created_at`, orgID, executionID, productID, before, actor).
			Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		out.ExecutionID, out.ProductID, out.RestorePriceKobo, out.Status = executionID, productID, before, "queued"
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "price_change.rollback_queued", ResourceType: "price_rollback", ResourceID: out.ID, NewState: out})
	})
	return out, err
}

// ProcessNextRollback restores one prior price.
func (s *Service) ProcessNextRollback(ctx context.Context) (bool, error) {
	task, err := s.claimRollback(ctx)
	if err != nil || task == nil {
		return false, err
	}
	s.runRollback(ctx, task)
	return true, nil
}

func (s *Service) claimRollback(ctx context.Context) (*rollbackTask, error) {
	jobDB := s.JobDB
	if jobDB == nil {
		jobDB = s.DB
	}
	tx, err := jobDB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var rollbackID, orgID string
	err = tx.QueryRowContext(ctx, `SELECT id, organization_id FROM price_rollbacks WHERE status='queued' ORDER BY created_at
		FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&rollbackID, &orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_rollbacks SET status='running', attempts=attempts+1 WHERE id=$1`, rollbackID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	task := &rollbackTask{RollbackID: rollbackID, OrgID: orgID}
	if err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT b.execution_id, b.product_id, b.restore_price_kobo, e.requested_price_kobo, b.attempts,
			COALESCE((SELECT variant_id::text FROM price_recommendations WHERE id=r.recommendation_id),'')
			FROM price_rollbacks b JOIN price_change_executions e ON e.id=b.execution_id
			JOIN price_change_requests r ON r.id=e.price_change_request_id
			WHERE b.organization_id=$1 AND b.id=$2`, orgID, rollbackID).
			Scan(&task.ExecutionID, &task.ProductID, &task.Restore, &task.Expected, &task.Attempts, &task.VariantID)
	}); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *Service) runRollback(ctx context.Context, task *rollbackTask) {
	if enabled, err := s.killSwitchEnabledDB(ctx, task.OrgID); err == nil && enabled {
		s.finishRollback(ctx, task, "failed", 0, "publishing disabled by kill switch")
		return
	}
	live, err := s.Woo.LoadProductLive(ctx, task.OrgID, task.ProductID, task.VariantID)
	if err != nil {
		s.handleRollbackError(ctx, task, err)
		return
	}
	if live.EffectivePriceKobo == task.Restore {
		s.finishRollback(ctx, task, "succeeded", task.Restore, "")
		return
	}
	if live.EffectivePriceKobo != task.Expected {
		s.finishRollback(ctx, task, "conflict", live.EffectivePriceKobo,
			fmt.Sprintf("price changed externally to %s; resolve before rolling back", formatMoney(live.EffectivePriceKobo)))
		return
	}
	updated, err := s.Woo.PublishRegularPrice(ctx, task.OrgID, task.ProductID, task.VariantID, task.Restore)
	if err != nil {
		s.handleRollbackError(ctx, task, err)
		return
	}
	if updated.EffectivePriceKobo != task.Restore {
		s.finishRollback(ctx, task, "conflict", updated.EffectivePriceKobo, "rollback verification failed")
		return
	}
	verified, err := s.Woo.LoadProductLive(ctx, task.OrgID, task.ProductID, task.VariantID)
	if err != nil {
		s.handleRollbackError(ctx, task, err)
		return
	}
	if verified.EffectivePriceKobo != task.Restore {
		s.finishRollback(ctx, task, "conflict", verified.EffectivePriceKobo, "rollback read-back verification failed")
		return
	}
	s.finishRollback(ctx, task, "succeeded", task.Restore, "")
}

func (s *Service) handleRollbackError(ctx context.Context, task *rollbackTask, cause error) {
	message := safePublishError(cause)
	if errors.Is(cause, woocommerce.ErrProductGone) || woocommerce.IsAuthError(cause) || task.Attempts >= maxPublishAttempts {
		s.finishRollback(ctx, task, "failed", 0, message)
		return
	}
	_ = db.WithTenant(ctx, s.DB, task.OrgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE price_rollbacks SET status='queued', last_error=$3 WHERE organization_id=$1 AND id=$2 AND status='running'`,
			task.OrgID, task.RollbackID, message)
		return err
	})
}

func (s *Service) finishRollback(ctx context.Context, task *rollbackTask, status string, after int64, message string) {
	_ = db.WithTenant(ctx, s.DB, task.OrgID, func(tx *sql.Tx) error {
		var verified any
		if after > 0 {
			verified = after
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_rollbacks SET status=$3, verified_after_price_kobo=$4, last_error=$5, finished_at=now()
			WHERE organization_id=$1 AND id=$2`, task.OrgID, task.RollbackID, status, verified, message); err != nil {
			return err
		}
		if status == "succeeded" {
			if _, err := tx.ExecContext(ctx, `UPDATE price_change_executions SET status='rolled_back', updated_at=now() WHERE organization_id=$1 AND id=$2`,
				task.OrgID, task.ExecutionID); err != nil {
				return err
			}
		}
		if status == "succeeded" {
			if _, err := tx.ExecContext(ctx, `UPDATE products SET price_kobo=$3, platform_price_kobo=$3, updated_at=now() WHERE organization_id=$1 AND id=$2`,
				task.OrgID, task.ProductID, task.Restore); err != nil {
				return err
			}
			if task.VariantID != "" {
				if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET price_kobo=$3 WHERE organization_id=$1 AND id=$2`, task.OrgID, task.VariantID, task.Restore); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `UPDATE price_change_requests SET status='rolled_back', updated_at=now() WHERE organization_id=$1 AND id=(SELECT price_change_request_id FROM price_change_executions WHERE organization_id=$1 AND id=$2)`,
				task.OrgID, task.ExecutionID); err != nil {
				return err
			}
		}
		title := "Rollback failed"
		body := message
		if status == "succeeded" {
			title = "Price change rolled back"
			body = fmt.Sprintf("Price restored to %s.", formatMoney(task.Restore))
		}
		if err := notifyOwners(ctx, tx, task.OrgID, "price_change.rollback_"+status, title, body); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{OrgID: task.OrgID, Action: "price_change.rollback_" + status,
			ResourceType: "price_rollback", ResourceID: task.RollbackID,
			OldState: map[string]any{"price_kobo": task.Expected},
			NewState: map[string]any{"price_kobo": task.Restore, "error": message}})
	})
}

func safePublishError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 300 {
		message = message[:300]
	}
	return strings.TrimSpace(message)
}

// tenantIDs lists tenants for the generation loop.
func (s *Service) tenantIDs(ctx context.Context) ([]string, error) {
	database := s.JobDB
	if database == nil {
		database = s.DB
	}
	rows, err := database.QueryContext(ctx, `SELECT id::text FROM organizations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// RunWorker drives generation, publishing, and rollbacks.
func (s *Service) RunWorker(ctx context.Context) {
	interval := s.Interval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	ticker := time.NewTicker(10 * time.Second)
	generate := time.NewTicker(interval)
	defer ticker.Stop()
	defer generate.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-generate.C:
			ids, err := s.tenantIDs(ctx)
			if err == nil {
				for _, id := range ids {
					if _, err := s.Generate(ctx, id); err != nil && s.Logger != nil {
						s.Logger.Error("recommendation_generation_failed", "organization_id", id, "error", err)
					}
				}
			}
		case <-ticker.C:
			for i := 0; i < 5; i++ {
				processed, err := s.ProcessNextExecution(ctx)
				if err != nil {
					if s.Logger != nil {
						s.Logger.Error("publisher_worker_failed", "error", err)
					}
					break
				}
				if !processed {
					break
				}
			}
			for i := 0; i < 5; i++ {
				processed, err := s.ProcessNextRollback(ctx)
				if err != nil {
					if s.Logger != nil {
						s.Logger.Error("rollback_worker_failed", "error", err)
					}
					break
				}
				if !processed {
					break
				}
			}
		}
	}
}
