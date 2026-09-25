package woocommerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"automation/internal/db"
)

type SyncRun struct {
	ID               string     `json:"id"`
	StoreID          string     `json:"store_id"`
	Kind             string     `json:"kind"`
	Status           string     `json:"status"`
	ImportedProducts int        `json:"imported_products"`
	ImportedVariants int        `json:"imported_variants"`
	FailedItems      int        `json:"failed_items"`
	ErrorSummary     string     `json:"error_summary"`
	Attempts         int        `json:"attempts"`
	NextAttemptAt    *time.Time `json:"next_attempt_at"`
	StartedAt        *time.Time `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

type SyncItem struct {
	ID         string    `json:"id"`
	ExternalID string    `json:"external_id"`
	SKU        string    `json:"sku"`
	Status     string    `json:"status"`
	Error      string    `json:"error"`
	CreatedAt  time.Time `json:"created_at"`
}

const syncRunSelect = `SELECT id, store_id, kind, status, imported_products, imported_variants, failed_items,
error_summary, attempts, next_attempt_at, started_at, finished_at, created_at FROM store_sync_runs`

func scanSyncRun(row rowScanner) (*SyncRun, error) {
	var r SyncRun
	err := row.Scan(&r.ID, &r.StoreID, &r.Kind, &r.Status, &r.ImportedProducts, &r.ImportedVariants,
		&r.FailedItems, &r.ErrorSummary, &r.Attempts, &r.NextAttemptAt, &r.StartedAt, &r.FinishedAt, &r.CreatedAt)
	return &r, err
}

func (s *Service) ListSyncRuns(ctx context.Context, orgID, storeID string, limit int) ([]SyncRun, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	out := []SyncRun{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := syncRunSelect + ` WHERE organization_id=$1`
		args := []any{orgID}
		if storeID != "" {
			query += ` AND store_id=$2`
			args = append(args, storeID)
		}
		query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, limit)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanSyncRun(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) GetSyncRun(ctx context.Context, orgID, runID string) (*SyncRun, []SyncItem, error) {
	var run *SyncRun
	items := []SyncItem{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		r, err := scanSyncRun(tx.QueryRowContext(ctx, syncRunSelect+` WHERE organization_id=$1 AND id=$2`, orgID, runID))
		if err != nil {
			return err
		}
		run = r
		rows, err := tx.QueryContext(ctx, `SELECT id, external_id, sku, status, error, created_at FROM store_sync_items WHERE sync_run_id=$1 ORDER BY created_at LIMIT 500`, runID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var i SyncItem
			if err := rows.Scan(&i.ID, &i.ExternalID, &i.SKU, &i.Status, &i.Error, &i.CreatedAt); err != nil {
				return err
			}
			items = append(items, i)
		}
		return rows.Err()
	})
	return run, items, err
}

// ProcessNext claims and processes one queued sync. The worker process calls it.
func (s *Service) ProcessNext(ctx context.Context) (bool, error) {
	var run *SyncRun
	var orgID string
	// The worker uses a dedicated job role in production. Query one job atomically.
	jobDB := s.JobDB
	if jobDB == nil {
		jobDB = s.DB
	}
	tx, err := jobDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM store_sync_runs
		WHERE status='queued' AND (next_attempt_at IS NULL OR next_attempt_at <= now())
		ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, `UPDATE store_sync_runs SET status='running', started_at=now(), attempts=attempts+1 WHERE id=$1 RETURNING organization_id`, id).Scan(&orgID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	run, err = s.GetRunForWorker(ctx, id)
	if err != nil {
		return false, err
	}
	s.runSync(ctx, orgID, run)
	return true, nil
}

func (s *Service) GetRunForWorker(ctx context.Context, runID string) (*SyncRun, error) {
	jobDB := s.JobDB
	if jobDB == nil {
		jobDB = s.DB
	}
	r, err := scanSyncRun(jobDB.QueryRowContext(ctx, syncRunSelect+` WHERE id=$1`, runID))
	return r, err
}

func (s *Service) runSync(ctx context.Context, orgID string, run *SyncRun) {
	conn, _, client, err := s.load(ctx, orgID, "")
	if err != nil {
		s.finishSync(ctx, orgID, run.ID, "failed", 0, 0, 1, safeError(err))
		return
	}
	if conn.StoreID != run.StoreID || conn.Status == "disconnected" || conn.Status == "revoked" {
		s.finishSync(ctx, orgID, run.ID, "failed", 0, 0, 1, "Store is disconnected. Reconnect before syncing.")
		return
	}

	products, variants, failed, syncErr := s.fetchAndPersist(ctx, orgID, run, client)
	if syncErr != nil && failed == 0 {
		if IsAuthError(syncErr) || IsRateLimited(syncErr) {
			s.markConnectionError(ctx, orgID, conn.ID, syncErr)
		}
		if run.Attempts < 3 && isRetryableSyncError(syncErr) {
			s.scheduleRetry(ctx, orgID, run.ID, run.Attempts, syncErr)
			return
		}
		s.finishSync(ctx, orgID, run.ID, "failed", 0, 0, 1, safeError(syncErr))
		return
	}
	status := "succeeded"
	if failed > 0 {
		status = "partial"
	}
	summary := ""
	if syncErr != nil {
		summary = safeError(syncErr)
	}
	s.finishSync(ctx, orgID, run.ID, status, products, variants, failed, summary)
	_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE store_connections SET last_sync_at=now(), status='connected', last_error='', updated_at=now() WHERE id=$1`, conn.ID)
		return err
	})
}

