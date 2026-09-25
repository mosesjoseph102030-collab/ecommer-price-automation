package competitor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"automation/internal/db"
)

type sourceTask struct {
	RunID, OrgID, ProductID, URL, MatchState string
	ConfirmedProductID                       string
	PreviousPrice                            sql.NullInt64
	PreviousAvailability                     string
	Interval, Freshness, Backoff             time.Duration
}

func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	jobDB := s.JobDB
	if jobDB == nil {
		jobDB = s.DB
	}
	tx, err := jobDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var runID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM competitor_source_runs WHERE status='queued' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&runID); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var orgID string
	if err := tx.QueryRowContext(ctx, `UPDATE competitor_source_runs SET status='running',started_at=now() WHERE id=$1 RETURNING organization_id`, runID).Scan(&orgID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	task, err := s.loadTask(ctx, orgID, runID)
	if err != nil {
		s.failRun(ctx, orgID, runID, 0, err)
		return true, nil
	}
	body, _, status, fetchErr := s.Provider.Fetch(ctx, task.URL)
	if fetchErr != nil {
		s.failRun(ctx, orgID, runID, status, fetchErr)
		return true, nil
	}
	extracted, extractErr := ExtractProduct(body)
	if extractErr != nil {
		s.failRun(ctx, orgID, runID, status, extractErr)
		return true, nil
	}
	if err := s.persistObservation(ctx, task, body, extracted, status); err != nil {
		s.failRun(ctx, orgID, runID, status, err)
		return true, nil
	}
	return true, nil
}

