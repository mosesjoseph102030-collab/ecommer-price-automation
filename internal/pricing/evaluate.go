package pricing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"automation/internal/db"
)

type ProductSnapshot struct {
	ID            string
	SKU           string
	Name          string
	PriceKobo     int64
	SalePrice     *int64
	StockStatus   string
	StockQuantity *int
	VariantSKU    string
	Locked        bool
	LockReason    string
	CategoryIDs   []string
}

func (s *Service) Evaluate(ctx context.Context, orgID, productID, variantID string) (Result, error) {
	return s.evaluate(ctx, orgID, productID, variantID, nil)
}

// SimulateDraft evaluates a not-yet-active rule without persisting it.
func (s *Service) SimulateDraft(ctx context.Context, orgID, productID, variantID string, draft Rule) (Result, error) {
	draft.Active = true
	return s.evaluate(ctx, orgID, productID, variantID, &draft)
}

func (s *Service) evaluate(ctx context.Context, orgID, productID, variantID string, override *Rule) (Result, error) {
	var product ProductSnapshot
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var lockReason sql.NullString
		var locked sql.NullBool
		err := tx.QueryRowContext(ctx, `SELECT p.id, p.sku, p.name, p.price_kobo, p.sale_price_kobo, p.stock_status, p.stock_quantity,
			(SELECT locked FROM product_price_locks l WHERE l.product_id=p.id AND l.variant_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid LIMIT 1),
			(SELECT reason FROM product_price_locks l WHERE l.product_id=p.id AND l.variant_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid LIMIT 1)
			FROM products p WHERE p.organization_id=$1 AND p.id=$2`, orgID, productID).Scan(&product.ID, &product.SKU, &product.Name, &product.PriceKobo, &product.SalePrice, &product.StockStatus, &product.StockQuantity, &locked, &lockReason)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		product.Locked, product.LockReason = locked.Valid && locked.Bool, lockReason.String
		rows, err := tx.QueryContext(ctx, `SELECT c.id FROM product_categories c JOIN product_category_memberships m ON m.category_id=c.id WHERE m.product_id=$1`, productID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			product.CategoryIDs = append(product.CategoryIDs, id)
		}
		rows.Close()
		if variantID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT sku FROM product_variants WHERE organization_id=$1 AND id=$2 AND product_id=$3`, orgID, variantID, productID).Scan(&product.VariantSKU); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return ErrNotFound
				}
				return err
			}
			if err := tx.QueryRowContext(ctx, `SELECT price_kobo, sale_price_kobo, stock_status, stock_quantity FROM product_variants WHERE organization_id=$1 AND id=$2`, orgID, variantID).Scan(&product.PriceKobo, &product.SalePrice, &product.StockStatus, &product.StockQuantity); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	cost, err := s.GetCost(ctx, orgID, productID, variantID)
	if err != nil {
		return Result{}, err
	}
	policies, rules, err := s.loadPoliciesAndRules(ctx, orgID, product, variantID)
	if err != nil {
		return Result{}, err
	}
	policy := selectPolicy(policies, variantID != "")
	eligible := make([]Rule, 0, len(rules))
	for _, candidate := range rules {
		if ruleApplies(candidate, product, variantID) && conditionsMatch(candidate.Conditions, product) {
			eligible = append(eligible, candidate)
		}
	}
	selectedRule := SelectRule(eligible, variantID != "")
	if override != nil && ruleApplies(*override, product, variantID) && conditionsMatch(override.Conditions, product) && (selectedRule == nil || ruleRank(*override) < ruleRank(*selectedRule) || (ruleRank(*override) == ruleRank(*selectedRule) && override.Priority < selectedRule.Priority)) {
		selectedRule = override
	}
	input := Input{ProductID: productID, VariantID: variantID, Cost: cost.Cost, CurrentPriceKobo: product.PriceKobo,
		CurrentSalePriceKobo: product.SalePrice, SelectedRule: selectedRule, ProductLocked: product.Locked, LockReason: product.LockReason}
	if policy != nil {
		input.ExplicitMinimumPriceKobo, input.ExplicitMaximumPriceKobo = policy.MinimumPriceKobo, policy.MaximumPriceKobo
		input.RoundingIncrementKobo = policy.RoundingIncrementKobo
	}
	return Calculate(input)
}

func (s *Service) loadPoliciesAndRules(ctx context.Context, orgID string, product ProductSnapshot, variantID string) ([]PricePolicy, []Rule, error) {
	policies := []PricePolicy{}
	rules := []Rule{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, scope_type, COALESCE(product_id::text,''), COALESCE(variant_id::text,''),
			COALESCE(category_id::text,''), minimum_price_kobo, maximum_price_kobo, rounding_increment_kobo
			FROM product_price_policies WHERE organization_id=$1 AND
			(scope_type='store' OR (scope_type='product' AND product_id=$2) OR (scope_type='variant' AND variant_id=NULLIF($3,'')::uuid)
			 OR (scope_type='category' AND category_id=ANY($4::uuid[])))`, orgID, product.ID, variantID, uuidArray(product.CategoryIDs))
		if err != nil {
			return err
		}
		for rows.Next() {
			var p PricePolicy
			if err := rows.Scan(&p.ID, &p.ScopeType, &p.ProductID, &p.VariantID, &p.CategoryID, &p.MinimumPriceKobo, &p.MaximumPriceKobo, &p.RoundingIncrementKobo); err != nil {
				rows.Close()
				return err
			}
			policies = append(policies, p)
		}
		rows.Close()
		rows, err = tx.QueryContext(ctx, `SELECT id, name, scope_type, COALESCE(product_id::text,''), COALESCE(variant_id::text,''), COALESCE(category_id::text,''), minimum_margin_bps, maximum_change_bps,
			maximum_price_kobo, rounding_increment_kobo, priority, conditions FROM pricing_rules WHERE organization_id=$1 AND active=true AND
			(scope_type='store' OR (scope_type='product' AND product_id=$2) OR (scope_type='variant' AND variant_id=NULLIF($3,'')::uuid)
			 OR (scope_type='category' AND category_id=ANY($4::uuid[])))`, orgID, product.ID, variantID, uuidArray(product.CategoryIDs))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r Rule
			var maximum sql.NullInt64
			var conditions []byte
			if err := rows.Scan(&r.ID, &r.Name, &r.Scope, &r.ProductID, &r.VariantID, &r.CategoryID, &r.MinimumMarginBPS, &r.MaximumChangeBPS, &maximum, &r.RoundingIncrementKobo, &r.Priority, &conditions); err != nil {
				return err
			}
			if maximum.Valid {
				r.MaximumPriceKobo = &maximum.Int64
			}
			if len(conditions) > 0 {
				_ = json.Unmarshal(conditions, &r.Conditions)
			}
			r.Active = true
			rules = append(rules, r)
		}
		return rows.Err()
	})
	return policies, rules, err
}