func (s *Service) fetchAndPersist(ctx context.Context, orgID string, run *SyncRun, client *Client) (int, int, int, error) {
	categories, err := client.ListCategories(ctx)
	if err != nil {
		return 0, 0, 1, err
	}
	if err := s.persistCategories(ctx, orgID, categories); err != nil {
		return 0, 0, 1, err
	}
	modifiedAfter := ""
	if run.Kind == "webhook" {
		var t sql.NullTime
		_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT sc.last_sync_at FROM store_connections sc WHERE sc.store_id=$1 ORDER BY sc.updated_at DESC LIMIT 1`, run.StoreID).Scan(&t)
		})
		if t.Valid {
			// Overlap by two minutes so boundary updates are not missed.
			modifiedAfter = t.Time.UTC().Add(-2 * time.Minute).Format(time.RFC3339)
		}
	}
	products, err := client.ListProducts(ctx, modifiedAfter)
	if err != nil {
		return 0, 0, 1, err
	}
	productCount, variantCount, failed := 0, 0, 0
	for _, p := range products {
		pc, vc, err := s.persistProduct(ctx, orgID, run.ID, run.StoreID, client, p)
		productCount += pc
		variantCount += vc
		if err != nil {
			failed++
			_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
				_, e := tx.ExecContext(ctx, `INSERT INTO store_sync_items (organization_id, sync_run_id, external_id, sku, status, error) VALUES ($1,$2,$3,$4,'failed',$5)`, orgID, run.ID, fmt.Sprint(p.ID), p.SKU, safeError(err))
				return e
			})
			continue
		}
		_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `INSERT INTO store_sync_items (organization_id, sync_run_id, external_id, sku, status) VALUES ($1,$2,$3,$4,'ok')`, orgID, run.ID, fmt.Sprint(p.ID), p.SKU)
			return e
		})
	}
	if failed == 0 && run.Kind != "webhook" {
		_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `UPDATE products SET status='deleted', last_seen_at=now(), updated_at=now()
				WHERE organization_id=$1 AND store_id=$2 AND status <> 'deleted'
				  AND (last_sync_run_id IS NULL OR last_sync_run_id <> $3)`, orgID, run.StoreID, run.ID)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE product_external_mappings SET conflict='upstream_deleted', updated_at=now()
				WHERE organization_id=$1 AND product_id IN (
				  SELECT id FROM products WHERE organization_id=$1 AND store_id=$2 AND status='deleted'
				) AND conflict=''`, orgID, run.StoreID)
			return err
		})
	}
	return productCount, variantCount, failed, nil
}

