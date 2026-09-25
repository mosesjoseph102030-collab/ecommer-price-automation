package pricing

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/db"
)

var ErrNotFound = errors.New("record not found")

type Service struct{ DB *sql.DB }

type CostInput struct {
	ProductID     string         `json:"product_id"`
	VariantID     string         `json:"variant_id,omitempty"`
	Currency      string         `json:"currency"`
	Cost          CostComponents `json:"cost"`
	EffectiveFrom *time.Time     `json:"effective_from,omitempty"`
}

type CostRecord struct {
	ID             string         `json:"id"`
	ProductID      string         `json:"product_id"`
	VariantID      string         `json:"variant_id,omitempty"`
	Currency       string         `json:"currency"`
	Cost           CostComponents `json:"cost"`
	LandedCostKobo int64          `json:"landed_cost_kobo"`
	EffectiveFrom  time.Time      `json:"effective_from"`
	EffectiveTo    *time.Time     `json:"effective_to,omitempty"`
	Status         string         `json:"status"`
	Version        int            `json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
}

type PricePolicy struct {
	ID                    string `json:"id"`
	ScopeType             string `json:"scope_type"`
	ProductID             string `json:"product_id,omitempty"`
	VariantID             string `json:"variant_id,omitempty"`
	CategoryID            string `json:"category_id,omitempty"`
	MinimumPriceKobo      *int64 `json:"minimum_price_kobo,omitempty"`
	MaximumPriceKobo      *int64 `json:"maximum_price_kobo,omitempty"`
	RoundingIncrementKobo int64  `json:"rounding_increment_kobo"`
}

type LockRecord struct {
	ProductID string    `json:"product_id"`
	VariantID string    `json:"variant_id,omitempty"`
	Locked    bool      `json:"locked"`
	Reason    string    `json:"reason"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Service) SetCost(ctx context.Context, orgID, actor string, input CostInput) (CostRecord, error) {
	if err := input.Cost.Validate(); err != nil {
		return CostRecord{}, err
	}
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.Currency == "" {
		input.Currency = "NGN"
	}
	if len(input.Currency) != 3 {
		return CostRecord{}, errors.New("currency must be a three-letter code")
	}
	if input.EffectiveFrom == nil {
		now := time.Now().UTC()
		input.EffectiveFrom = &now
	}
	if input.EffectiveFrom.After(time.Now().UTC().Add(time.Minute)) {
		return CostRecord{}, errors.New("future cost activation is not supported yet")
	}
	var out CostRecord
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var organizationCurrency string
		if err := tx.QueryRowContext(ctx, `SELECT currency FROM organizations WHERE id=$1`, orgID).Scan(&organizationCurrency); err != nil {
			return err
		}
		if !strings.EqualFold(organizationCurrency, input.Currency) {
			return errors.New("cost currency must match the organization currency")
		}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE organization_id=$1 AND id=$2`, orgID, input.ProductID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		if input.VariantID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_variants WHERE organization_id=$1 AND id=$2 AND product_id=$3`, orgID, input.VariantID, input.ProductID).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				return errors.New("variant does not belong to product")
			}
		}
		var version int
		var previousLanded sql.NullInt64
		var previousFrom sql.NullTime
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1,
			(SELECT landed_cost_kobo FROM product_costs WHERE organization_id=$1 AND product_id=$2 AND variant_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND status='active' LIMIT 1),
			(SELECT effective_from FROM product_costs WHERE organization_id=$1 AND product_id=$2 AND variant_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND status='active' LIMIT 1)
			FROM product_costs WHERE organization_id=$1 AND product_id=$2 AND variant_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid`, orgID, input.ProductID, input.VariantID).Scan(&version, &previousLanded, &previousFrom)
		if previousFrom.Valid && !input.EffectiveFrom.After(previousFrom.Time) {
			return errors.New("effective date must be later than the active cost")
		}
		_, err := tx.ExecContext(ctx, `UPDATE product_costs SET status='superseded', effective_to=$4, updated_at=now()
			WHERE organization_id=$1 AND product_id=$2 AND variant_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND status='active'`, orgID, input.ProductID, input.VariantID, *input.EffectiveFrom)
		if err != nil {
			return err
		}
		landed := input.Cost.LandedKobo()
		if err := tx.QueryRowContext(ctx, `INSERT INTO product_costs (organization_id, product_id, variant_id, currency,
			supplier_cost_kobo, shipping_cost_kobo, packaging_cost_kobo, payment_fee_kobo, tax_import_cost_kobo,
			other_allocated_cost_kobo, landed_cost_kobo, effective_from, status, version, created_by_user_id)
			VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,'active',$13,$14) RETURNING id, created_at`,
			orgID, input.ProductID, input.VariantID, input.Currency, input.Cost.SupplierKobo, input.Cost.ShippingKobo,
			input.Cost.PackagingKobo, input.Cost.PaymentFeeKobo, input.Cost.TaxImportKobo, input.Cost.OtherKobo,
			landed, *input.EffectiveFrom, version, actor).Scan(&out.ID, &out.CreatedAt); err != nil {
			return err
		}
		components := []struct {
			kind   string
			amount int64
		}{
			{"supplier", input.Cost.SupplierKobo}, {"shipping", input.Cost.ShippingKobo}, {"packaging", input.Cost.PackagingKobo},
			{"payment_fee", input.Cost.PaymentFeeKobo}, {"tax_import", input.Cost.TaxImportKobo}, {"other", input.Cost.OtherKobo},
		}
		for _, component := range components {
			if _, err := tx.ExecContext(ctx, `INSERT INTO product_cost_components (organization_id, product_cost_id, component_type, amount_kobo) VALUES ($1,$2,$3,$4)`, orgID, out.ID, component.kind, component.amount); err != nil {
				return err
			}
		}
		out = costFromInput(out, input, landed, version)
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "product.cost.updated", ResourceType: "product_cost", ResourceID: out.ID, OldState: nullableKobo(previousLanded), NewState: map[string]any{"landed_cost_kobo": landed, "version": version}})
	})
	return out, err
}

func nullableKobo(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return map[string]int64{"landed_cost_kobo": value.Int64}
}

func costFromInput(out CostRecord, input CostInput, landed int64, version int) CostRecord {
	out.ProductID, out.VariantID, out.Currency, out.Cost, out.LandedCostKobo = input.ProductID, input.VariantID, input.Currency, input.Cost, landed
	out.EffectiveFrom, out.Status, out.Version = *input.EffectiveFrom, "active", version
	return out
}

func (s *Service) ListCostVersions(ctx context.Context, orgID, productID, variantID string, limit int) ([]CostRecord, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	out := []CostRecord{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, product_id, variant_id, currency, supplier_cost_kobo, shipping_cost_kobo,
			packaging_cost_kobo, payment_fee_kobo, tax_import_cost_kobo, other_allocated_cost_kobo, landed_cost_kobo,
			effective_from, effective_to, status, version, created_at FROM product_costs WHERE organization_id=$1 AND product_id=$2
			AND variant_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid ORDER BY version DESC LIMIT $4`, orgID, productID, variantID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var record CostRecord
			var variant sql.NullString
			if err := rows.Scan(&record.ID, &record.ProductID, &variant, &record.Currency, &record.Cost.SupplierKobo, &record.Cost.ShippingKobo, &record.Cost.PackagingKobo, &record.Cost.PaymentFeeKobo, &record.Cost.TaxImportKobo, &record.Cost.OtherKobo, &record.LandedCostKobo, &record.EffectiveFrom, &record.EffectiveTo, &record.Status, &record.Version, &record.CreatedAt); err != nil {
				return err
			}
			if variant.Valid {
				record.VariantID = variant.String
			}
			out = append(out, record)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) GetCost(ctx context.Context, orgID, productID, variantID string) (CostRecord, error) {
	var out CostRecord
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var variant sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT id, product_id, variant_id, currency, supplier_cost_kobo, shipping_cost_kobo,
			packaging_cost_kobo, payment_fee_kobo, tax_import_cost_kobo, other_allocated_cost_kobo, landed_cost_kobo,
			effective_from, effective_to, status, version, created_at FROM product_costs
			WHERE organization_id=$1 AND product_id=$2 AND variant_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND status='active'`, orgID, productID, variantID).
			Scan(&out.ID, &out.ProductID, &variant, &out.Currency, &out.Cost.SupplierKobo, &out.Cost.ShippingKobo,
				&out.Cost.PackagingKobo, &out.Cost.PaymentFeeKobo, &out.Cost.TaxImportKobo, &out.Cost.OtherKobo,
				&out.LandedCostKobo, &out.EffectiveFrom, &out.EffectiveTo, &out.Status, &out.Version, &out.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if variant.Valid {
			out.VariantID = variant.String
		}
		return nil
	})
	return out, err
}
