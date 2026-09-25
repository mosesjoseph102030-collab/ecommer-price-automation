package woocommerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"automation/internal/db"
)

type ProductRecord struct {
	ID                string     `json:"id"`
	ExternalID        string     `json:"external_id"`
	SKU               string     `json:"sku"`
	Name              string     `json:"name"`
	Status            string     `json:"status"`
	StockStatus       string     `json:"stock_status"`
	StockQuantity     *int       `json:"stock_quantity"`
	PriceKobo         int64      `json:"price_kobo"`
	SalePriceKobo     *int64     `json:"sale_price_kobo"`
	PlatformPriceKobo *int64     `json:"platform_price_kobo"`
	PriceConflict     bool       `json:"price_conflict"`
	PriceConflictAt   *time.Time `json:"price_conflict_detected_at"`
	NeedsReview       bool       `json:"needs_review"`
	ReviewReason      string     `json:"review_reason"`
	TaxClass          string     `json:"tax_class"`
	WooUpdatedAt      *time.Time `json:"woo_updated_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	VariantCount      int        `json:"variant_count"`
	MissingSKU        bool       `json:"missing_sku"`
	Categories        []string   `json:"categories"`
}

type VariantRecord struct {
	ID              string            `json:"id"`
	ExternalID      string            `json:"external_id"`
	SKU             string            `json:"sku"`
	PriceKobo       int64             `json:"price_kobo"`
	SalePriceKobo   *int64            `json:"sale_price_kobo"`
	StockStatus     string            `json:"stock_status"`
	StockQuantity   *int              `json:"stock_quantity"`
	Attributes      map[string]string `json:"attributes"`
	MappingConflict bool              `json:"mapping_conflict"`
	MappingDetail   string            `json:"mapping_conflict_detail"`
}

type ProductPage struct {
	Products   []ProductRecord `json:"products"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type ProductDetail struct {
	ProductRecord
	Variants []VariantRecord `json:"variants"`
}

func (s *Service) ListProducts(ctx context.Context, orgID, search, status, after string, limit int) (ProductPage, error) {
	allowedStatus := map[string]bool{"published": true, "draft": true, "private": true, "archived": true, "deleted": true}
	if status != "" && !allowedStatus[status] {
		return ProductPage{}, errors.New("invalid product status filter")
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	args := []any{orgID}
	query := `
		SELECT p.id, p.external_id, COALESCE(p.sku,''), p.name, p.status, p.stock_status, p.stock_quantity,
		       p.price_kobo, p.sale_price_kobo, p.platform_price_kobo, p.price_conflict, p.price_conflict_detected_at,
		       p.needs_review, p.review_reason, p.tax_class, p.woo_updated_at, p.last_seen_at,
		       (SELECT COUNT(*) FROM product_variants v WHERE v.product_id=p.id),
		       COALESCE((SELECT array_agg(c.name ORDER BY c.name) FROM product_category_memberships pc
		                 JOIN product_categories c ON c.id=pc.category_id WHERE pc.product_id=p.id), '{}')
		FROM products p WHERE p.organization_id=$1`
	if search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		query += " AND (lower(p.name) LIKE $" + strconv.Itoa(len(args)) + " OR lower(p.sku) LIKE $" + strconv.Itoa(len(args)) + ")"
	}
	if status != "" {
		args = append(args, status)
		query += " AND p.status=$" + strconv.Itoa(len(args))
	}
	if after != "" {
		args = append(args, after)
		query += " AND p.id>$" + strconv.Itoa(len(args)) + "::uuid"
	}
	args = append(args, limit+1)
	query += " ORDER BY p.id LIMIT $" + strconv.Itoa(len(args))
	out := ProductPage{Products: []ProductRecord{}}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p ProductRecord
			var sale, platform sql.NullInt64
			if err := rows.Scan(&p.ID, &p.ExternalID, &p.SKU, &p.Name, &p.Status, &p.StockStatus, &p.StockQuantity,
				&p.PriceKobo, &sale, &platform, &p.PriceConflict, &p.PriceConflictAt, &p.NeedsReview, &p.ReviewReason, &p.TaxClass, &p.WooUpdatedAt,
				&p.LastSeenAt, &p.VariantCount, &p.Categories); err != nil {
				return err
			}
			if sale.Valid {
				p.SalePriceKobo = &sale.Int64
			}
			if platform.Valid {
				p.PlatformPriceKobo = &platform.Int64
			}
			p.MissingSKU = strings.TrimSpace(p.SKU) == ""
			out.Products = append(out.Products, p)
		}
		return rows.Err()
	})
	if err != nil {
		return ProductPage{}, err
	}
	if len(out.Products) > limit {
		out.NextCursor = out.Products[limit-1].ID
		out.Products = out.Products[:limit]
	}
	return out, nil
}

func (s *Service) GetProduct(ctx context.Context, orgID, id string) (ProductDetail, error) {
	var out ProductDetail
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var p ProductRecord
		var sale, platform sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT id, external_id, COALESCE(sku,''), name, status, stock_status, stock_quantity,
			price_kobo, sale_price_kobo, platform_price_kobo, price_conflict, price_conflict_detected_at,
			needs_review, review_reason, tax_class, woo_updated_at, last_seen_at FROM products WHERE organization_id=$1 AND id=$2`, orgID, id).
			Scan(&p.ID, &p.ExternalID, &p.SKU, &p.Name, &p.Status, &p.StockStatus, &p.StockQuantity, &p.PriceKobo,
				&sale, &platform, &p.PriceConflict, &p.PriceConflictAt, &p.NeedsReview, &p.ReviewReason, &p.TaxClass, &p.WooUpdatedAt, &p.LastSeenAt)
		if err != nil {
			return err
		}
		if sale.Valid {
			p.SalePriceKobo = &sale.Int64
		}
		if platform.Valid {
			p.PlatformPriceKobo = &platform.Int64
		}
		p.MissingSKU = p.SKU == ""
		variants := []VariantRecord{}
		rows, err := tx.QueryContext(ctx, `SELECT id, external_id, COALESCE(sku,''), price_kobo, sale_price_kobo,
			stock_status, stock_quantity, attributes, mapping_conflict, mapping_conflict_detail
			FROM product_variants WHERE organization_id=$1 AND product_id=$2 ORDER BY id`, orgID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v VariantRecord
			var vSale sql.NullInt64
			var raw []byte
			if err := rows.Scan(&v.ID, &v.ExternalID, &v.SKU, &v.PriceKobo, &vSale, &v.StockStatus, &v.StockQuantity, &raw, &v.MappingConflict, &v.MappingDetail); err != nil {
				return err
			}
			if vSale.Valid {
				v.SalePriceKobo = &vSale.Int64
			}
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &v.Attributes)
			}
			variants = append(variants, v)
		}
		out = ProductDetail{ProductRecord: p, Variants: variants}
		return rows.Err()
	})
	return out, err
}
