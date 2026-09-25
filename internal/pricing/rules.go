package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/db"
)

type RuleRecord struct {
	ID                    string         `json:"id"`
	Name                  string         `json:"name"`
	ScopeType             string         `json:"scope_type"`
	ProductID             string         `json:"product_id,omitempty"`
	VariantID             string         `json:"variant_id,omitempty"`
	CategoryID            string         `json:"category_id,omitempty"`
	MinimumMarginBPS      int            `json:"minimum_margin_bps"`
	MaximumChangeBPS      int            `json:"maximum_change_bps"`
	MaximumPriceKobo      *int64         `json:"maximum_price_kobo,omitempty"`
	RoundingIncrementKobo int64          `json:"rounding_increment_kobo"`
	Priority              int            `json:"priority"`
	Active                bool           `json:"active"`
	CooldownMinutes       int            `json:"cooldown_minutes"`
	Conditions            RuleConditions `json:"conditions"`
	Version               int            `json:"version"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type RuleInput struct {
	Name                  string         `json:"name"`
	ScopeType             string         `json:"scope_type"`
	ProductID             string         `json:"product_id,omitempty"`
	VariantID             string         `json:"variant_id,omitempty"`
	CategoryID            string         `json:"category_id,omitempty"`
	MinimumMarginBPS      int            `json:"minimum_margin_bps"`
	MaximumChangeBPS      int            `json:"maximum_change_bps"`
	MaximumPriceKobo      *int64         `json:"maximum_price_kobo,omitempty"`
	RoundingIncrementKobo int64          `json:"rounding_increment_kobo"`
	Priority              int            `json:"priority"`
	Active                bool           `json:"active"`
	CooldownMinutes       int            `json:"cooldown_minutes"`
	Conditions            RuleConditions `json:"conditions"`
	ExpectedVersion       int            `json:"expected_version,omitempty"`
}

func validateRule(input RuleInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Name) < 3 || len(input.Name) > 100 {
		return errors.New("rule name must be 3-100 characters")
	}
	if input.MinimumMarginBPS < 1 || input.MinimumMarginBPS > 9999 {
		return ErrInvalidMargin
	}
	if input.MaximumChangeBPS < 1 || input.MaximumChangeBPS > 10000 {
		return errors.New("maximum change must be between 1 and 10000 basis points")
	}
	if input.RoundingIncrementKobo <= 0 {
		return errors.New("rounding increment must be positive")
	}
	if input.CooldownMinutes < 0 {
		return errors.New("cooldown cannot be negative")
	}
	if input.Conditions.MinimumStock != nil && *input.Conditions.MinimumStock < 0 {
		return errors.New("minimum stock cannot be negative")
	}
	if input.Conditions.MaximumCurrentKobo != nil && *input.Conditions.MaximumCurrentKobo < 0 {
		return errors.New("maximum current price cannot be negative")
	}
	switch input.ScopeType {
	case "product":
		if input.ProductID == "" || input.VariantID != "" || input.CategoryID != "" {
			return errors.New("product rule requires only product_id")
		}
	case "variant":
		if input.VariantID == "" || input.ProductID != "" || input.CategoryID != "" {
			return errors.New("variant rule requires only variant_id")
		}
	case "category":
		if input.CategoryID == "" || input.ProductID != "" || input.VariantID != "" {
			return errors.New("category rule requires only category_id")
		}
	case "store":
		if input.ProductID != "" || input.VariantID != "" || input.CategoryID != "" {
			return errors.New("store rule must not include a target")
		}
	default:
		return errors.New("scope_type must be product, variant, category, or store")
	}
	return nil
}

func (s *Service) CreateRule(ctx context.Context, orgID, actor string, input RuleInput) (RuleRecord, error) {
	input.Name = strings.TrimSpace(input.Name)
	if err := validateRule(input); err != nil {
		return RuleRecord{}, err
	}
	if input.Active {
		return RuleRecord{}, errors.New("create rules inactive; simulate the draft before activation")
	}
	var out RuleRecord
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if err := validateRuleTarget(ctx, tx, orgID, input); err != nil {
			return err
		}
		conditionsJSON, _ := json.Marshal(input.Conditions)
		err := tx.QueryRowContext(ctx, `INSERT INTO pricing_rules (organization_id, name, scope_type, product_id, variant_id, category_id,
			minimum_margin_bps, maximum_change_bps, maximum_price_kobo, rounding_increment_kobo, priority, active, cooldown_minutes, conditions, created_by_user_id)
			VALUES ($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			RETURNING id, version, created_at, updated_at`, orgID, input.Name, input.ScopeType, input.ProductID, input.VariantID, input.CategoryID,
			input.MinimumMarginBPS, input.MaximumChangeBPS, input.MaximumPriceKobo, input.RoundingIncrementKobo, input.Priority, input.Active, input.CooldownMinutes, conditionsJSON, actor).
			Scan(&out.ID, &out.Version, &out.CreatedAt, &out.UpdatedAt)
		if err != nil {
			return err
		}
		fillRule(&out, input)
		return appendRuleVersion(ctx, tx, orgID, actor, out)
	})
	return out, err
}

func (s *Service) ListRules(ctx context.Context, orgID string, activeOnly bool) ([]RuleRecord, error) {
	out := []RuleRecord{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT id, name, scope_type, COALESCE(product_id::text,''), COALESCE(variant_id::text,''), COALESCE(category_id::text,''),
			minimum_margin_bps, maximum_change_bps, maximum_price_kobo, rounding_increment_kobo, priority, active, cooldown_minutes, conditions, version, created_at, updated_at
			FROM pricing_rules WHERE organization_id=$1`
		args := []any{orgID}
		if activeOnly {
			query += " AND active=true"
		}
		query += " ORDER BY active DESC, priority, name"
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r RuleRecord
			var maximum sql.NullInt64
			var conditions []byte
			if err := rows.Scan(&r.ID, &r.Name, &r.ScopeType, &r.ProductID, &r.VariantID, &r.CategoryID, &r.MinimumMarginBPS, &r.MaximumChangeBPS, &maximum, &r.RoundingIncrementKobo, &r.Priority, &r.Active, &r.CooldownMinutes, &conditions, &r.Version, &r.CreatedAt, &r.UpdatedAt); err != nil {
				return err
			}
			if maximum.Valid {
				r.MaximumPriceKobo = &maximum.Int64
			}
			if len(conditions) > 0 {
				_ = json.Unmarshal(conditions, &r.Conditions)
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) UpdateRule(ctx context.Context, orgID, actor, id string, input RuleInput) (RuleRecord, error) {
	input.Name = strings.TrimSpace(input.Name)
	if err := validateRule(input); err != nil {
		return RuleRecord{}, err
	}
	var out RuleRecord
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if err := validateRuleTarget(ctx, tx, orgID, input); err != nil {
			return err
		}
		var current int
		if err := tx.QueryRowContext(ctx, `SELECT version FROM pricing_rules WHERE organization_id=$1 AND id=$2 FOR UPDATE`, orgID, id).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if input.ExpectedVersion > 0 && input.ExpectedVersion != current {
			return errors.New("rule changed; refresh and retry")
		}
		conditionsJSON, _ := json.Marshal(input.Conditions)
		err := tx.QueryRowContext(ctx, `UPDATE pricing_rules SET name=$3,scope_type=$4,product_id=NULLIF($5,'')::uuid,variant_id=NULLIF($6,'')::uuid,category_id=NULLIF($7,'')::uuid,
			minimum_margin_bps=$8,maximum_change_bps=$9,maximum_price_kobo=$10,rounding_increment_kobo=$11,priority=$12,active=$13,cooldown_minutes=$14,conditions=$15,version=version+1,updated_at=now()
			WHERE organization_id=$1 AND id=$2 RETURNING id,version,created_at,updated_at`, orgID, id, input.Name, input.ScopeType, input.ProductID, input.VariantID, input.CategoryID,
			input.MinimumMarginBPS, input.MaximumChangeBPS, input.MaximumPriceKobo, input.RoundingIncrementKobo, input.Priority, input.Active, input.CooldownMinutes, conditionsJSON).Scan(&out.ID, &out.Version, &out.CreatedAt, &out.UpdatedAt)
		if err != nil {
			return err
		}
		fillRule(&out, input)
		return appendRuleVersion(ctx, tx, orgID, actor, out)
	})
	return out, err
}

