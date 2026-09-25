// Package reporting produces tenant-scoped operational reports and CSV exports.
// Every query is tenant-scoped through db.WithTenant, and report access is
// permission-gated at the HTTP layer.
package reporting

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"automation/internal/db"
	"automation/internal/pricing"
)

var ErrUnknownReport = errors.New("unknown report kind")

type Service struct {
	DB *sql.DB
}

type Report struct {
	Kind        string     `json:"kind"`
	Columns     []string   `json:"columns"`
	Rows        [][]string `json:"rows"`
	RowCount    int        `json:"row_count"`
	GeneratedAt time.Time  `json:"generated_at"`
}

type MarginRow struct {
	ProductID      string `json:"product_id"`
	ProductName    string `json:"product_name"`
	SKU            string `json:"product_sku"`
	PriceKobo      int64  `json:"price_kobo"`
	LandedCostKobo *int64 `json:"landed_cost_kobo,omitempty"`
	MarginBPS      *int64 `json:"margin_bps,omitempty"`
	MinimumKobo    *int64 `json:"minimum_profitable_price_kobo,omitempty"`
	Status         string `json:"status"`
	StockStatus    string `json:"stock_status"`
	StockQuantity  *int   `json:"stock_quantity,omitempty"`
}

// kinds is the closed set of supported reports.
var kinds = map[string]bool{
	"margin": true, "below_floor": true, "competitor_gap": true, "price_change_history": true,
	"publish_reliability": true, "competitor_health": true, "recommendation_outcomes": true, "stock_opportunity": true,
}

func ValidKind(kind string) bool { return kinds[kind] }

func (s *Service) KindList() []string {
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	return out
}