func (s *Service) loadTask(ctx context.Context, orgID, runID string) (sourceTask, error) {
	var t sourceTask
	var policyRaw []byte
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT r.id,r.organization_id,cp.id,cp.url,cp.match_state,COALESCE(cp.confirmed_product_id::text,''),cp.last_observed_price_kobo,COALESCE((SELECT availability FROM competitor_price_observations WHERE competitor_product_id=cp.id ORDER BY observed_at DESC LIMIT 1),'unknown'),c.monitoring_policy FROM competitor_source_runs r JOIN competitor_products cp ON cp.id=r.competitor_product_id JOIN competitors c ON c.id=cp.competitor_id WHERE r.organization_id=$1 AND r.id=$2`, orgID, runID).Scan(&t.RunID, &t.OrgID, &t.ProductID, &t.URL, &t.MatchState, &t.ConfirmedProductID, &t.PreviousPrice, &t.PreviousAvailability, &policyRaw)
	})
	if err != nil {
		return t, err
	}
	var policy MonitoringPolicy
	if len(policyRaw) > 0 {
		_ = json.Unmarshal(policyRaw, &policy)
	}
	t.Interval, t.Freshness, t.Backoff = sourceInterval(policy), sourceFreshness(policy), sourceBackoff(policy)
	return t, nil
}

func (s *Service) persistObservation(ctx context.Context, task sourceTask, body []byte, extracted ExtractedProduct, status int) error {
	hash := sha256.Sum256(body)
	evidenceHash := hex.EncodeToString(hash[:])
	now := time.Now().UTC()
	freshUntil := now.Add(task.Freshness)
	catalog := []CatalogProduct{}
	var orgCurrency string
	if err := db.WithTenant(ctx, s.DB, task.OrgID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT currency FROM organizations WHERE id=$1`, task.OrgID).Scan(&orgCurrency); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,name,sku FROM products WHERE organization_id=$1 AND status='published'`, task.OrgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p CatalogProduct
			if err := rows.Scan(&p.ID, &p.Name, &p.SKU); err != nil {
				return err
			}
			catalog = append(catalog, p)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	suggestion := SuggestMatch(extracted.Name, extracted.SKU, catalog)
	return db.WithTenant(ctx, s.DB, task.OrgID, func(tx *sql.Tx) error {
		var observationID string
		if err := tx.QueryRowContext(ctx, `INSERT INTO competitor_price_observations (organization_id,competitor_product_id,source_url,observed_price_kobo,regular_price_kobo,sale_price_kobo,currency,availability,extraction_confidence_bps,raw_evidence_sha256,observed_at,fresh_until) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::char(3),$8,$9,$10,$11,$12) RETURNING id`, task.OrgID, task.ProductID, task.URL, extracted.PriceKobo, extracted.RegularPriceKobo, extracted.SalePriceKobo, extracted.Currency, extracted.Availability, extracted.ExtractionConfidenceBPS, evidenceHash, now, freshUntil).Scan(&observationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO competitor_evidence (organization_id,observation_id,raw_content,content_sha256) VALUES($1,$2,$3,$4)`, task.OrgID, observationID, strings.ToValidUTF8(string(body), "\uFFFD"), evidenceHash); err != nil {
			return err
		}
		matchState := task.MatchState
		if task.ConfirmedProductID != "" {
			matchState = "confirmed"
		}
		var suggested any
		confidence := 0
		if matchState == "unmatched" || matchState == "suggested" || matchState == "stale" || matchState == "broken" {
			if suggestion != nil {
				matchState = "suggested"
				suggested = suggestion.ProductID
				confidence = suggestion.ConfidenceBPS
			} else {
				matchState = "unmatched"
			}
		}
		name, sku := extracted.Name, extracted.SKU
		if name == "" {
			name = task.URL
		}
		if _, err := tx.ExecContext(ctx, `UPDATE competitor_products SET name=$2,sku=$3,match_state=$4,suggested_product_id=$5,match_confidence_bps=$6,last_observed_price_kobo=$7,last_observed_at=$8,next_check_at=$9,consecutive_failures=0,last_error='',updated_at=now() WHERE organization_id=$1 AND id=$10`, task.OrgID, name, sku, matchState, suggested, confidence, extracted.PriceKobo, now, now.Add(task.Interval), task.ProductID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE competitor_source_runs SET status='succeeded',http_status=$2,duration_ms=extract(epoch from (now()-started_at))*1000,finished_at=now() WHERE organization_id=$1 AND id=$3`, task.OrgID, status, task.RunID); err != nil {
			return err
		}
		if extracted.PriceKobo == nil {
			_ = insertAlert(ctx, tx, task.OrgID, task.ProductID, "price_missing", "Structured page contained no exact product price.", "", "")
		}
		if extracted.Currency != "" && !strings.EqualFold(extracted.Currency, orgCurrency) {
			_ = insertAlert(ctx, tx, task.OrgID, task.ProductID, "currency_mismatch", "Competitor currency differs from store currency; no comparison was made.", orgCurrency, extracted.Currency)
		}
		if task.PreviousPrice.Valid && extracted.PriceKobo != nil && *extracted.PriceKobo < task.PreviousPrice.Int64 {
			_ = insertAlert(ctx, tx, task.OrgID, task.ProductID, "price_drop", "Competitor price decreased.", moneyString(task.PreviousPrice.Int64), moneyString(*extracted.PriceKobo))
		}
		if task.PreviousAvailability != "unknown" && extracted.Availability != "unknown" && task.PreviousAvailability != extracted.Availability {
			_ = insertAlert(ctx, tx, task.OrgID, task.ProductID, "stock_change", "Competitor availability changed.", task.PreviousAvailability, extracted.Availability)
		}
		return nil
	})
}

func (s *Service) failRun(ctx context.Context, orgID, runID string, status int, cause error) {
	message := safeCompetitorError(cause)
	_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var taskID string
		var failures int
		var backoffMinutes int
		if err := tx.QueryRowContext(ctx, `SELECT r.competitor_product_id,cp.consecutive_failures+1,
			COALESCE((c.monitoring_policy->>'backoff_minutes')::int,60)
			FROM competitor_source_runs r JOIN competitor_products cp ON cp.id=r.competitor_product_id
			JOIN competitors c ON c.id=cp.competitor_id WHERE r.organization_id=$1 AND r.id=$2`, orgID, runID).Scan(&taskID, &failures, &backoffMinutes); err != nil {
			return err
		}
		state := ""
		if failures >= 3 {
			state = "broken"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE competitor_products SET consecutive_failures=$3,last_error=$4,
			match_state=CASE WHEN match_state IN ('rejected','paused') THEN match_state WHEN $5='broken' THEN 'broken' ELSE match_state END,
			next_check_at=now()+($6 * interval '1 minute'),updated_at=now() WHERE organization_id=$1 AND id=$2`,
			orgID, taskID, failures, message, state, backoffMinutes); err != nil {
			return err
		}
		if state == "broken" {
			_ = insertAlert(ctx, tx, orgID, taskID, "broken_url", "Competitor source failed three consecutive checks.", "", message)
		}
		_, err := tx.ExecContext(ctx, `UPDATE competitor_source_runs SET status='failed',http_status=$2,error=$3,finished_at=now() WHERE organization_id=$1 AND id=$4`, orgID, status, message, runID)
		return err
	})
}