func ruleRank(rule Rule) int {
	return map[string]int{"product": 3, "variant": 4, "category": 5, "store": 6}[rule.Scope]
}

func ruleApplies(rule Rule, product ProductSnapshot, variantID string) bool {
	switch rule.Scope {
	case "store":
		return true
	case "product":
		return rule.ProductID == product.ID
	case "variant":
		return variantID != "" && rule.VariantID == variantID
	case "category":
		for _, id := range product.CategoryIDs {
			if id == rule.CategoryID {
				return true
			}
		}
	default:
		return false
	}
	return false
}

func conditionsMatch(conditions RuleConditions, product ProductSnapshot) bool {
	if conditions.InStockOnly && product.StockStatus != "instock" {
		return false
	}
	if conditions.MinimumStock != nil && (product.StockQuantity == nil || *product.StockQuantity < *conditions.MinimumStock) {
		return false
	}
	if conditions.MaximumCurrentKobo != nil {
		current := product.PriceKobo
		if product.SalePrice != nil {
			current = *product.SalePrice
		}
		if current > *conditions.MaximumCurrentKobo {
			return false
		}
	}
	return true
}

func uuidArray(values []string) string { return "{" + strings.Join(values, ",") + "}" }

func selectPolicy(policies []PricePolicy, hasVariant bool) *PricePolicy {
	bestRank, best := 9, (*PricePolicy)(nil)
	for i := range policies {
		p := &policies[i]
		rank := map[string]int{"product": 1, "variant": 2, "category": 3, "store": 4}[p.ScopeType]
		if rank == 0 || (p.ScopeType == "variant" && !hasVariant) {
			continue
		}
		if rank < bestRank {
			bestRank, best = rank, p
		}
	}
	return best
}

func (s *Service) RecordImpact(ctx context.Context, orgID, productID, variantID string, result Result) error {
	if result.Status == "compliant" || result.Status == "locked" {
		return nil
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cost_impact_alerts WHERE organization_id=$1 AND product_id=$2 AND alert_type='below_target_margin' AND acknowledged_at IS NULL`, orgID, productID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return nil
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO cost_impact_alerts (organization_id, product_id, alert_type, current_value_kobo, message)
			VALUES ($1,$2,'below_target_margin',$3,$4)`, orgID, productID, result.CurrentPriceKobo, result.Reason)
		return err
	})
}