// MarginReport lists per-product price, cost, margin, and floor.
func (s *Service) MarginReport(ctx context.Context, orgID string, limit int) ([]MarginRow, error) {
	if limit < 1 || limit > 1000 {
		limit = 500
	}
	out := []MarginRow{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT p.id, p.name, p.sku, COALESCE(p.sale_price_kobo,p.price_kobo),
			c.landed_cost_kobo, p.stock_status, p.stock_quantity
			FROM products p LEFT JOIN product_costs c ON c.product_id=p.id AND c.variant_id IS NULL AND c.status='active'
			WHERE p.organization_id=$1 AND p.status='published'
			ORDER BY p.name LIMIT $2`, orgID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row MarginRow
			var cost sql.NullInt64
			if err := rows.Scan(&row.ProductID, &row.ProductName, &row.SKU, &row.PriceKobo, &cost, &row.StockStatus, &row.StockQuantity); err != nil {
				return err
			}
			if cost.Valid {
				landed := cost.Int64
				row.LandedCostKobo = &landed
				// Reuse the Phase 3 calculator so reports can never disagree
				// with the pricing engine about margin or floor.
				margin := pricing.MarginBPS(row.PriceKobo, landed)
				row.MarginBPS = &margin
				minimum, err := pricing.MinimumProfitablePrice(landed, defaultTargetMarginBPS)
				if err == nil {
					row.MinimumKobo = &minimum
					row.Status = "healthy"
					if row.PriceKobo < minimum {
						row.Status = "below_floor"
					}
				} else {
					row.Status = "guardrail_conflict"
				}
			} else {
				row.Status = "cost_required"
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}

// Build renders a report as columns plus string rows for the UI and CSV export.
func (s *Service) Build(ctx context.Context, orgID, kind string, limit int) (Report, error) {
	if !ValidKind(kind) {
		return Report{}, ErrUnknownReport
	}
	report := Report{Kind: kind, GeneratedAt: time.Now().UTC(), Rows: [][]string{}}
	switch kind {
	case "margin", "below_floor":
		rows, err := s.MarginReport(ctx, orgID, limit)
		if err != nil {
			return Report{}, err
		}
		report.Columns = []string{"product_id", "product_name", "sku", "price", "landed_cost", "margin", "minimum_price", "status", "stock_status", "stock_quantity"}
		for _, r := range rows {
			if kind == "below_floor" && r.Status != "below_floor" {
				continue
			}
			report.Rows = append(report.Rows, []string{
				r.ProductID, r.ProductName, r.SKU, Money(r.PriceKobo), Money(deref(r.LandedCostKobo)),
				Percent(deref(r.MarginBPS)), Money(deref(r.MinimumKobo)), r.Status, r.StockStatus, intOrDash(r.StockQuantity),
			})
		}
	case "competitor_gap":
		report.Columns = []string{"product_id", "product_name", "our_price", "lowest_confirmed_competitor", "gap", "gap_bps"}
		if err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT p.id, p.name, COALESCE(p.sale_price_kobo,p.price_kobo), MIN(o.observed_price_kobo)
				FROM products p
				JOIN competitor_products cp ON cp.confirmed_product_id=p.id
				JOIN LATERAL (SELECT observed_price_kobo FROM competitor_price_observations o
					WHERE o.competitor_product_id=cp.id ORDER BY o.observed_at DESC LIMIT 1) o ON TRUE
				WHERE p.organization_id=$1 AND o.observed_price_kobo IS NOT NULL
				  AND cp.organization_id=$1
				GROUP BY p.id, p.name, COALESCE(p.sale_price_kobo,p.price_kobo)
				ORDER BY p.name LIMIT $2`, orgID, limitOr(limit))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id, name string
				var ours, theirs sql.NullInt64
				if err := rows.Scan(&id, &name, &ours, &theirs); err != nil {
					return err
				}
				gap := int64(0)
				bps := int64(0)
				if ours.Valid && theirs.Valid {
					gap = ours.Int64 - theirs.Int64
					if ours.Int64 > 0 {
						bps = gap * 10000 / ours.Int64
					}
				}
				report.Rows = append(report.Rows, []string{id, name, Money(nullInt(ours)), Money(nullInt(theirs)), Money(gap), Percent(bps)})
			}
			return rows.Err()
		}); err != nil {
			return Report{}, err
		}
	case "price_change_history":
		report.Columns = []string{"created_at", "product_name", "previous_price", "requested_price", "status", "execution_status", "verified_price", "approved_role", "error"}
		if err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT r.created_at, p.name, r.previous_price_kobo, r.requested_price_kobo, r.status,
				COALESCE(e.status,''), e.verified_after_price_kobo, COALESCE(a.role_id,''), COALESCE(e.last_error,'')
				FROM price_change_requests r JOIN products p ON p.id=r.product_id
				LEFT JOIN price_change_executions e ON e.price_change_request_id=r.id
				LEFT JOIN LATERAL (SELECT role_id FROM price_change_approvals x WHERE x.price_change_request_id=r.id ORDER BY x.created_at DESC LIMIT 1) a ON TRUE
				WHERE r.organization_id=$1 ORDER BY r.created_at DESC LIMIT $2`, orgID, limitOr(limit))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var created time.Time
				var name, status, execStatus, role, errMsg string
				var prev, req int64
				var verified sql.NullInt64
				if err := rows.Scan(&created, &name, &prev, &req, &status, &execStatus, &verified, &role, &errMsg); err != nil {
					return err
				}
				report.Rows = append(report.Rows, []string{created.UTC().Format(time.RFC3339), name, Money(prev), Money(req),
					status, execStatus, Money(nullInt(verified)), role, errMsg})
			}
			return rows.Err()
		}); err != nil {
			return Report{}, err
		}
	case "publish_reliability":
		report.Columns = []string{"status", "count"}
		if err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT status, COUNT(*) FROM price_change_executions WHERE organization_id=$1 GROUP BY status ORDER BY status`, orgID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var status string
				var n int64
				if err := rows.Scan(&status, &n); err != nil {
					return err
				}
				report.Rows = append(report.Rows, []string{status, strconv.FormatInt(n, 10)})
			}
			// Approval success rate is derived, not stored, so it cannot drift.
			var approved, total int64
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE decision='approved'),
				COUNT(*) FROM price_change_approvals WHERE organization_id=$1`, orgID).Scan(&approved, &total); err != nil {
				return err
			}
			rate := "n/a"
			if total > 0 {
				rate = fmt.Sprintf("%.1f%%", float64(approved)*100/float64(total))
			}
			report.Rows = append(report.Rows, []string{"approval_success_rate", rate})
			return rows.Err()
		}); err != nil {
			return Report{}, err
		}
	case "competitor_health":
		report.Columns = []string{"competitor", "url", "match_state", "consecutive_failures", "last_observed_at", "last_error"}
		if err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT c.name, cp.url, cp.match_state, cp.consecutive_failures, cp.last_observed_at, cp.last_error
				FROM competitor_products cp JOIN competitors c ON c.id=cp.competitor_id
				WHERE cp.organization_id=$1 ORDER BY c.name LIMIT $2`, orgID, limitOr(limit))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var name, url, state, lastErr string
				var failures int
				var observed sql.NullTime
				if err := rows.Scan(&name, &url, &state, &failures, &observed, &lastErr); err != nil {
					return err
				}
				report.Rows = append(report.Rows, []string{name, url, state, strconv.Itoa(failures), timeOrDash(observed), lastErr})
			}
			return rows.Err()
		}); err != nil {
			return Report{}, err
		}
	case "recommendation_outcomes":
		report.Columns = []string{"state", "count"}
		if err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT state, COUNT(*) FROM price_recommendations WHERE organization_id=$1 GROUP BY state ORDER BY state`, orgID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var state string
				var n int64
				if err := rows.Scan(&state, &n); err != nil {
					return err
				}
				report.Rows = append(report.Rows, []string{state, strconv.FormatInt(n, 10)})
			}
			var published, approved int64
			if err := tx.QueryRowContext(ctx, `SELECT
				COUNT(*) FILTER (WHERE status='published'),
				COUNT(*) FROM price_change_requests WHERE organization_id=$1`, orgID).Scan(&published, &approved); err != nil {
				return err
			}
			rate := "n/a"
			if approved > 0 {
				rate = fmt.Sprintf("%.1f%%", float64(published)*100/float64(approved))
			}
			report.Rows = append(report.Rows, []string{"publish_conversion_rate", rate})
			return rows.Err()
		}); err != nil {
			return Report{}, err
		}
	case "stock_opportunity":
		rows, err := s.MarginReport(ctx, orgID, limit)
		if err != nil {
			return Report{}, err
		}
		report.Columns = []string{"product_id", "product_name", "sku", "price", "stock_status", "stock_quantity", "lowest_competitor", "opportunity"}
		if err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
			for _, r := range rows {
				var lowest sql.NullInt64
				if err := tx.QueryRowContext(ctx, `SELECT MIN(o.observed_price_kobo) FROM competitor_products cp
					JOIN LATERAL (SELECT observed_price_kobo FROM competitor_price_observations o
						WHERE o.competitor_product_id=cp.id ORDER BY o.observed_at DESC LIMIT 1) o ON TRUE
					WHERE cp.organization_id=$1 AND cp.confirmed_product_id=$2`, orgID, r.ProductID).Scan(&lowest); err != nil {
					return err
				}
				opportunity := int64(0)
				if lowest.Valid && lowest.Int64 > r.PriceKobo {
					opportunity = lowest.Int64 - r.PriceKobo
				}
				report.Rows = append(report.Rows, []string{r.ProductID, r.ProductName, r.SKU, Money(r.PriceKobo),
					r.StockStatus, intOrDash(r.StockQuantity), Money(nullInt(lowest)), Money(opportunity)})
			}
			return nil
		}); err != nil {
			return Report{}, err
		}
	}
	report.RowCount = len(report.Rows)
	return report, nil
}

// CSV renders a report as RFC 4180 CSV with formula-injection protection.
func (r Report) CSV() ([]byte, error) {
	var b strings.Builder
	writer := csv.NewWriter(&b)
	if err := writer.Write(r.Columns); err != nil {
		return nil, err
	}
	for _, row := range r.Rows {
		safe := make([]string, len(row))
		for i, cell := range row {
			safe[i] = SanitizeCell(cell)
		}
		if err := writer.Write(safe); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return []byte(b.String()), writer.Error()
}

// dangerousCellPrefixes trigger formula execution in spreadsheet software.
var dangerousCellPrefixes = []string{"=", "+", "-", "@", "\t", "\r"}

// SanitizeCell neutralises CSV formula injection.
func SanitizeCell(cell string) string {
	trimmed := strings.TrimLeft(cell, " ")
	if trimmed == "" {
		return cell
	}
	for _, prefix := range dangerousCellPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return "'" + cell
		}
	}
	return cell
}

// RecordExport persists that an export was produced for auditability.
func (s *Service) RecordExport(ctx context.Context, orgID, actor, kind string, rowCount int) (string, error) {
	if !ValidKind(kind) {
		return "", ErrUnknownReport
	}
	var id string
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO report_exports (organization_id, report_kind, row_count, requested_by_user_id)
			VALUES ($1,$2,$3,NULLIF($4,'')::uuid) RETURNING id::text`, orgID, kind, rowCount, actor).Scan(&id)
	})
	return id, err
}

const defaultTargetMarginBPS int64 = 2500

func limitOr(limit int) int {
	if limit < 1 || limit > 2000 {
		return 1000
	}
	return limit
}

func deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// nullInt renders a nullable integer for a report cell.
func nullInt(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}

func intOrDash(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func timeOrDash(v sql.NullTime) string {
	if !v.Valid {
		return ""
	}
	return v.Time.UTC().Format(time.RFC3339)
}

func Money(k int64) string {
	neg := k < 0
	if neg {
		k = -k
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, k/100, k%100)
}

func Percent(bps int64) string {
	if bps == 0 {
		return ""
	}
	return fmt.Sprintf("%.2f%%", float64(bps)/100)
}