func insertAlert(ctx context.Context, tx *sql.Tx, orgID, productID, kind, message, previous, current string) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM competitor_alerts WHERE organization_id=$1 AND COALESCE(competitor_product_id::text,'')=$2 AND alert_type=$3 AND acknowledged_at IS NULL`, orgID, productID, kind).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO competitor_alerts (organization_id,competitor_product_id,alert_type,message,previous_value,current_value) VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6)`, orgID, productID, kind, message, previous, current)
	return err
}
func moneyString(value int64) string {
	remainder := value % 100
	if remainder < 0 {
		remainder = -remainder
	}
	return fmt.Sprintf("%d.%02d", value/100, remainder)
}
func (s *Service) EnqueueDue(ctx context.Context) error {
	jobDB := s.JobDB
	if jobDB == nil {
		jobDB = s.DB
	}
	rows, err := jobDB.QueryContext(ctx, `SELECT cp.organization_id,cp.id FROM competitor_products cp WHERE cp.next_check_at<=now() AND cp.match_state NOT IN ('paused','rejected') AND NOT EXISTS (SELECT 1 FROM competitor_source_runs r WHERE r.competitor_product_id=cp.id AND r.status IN ('queued','running')) LIMIT 50`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var orgID, id string
		if err := rows.Scan(&orgID, &id); err != nil {
			return err
		}
		_, _ = s.QueueCheck(ctx, orgID, id)
	}
	return rows.Err()
}

func (s *Service) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	schedule := time.NewTicker(time.Minute)
	stale := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	defer schedule.Stop()
	defer stale.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-schedule.C:
			if err := s.EnqueueDue(ctx); err != nil && s.Logger != nil {
				s.Logger.Error("competitor_schedule_failed", "error", err)
			}
		case <-stale.C:
			if err := s.MarkStale(ctx); err != nil && s.Logger != nil {
				s.Logger.Error("competitor_stale_scan_failed", "error", err)
			}
		case <-ticker.C:
			for i := 0; i < 3; i++ {
				processed, err := s.ProcessNext(ctx)
				if err != nil {
					if s.Logger != nil {
						s.Logger.Error("competitor_worker_failed", "error", err)
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
func (s *Service) MarkStale(ctx context.Context) error {
	jobDB := s.JobDB
	if jobDB == nil {
		jobDB = s.DB
	}
	rows, err := jobDB.QueryContext(ctx, `SELECT cp.organization_id,cp.id FROM competitor_products cp JOIN competitors c ON c.id=cp.competitor_id WHERE cp.match_state NOT IN ('paused','rejected','broken') AND (cp.last_observed_at IS NULL OR cp.last_observed_at<now()-((COALESCE((c.monitoring_policy->>'freshness_minutes')::int,720)) * interval '1 minute')) LIMIT 200`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var orgID, id string
		if err := rows.Scan(&orgID, &id); err != nil {
			return err
		}
		_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `UPDATE competitor_products SET match_state='stale',updated_at=now() WHERE organization_id=$1 AND id=$2 AND match_state NOT IN ('confirmed','rejected','paused')`, orgID, id)
			if e != nil {
				return e
			}
			return insertAlert(ctx, tx, orgID, id, "stale_data", "Competitor observation exceeded its freshness window.", "", "")
		})
	}
	return rows.Err()
}