type CostRow struct {
	ProductID        string     `json:"product_id"`
	ProductName      string     `json:"product_name"`
	SKU              string     `json:"sku"`
	VariantID        string     `json:"variant_id,omitempty"`
	CurrentPriceKobo int64      `json:"current_price_kobo"`
	CostID           string     `json:"cost_id,omitempty"`
	LandedCostKobo   *int64     `json:"landed_cost_kobo,omitempty"`
	MarginBPS        *int64     `json:"margin_bps,omitempty"`
	MinimumPriceKobo *int64     `json:"minimum_price_kobo,omitempty"`
	Status           string     `json:"status"`
	EffectiveFrom    *time.Time `json:"effective_from,omitempty"`
}

func (s *Service) ListCosts(ctx context.Context, orgID, search string, limit int) ([]CostRow, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rowsOut := []CostRow{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT p.id, p.name, p.sku, COALESCE(p.sale_price_kobo,p.price_kobo), c.id, c.landed_cost_kobo, c.effective_from,
			COALESCE(l.locked,false) FROM products p LEFT JOIN product_costs c ON c.product_id=p.id AND c.variant_id IS NULL AND c.status='active'
			LEFT JOIN product_price_locks l ON l.product_id=p.id AND l.variant_id IS NULL WHERE p.organization_id=$1 AND p.status <> 'deleted'`
		args := []any{orgID}
		if search != "" {
			args = append(args, "%"+strings.ToLower(search)+"%")
			query += " AND (lower(p.name) LIKE $2 OR lower(p.sku) LIKE $2)"
		}
		query += " ORDER BY p.name LIMIT $3"
		args = append(args, limit)
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r CostRow
			var costID sql.NullString
			var landed sql.NullInt64
			var effective sql.NullTime
			var locked bool
			if err := rows.Scan(&r.ProductID, &r.ProductName, &r.SKU, &r.CurrentPriceKobo, &costID, &landed, &effective, &locked); err != nil {
				return err
			}
			if costID.Valid {
				r.CostID, r.LandedCostKobo = costID.String, &landed.Int64
				if effective.Valid {
					effectiveTime := effective.Time
					r.EffectiveFrom = &effectiveTime
				}
				r.MarginBPS = ptr(MarginBPS(r.CurrentPriceKobo, landed.Int64))
				result, err := Calculate(Input{ProductID: r.ProductID, Cost: CostComponents{SupplierKobo: landed.Int64}, CurrentPriceKobo: r.CurrentPriceKobo})
				if err == nil {
					r.MinimumPriceKobo = &result.MinimumProfitableKobo
					r.Status = result.Status
				} else {
					r.Status = "cost_required"
				}
			} else {
				r.Status = "cost_required"
			}
			if locked {
				r.Status = "locked"
			}
			rowsOut = append(rowsOut, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for i := range rowsOut {
		if rowsOut[i].CostID == "" {
			continue
		}
		if result, evalErr := s.Evaluate(ctx, orgID, rowsOut[i].ProductID, ""); evalErr == nil {
			rowsOut[i].MarginBPS = ptr(result.CurrentMarginBPS)
			rowsOut[i].MinimumPriceKobo = ptr(result.MinimumProfitableKobo)
			rowsOut[i].Status = result.Status
		}
	}
	return rowsOut, nil
}
func ptr(v int64) *int64 { return &v }

type ImpactAlert struct {
	ID                string     `json:"id"`
	ProductID         string     `json:"product_id"`
	ProductName       string     `json:"product_name"`
	AlertType         string     `json:"alert_type"`
	PreviousValueKobo *int64     `json:"previous_value_kobo,omitempty"`
	CurrentValueKobo  *int64     `json:"current_value_kobo,omitempty"`
	Message           string     `json:"message"`
	AcknowledgedAt    *time.Time `json:"acknowledged_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

func (s *Service) ListImpactAlerts(ctx context.Context, orgID string, unacknowledgedOnly bool) ([]ImpactAlert, error) {
	out := []ImpactAlert{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT a.id,a.product_id,p.name,a.alert_type,a.previous_value_kobo,a.current_value_kobo,a.message,a.acknowledged_at,a.created_at FROM cost_impact_alerts a JOIN products p ON p.id=a.product_id WHERE a.organization_id=$1`
		if unacknowledgedOnly {
			query += " AND a.acknowledged_at IS NULL"
		}
		query += " ORDER BY a.created_at DESC LIMIT 200"
		rows, err := tx.QueryContext(ctx, query, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a ImpactAlert
			if err := rows.Scan(&a.ID, &a.ProductID, &a.ProductName, &a.AlertType, &a.PreviousValueKobo, &a.CurrentValueKobo, &a.Message, &a.AcknowledgedAt, &a.CreatedAt); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}