func (s *Service) DeleteRule(ctx context.Context, orgID, id string) error {
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM pricing_rules WHERE organization_id=$1 AND id=$2`, orgID, id)
		return err
	})
}

func fillRule(out *RuleRecord, input RuleInput) {
	out.Name, out.ScopeType, out.ProductID, out.VariantID, out.CategoryID = input.Name, input.ScopeType, input.ProductID, input.VariantID, input.CategoryID
	out.MinimumMarginBPS, out.MaximumChangeBPS, out.MaximumPriceKobo = input.MinimumMarginBPS, input.MaximumChangeBPS, input.MaximumPriceKobo
	out.RoundingIncrementKobo, out.Priority, out.Active, out.CooldownMinutes, out.Conditions = input.RoundingIncrementKobo, input.Priority, input.Active, input.CooldownMinutes, input.Conditions
}

func validateRuleTarget(ctx context.Context, tx *sql.Tx, orgID string, input RuleInput) error {
	var n int
	var err error
	switch input.ScopeType {
	case "product":
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE organization_id=$1 AND id=$2`, orgID, input.ProductID).Scan(&n)
	case "variant":
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_variants WHERE organization_id=$1 AND id=$2`, orgID, input.VariantID).Scan(&n)
	case "category":
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_categories WHERE organization_id=$1 AND id=$2`, orgID, input.CategoryID).Scan(&n)
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func appendRuleVersion(ctx context.Context, tx *sql.Tx, orgID, actor string, rule RuleRecord) error {
	raw, _ := json.Marshal(rule)
	_, err := tx.ExecContext(ctx, `INSERT INTO pricing_rule_versions (organization_id,pricing_rule_id,version,snapshot,changed_by_user_id) VALUES($1,$2,$3,$4,$5)`, orgID, rule.ID, rule.Version, raw, actor)
	if err != nil {
		return err
	}
	return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "pricing_rule.updated", ResourceType: "pricing_rule", ResourceID: rule.ID, NewState: map[string]any{"active": rule.Active, "version": rule.Version}})
}

func (s *Service) SetLock(ctx context.Context, orgID, actor, productID, variantID string, locked bool, reason string) (LockRecord, error) {
	out := LockRecord{ProductID: productID, VariantID: variantID, Locked: locked, Reason: strings.TrimSpace(reason)}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE organization_id=$1 AND id=$2`, orgID, productID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO product_price_locks (organization_id,product_id,variant_id,locked,reason,locked_by_user_id) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6) ON CONFLICT(product_id,COALESCE(variant_id,'00000000-0000-0000-0000-000000000000'::uuid)) DO UPDATE SET locked=EXCLUDED.locked,reason=EXCLUDED.reason,locked_by_user_id=EXCLUDED.locked_by_user_id,updated_at=now() RETURNING updated_at`, orgID, productID, variantID, locked, out.Reason, actor).Scan(&out.UpdatedAt); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "product.price_lock.updated", ResourceType: "product", ResourceID: productID, NewState: map[string]any{"locked": locked, "reason": out.Reason}})
	})
	return out, err
}
