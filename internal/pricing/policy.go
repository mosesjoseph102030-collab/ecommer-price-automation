package pricing

import (
	"context"
	"database/sql"
	"errors"

	"automation/internal/audit"
	"automation/internal/db"
)

type PolicyInput struct {
	ScopeType             string `json:"scope_type"`
	ProductID             string `json:"product_id,omitempty"`
	VariantID             string `json:"variant_id,omitempty"`
	CategoryID            string `json:"category_id,omitempty"`
	MinimumPriceKobo      *int64 `json:"minimum_price_kobo,omitempty"`
	MaximumPriceKobo      *int64 `json:"maximum_price_kobo,omitempty"`
	RoundingIncrementKobo int64  `json:"rounding_increment_kobo"`
}

func (s *Service) UpsertPolicy(ctx context.Context, orgID, actor string, input PolicyInput) (PricePolicy, error) {
	if input.MinimumPriceKobo == nil && input.MaximumPriceKobo == nil {
		return PricePolicy{}, errors.New("minimum or maximum price is required")
	}
	if input.MinimumPriceKobo != nil && *input.MinimumPriceKobo < 0 || input.MaximumPriceKobo != nil && *input.MaximumPriceKobo < 0 {
		return PricePolicy{}, errors.New("policy prices cannot be negative")
	}
	if input.MinimumPriceKobo != nil && input.MaximumPriceKobo != nil && *input.MaximumPriceKobo < *input.MinimumPriceKobo {
		return PricePolicy{}, ErrFloorAboveCeiling
	}
	if input.RoundingIncrementKobo <= 0 {
		input.RoundingIncrementKobo = 100
	}
	switch input.ScopeType {
	case "product":
		if input.ProductID == "" {
			return PricePolicy{}, errors.New("product_id is required")
		}
	case "variant":
		if input.VariantID == "" {
			return PricePolicy{}, errors.New("variant_id is required")
		}
	case "category":
		if input.CategoryID == "" {
			return PricePolicy{}, errors.New("category_id is required")
		}
	case "store":
	default:
		return PricePolicy{}, errors.New("invalid scope_type")
	}
	out := PricePolicy{ScopeType: input.ScopeType, ProductID: input.ProductID, VariantID: input.VariantID, CategoryID: input.CategoryID, MinimumPriceKobo: input.MinimumPriceKobo, MaximumPriceKobo: input.MaximumPriceKobo, RoundingIncrementKobo: input.RoundingIncrementKobo}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM product_price_policies WHERE organization_id=$1 AND scope_type=$2 AND product_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND variant_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid AND category_id IS NOT DISTINCT FROM NULLIF($5,'')::uuid`, orgID, input.ScopeType, input.ProductID, input.VariantID, input.CategoryID).Scan(&existing)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if existing != "" {
			_, err = tx.ExecContext(ctx, `UPDATE product_price_policies SET minimum_price_kobo=$2,maximum_price_kobo=$3,rounding_increment_kobo=$4,created_by_user_id=$5,updated_at=now() WHERE organization_id=$1 AND id=$6`, orgID, input.MinimumPriceKobo, input.MaximumPriceKobo, input.RoundingIncrementKobo, actor, existing)
			if err != nil {
				return err
			}
			out.ID = existing
			return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "price_policy.updated", ResourceType: "price_policy", ResourceID: out.ID, NewState: input})
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO product_price_policies (organization_id,scope_type,product_id,variant_id,category_id,minimum_price_kobo,maximum_price_kobo,rounding_increment_kobo,created_by_user_id) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8,$9) RETURNING id`, orgID, input.ScopeType, input.ProductID, input.VariantID, input.CategoryID, input.MinimumPriceKobo, input.MaximumPriceKobo, input.RoundingIncrementKobo, actor).Scan(&out.ID); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "price_policy.created", ResourceType: "price_policy", ResourceID: out.ID, NewState: input})
	})
	return out, err
}