func (s *Service) persistCategories(ctx context.Context, orgID string, terms []Term) error {
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		for _, term := range terms {
			if strings.TrimSpace(term.Name) == "" {
				continue
			}
			_, err := tx.ExecContext(ctx, `
				INSERT INTO product_categories (organization_id, external_id, name, slug) VALUES ($1,$2,$3,$4)
				ON CONFLICT (organization_id, external_id) DO UPDATE SET name=EXCLUDED.name, slug=EXCLUDED.slug, updated_at=now()`,
				orgID, fmt.Sprint(term.ID), term.Name, term.Slug)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) persistProduct(ctx context.Context, orgID, runID, storeID string, client *Client, p Product) (int, int, error) {
	priceRaw := p.Price
	if priceRaw == "" {
		priceRaw = p.RegularPrice
	}
	price, err := MoneyToKobo(priceRaw)
	if err != nil {
		return 0, 0, err
	}
	var sale any
	if p.SalePrice != "" {
		v, err := MoneyToKobo(p.SalePrice)
		if err != nil {
			return 0, 0, err
		}
		sale = v
	}
	status := normalizeProductStatus(p.Status)
	reviewReason := ""
	if price == 0 {
		reviewReason = "missing_price"
	} else if strings.TrimSpace(p.SKU) == "" {
		reviewReason = "missing_sku"
	}
	var wooUpdated any
	if t, err := time.Parse(time.RFC3339, p.DateModified); err == nil {
		wooUpdated = t.UTC()
	}
	var productID string
	var oldPrice sql.NullInt64
	var platformPrice sql.NullInt64
	err = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT id, price_kobo, platform_price_kobo FROM products WHERE organization_id=$1 AND external_id=$2`, orgID, fmt.Sprint(p.ID)).Scan(&productID, &oldPrice, &platformPrice); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		conflict := platformPrice.Valid && platformPrice.Int64 != price
		err := tx.QueryRowContext(ctx, `
			INSERT INTO products (organization_id, store_id, external_id, sku, name, status, stock_status, stock_quantity,
				price_kobo, sale_price_kobo, tax_class, woo_updated_at, last_seen_at, price_conflict, price_conflict_detected_at,
				needs_review, review_reason, last_sync_run_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now(),$13,CASE WHEN $13::timestamptz IS NULL THEN NULL ELSE now() END,$14,$15,$16)
			ON CONFLICT (organization_id, external_id) DO UPDATE SET store_id=EXCLUDED.store_id, sku=EXCLUDED.sku, name=EXCLUDED.name,
				status=EXCLUDED.status, stock_status=EXCLUDED.stock_status, stock_quantity=EXCLUDED.stock_quantity,
				price_kobo=EXCLUDED.price_kobo, sale_price_kobo=EXCLUDED.sale_price_kobo, tax_class=EXCLUDED.tax_class,
				woo_updated_at=EXCLUDED.woo_updated_at, last_sync_run_id=EXCLUDED.last_sync_run_id,
				last_seen_at=now(), price_conflict=EXCLUDED.price_conflict,
				price_conflict_detected_at=EXCLUDED.price_conflict_detected_at, needs_review=EXCLUDED.needs_review,
				review_reason=EXCLUDED.review_reason, updated_at=now()
			RETURNING id`, orgID, storeID, fmt.Sprint(p.ID), p.SKU, p.Name, status, p.StockStatus, p.StockQuantity,
			price, sale, p.TaxClass, wooUpdated, wooUpdated, conflict, reviewReason != "", reviewReason, runID).Scan(&productID)
		if err != nil {
			return err
		}
		if oldPrice.Valid && oldPrice.Int64 != price {
			_, err = tx.ExecContext(ctx, `INSERT INTO product_price_snapshots (organization_id, product_id, price_kobo, source, observed_at) VALUES ($1,$2,$3,'woo',now())`, orgID, productID, oldPrice.Int64)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO product_external_mappings (organization_id, external_product_id, product_id, conflict)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (organization_id, external_product_id, external_variant_id) DO UPDATE SET product_id=EXCLUDED.product_id, conflict=EXCLUDED.conflict, updated_at=now()`,
			orgID, fmt.Sprint(p.ID), productID, conflictText(conflict))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM product_category_memberships WHERE product_id=$1`, productID)
		if err != nil {
			return err
		}
		for _, category := range p.Categories {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO product_category_memberships (organization_id, product_id, category_id)
				SELECT $1,$2,id FROM product_categories WHERE organization_id=$1 AND external_id=$3
				ON CONFLICT DO NOTHING`, orgID, productID, fmt.Sprint(category.ID))
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO product_stock_snapshots (organization_id, product_id, quantity, stock_status) VALUES ($1,$2,$3,$4)`, orgID, productID, p.StockQuantity, p.StockStatus)
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	variantCount := 0
	if p.Type == "variable" && status != "deleted" {
		variants, err := client.ListVariations(ctx, p.ID)
		if err != nil {
			return 1, variantCount, err
		}
		for _, v := range variants {
			if err := s.persistVariant(ctx, orgID, productID, p.ID, v); err != nil {
				return 1, variantCount, err
			}
			variantCount++
		}
	}
	return 1, variantCount, nil
}

func (s *Service) persistVariant(ctx context.Context, orgID, productID string, externalProductID int64, v Variant) error {
	priceRaw := v.Price
	if priceRaw == "" {
		priceRaw = v.RegularPrice
	}
	price, err := MoneyToKobo(priceRaw)
	if err != nil {
		return err
	}
	var sale any
	if v.SalePrice != "" {
		saleValue, err := MoneyToKobo(v.SalePrice)
		if err != nil {
			return err
		}
		sale = saleValue
	}
	attrs := map[string]string{}
	for _, a := range v.Attributes {
		if a.Name != "" {
			attrs[a.Name] = a.Option
		}
	}
	attrJSON, _ := json.Marshal(attrs)
	var wooUpdated any
	if t, err := time.Parse(time.RFC3339, v.DateModified); err == nil {
		wooUpdated = t.UTC()
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var oldProductID string
		var oldPrice sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT product_id, price_kobo FROM product_variants WHERE organization_id=$1 AND external_id=$2`, orgID, fmt.Sprint(v.ID)).Scan(&oldProductID, &oldPrice)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		mappingConflict := err == nil && oldProductID != productID
		var variantID string
		err = tx.QueryRowContext(ctx, `
			INSERT INTO product_variants (organization_id, product_id, external_id, sku, price_kobo, sale_price_kobo,
				stock_status, stock_quantity, attributes, mapping_conflict, mapping_conflict_detail, woo_updated_at, last_seen_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now())
			ON CONFLICT (organization_id, external_id) DO UPDATE SET product_id=EXCLUDED.product_id, sku=EXCLUDED.sku,
				price_kobo=EXCLUDED.price_kobo, sale_price_kobo=EXCLUDED.sale_price_kobo, stock_status=EXCLUDED.stock_status,
				stock_quantity=EXCLUDED.stock_quantity, attributes=EXCLUDED.attributes, mapping_conflict=EXCLUDED.mapping_conflict,
				mapping_conflict_detail=EXCLUDED.mapping_conflict_detail, woo_updated_at=EXCLUDED.woo_updated_at, last_seen_at=now(), updated_at=now()
			RETURNING id`, orgID, productID, fmt.Sprint(v.ID), v.SKU, price, sale, v.StockStatus, v.StockQuantity, attrJSON,
			mappingConflict, conflictText(mappingConflict), wooUpdated).Scan(&variantID)
		if err != nil {
			return err
		}
		if oldPrice.Valid && oldPrice.Int64 != price {
			_, err = tx.ExecContext(ctx, `INSERT INTO product_price_snapshots (organization_id, variant_id, price_kobo, source) VALUES ($1,$2,$3,'woo')`, orgID, variantID, oldPrice.Int64)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO product_external_mappings (organization_id, external_product_id, external_variant_id, product_id, variant_id, conflict)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (organization_id, external_product_id, external_variant_id) DO UPDATE SET
				product_id=EXCLUDED.product_id, variant_id=EXCLUDED.variant_id, conflict=EXCLUDED.conflict, updated_at=now()`,
			orgID, fmt.Sprint(externalProductID), fmt.Sprint(v.ID), productID, variantID, conflictText(mappingConflict))
		return err
	})
}

func normalizeProductStatus(status string) string {
	switch status {
	case "publish", "published":
		return "published"
	case "private":
		return "private"
	case "draft", "pending":
		return "draft"
	case "trash", "delete", "deleted":
		return "deleted"
	default:
		return "archived"
	}
}

func conflictText(conflict bool) string {
	if conflict {
		return "manual_price_change"
	}
	return ""
}

func isRetryableSyncError(err error) bool {
	if err == nil || IsAuthError(err) {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status == http.StatusRequestTimeout || ae.Status == http.StatusTooManyRequests || ae.Status >= 500 || ae.Malformed
	}
	return true
}

func (s *Service) scheduleRetry(ctx context.Context, orgID, runID string, attempt int, cause error) {
	delay := time.Duration(30*time.Second) * time.Duration(1<<(attempt-1))
	var ae *APIError
	if errors.As(cause, &ae) && ae.RetryAfter > delay {
		delay = ae.RetryAfter
	}
	_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE store_sync_runs SET status='queued', error_summary=$2,
			next_attempt_at=now() + ($3 * interval '1 millisecond') WHERE id=$1`, runID, safeError(cause), delay.Milliseconds())
		return err
	})
}

func (s *Service) finishSync(ctx context.Context, orgID, runID, status string, products, variants, failed int, summary string) {
	_ = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE store_sync_runs SET status=$2, imported_products=$3, imported_variants=$4, failed_items=$5,
			error_summary=$6, finished_at=now() WHERE id=$1`, runID, status, products, variants, failed, summary)
		return err
	})
}
