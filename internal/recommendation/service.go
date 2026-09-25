package recommendation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/db"
	"automation/internal/permissions"
	"automation/internal/pricing"
	"automation/internal/woocommerce"
)

var (
	ErrNotFound        = errors.New("record not found")
	ErrKillSwitchOn    = errors.New("price publishing is disabled by the kill switch")
	ErrStale           = errors.New("recommendation expired; recompute before acting")
	ErrNotActionable   = errors.New("recommendation state is not actionable")
	ErrBelowFloor      = errors.New("requested price is below the minimum profitable price")
	ErrAboveCeiling    = errors.New("requested price exceeds the configured maximum price")
	ErrRoleLimit       = errors.New("change exceeds the approval limit for your role")
	ErrActiveExecution = errors.New("an active publish already exists for this request")
)

type Service struct {
	DB                *sql.DB
	JobDB             *sql.DB
	Pricing           *pricing.Service
	Woo               *woocommerce.Service
	Logger            *slog.Logger
	Interval          time.Duration
	RecommendationTTL time.Duration
}

// Generate builds or refreshes deterministic recommendations for a tenant.
func (s *Service) Generate(ctx context.Context, orgID string) (int, error) {
	products, err := s.catalogCandidates(ctx, orgID)
	if err != nil {
		return 0, err
	}
	signals, err := s.competitorSignals(ctx, orgID)
	if err != nil {
		return 0, err
	}
	written := 0
	for _, candidate := range products {
		result, evalErr := s.Pricing.Evaluate(ctx, orgID, candidate.ProductID, candidate.VariantID)
		if evalErr != nil || result.Status == "cost_required" {
			// Without a landed cost there is no safe recommendation. Skip.
			continue
		}
		productSignals := signals[candidate.ProductID]
		in := EngineInput{
			ProductID: candidate.ProductID, ProductName: candidate.Name, ProductSKU: candidate.SKU,
			VariantID: candidate.VariantID, CategoryIDs: candidate.CategoryIDs,
			CurrentPrice: result.CurrentPriceKobo, StockStatus: candidate.StockStatus, StockQuantity: candidate.StockQuantity,
			Calc: result, Competitors: productSignals.usable,
			CompetitorDataFresh: productSignals.total == 0 || len(productSignals.usable) > 0,
			Now:                 time.Now().UTC(), TTL: s.ttl(),
		}
		rec := Engine(in)
		if err := s.persist(ctx, orgID, rec); err != nil {
			return written, err
		}
		written++
	}
	if err := s.expireStale(ctx, orgID); err != nil {
		return written, err
	}
	return written, nil
}

type candidate struct {
	ProductID, VariantID, Name, SKU, StockStatus string
	StockQuantity                                *int
	CategoryIDs                                  []string
}

func (s *Service) catalogCandidates(ctx context.Context, orgID string) ([]candidate, error) {
	out := []candidate{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT p.id, p.name, p.sku, p.stock_status, p.stock_quantity,
			COALESCE(ARRAY(SELECT m.category_id FROM product_category_memberships m WHERE m.product_id=p.id),'{}') AS cats
			FROM products p WHERE p.organization_id=$1 AND p.status='published' AND p.external_id<>'' ORDER BY p.name`, orgID)
		if err != nil {
			return err
		}
		type productRow struct {
			id, name, sku, stock string
			quantity             *int
			cats                 []string
		}
		productRows := []productRow{}
		for rows.Next() {
			var p productRow
			if err := rows.Scan(&p.id, &p.name, &p.sku, &p.stock, &p.quantity, &p.cats); err != nil {
				rows.Close()
				return err
			}
			productRows = append(productRows, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, p := range productRows {
			out = append(out, candidate{ProductID: p.id, Name: p.name, SKU: p.sku, StockStatus: p.stock, StockQuantity: p.quantity, CategoryIDs: p.cats})
			variantRows, err := tx.QueryContext(ctx, `SELECT id, sku, stock_status, stock_quantity FROM product_variants WHERE organization_id=$1 AND product_id=$2 AND external_id<>'' ORDER BY id`, orgID, p.id)
			if err != nil {
				return err
			}
			for variantRows.Next() {
				var v candidate
				if err := variantRows.Scan(&v.VariantID, &v.SKU, &v.StockStatus, &v.StockQuantity); err != nil {
					variantRows.Close()
					return err
				}
				v.ProductID, v.Name, v.StockStatus, v.StockQuantity = p.id, p.name, p.stock, p.quantity
				out = append(out, v)
			}
			variantRows.Close()
		}
		return nil
	})
	return out, err
}

// competitorSet separates usable signals from total confirmed matches. When a
// product has confirmed competitors but none are usable (stale, out of stock,
// foreign currency, or low-confidence), the engine must investigate rather than
// silently treat the product as having no competitor data.
type competitorSet struct {
	usable []CompetitorSignal
	total  int
}

func (s *Service) competitorSignals(ctx context.Context, orgID string) (map[string]competitorSet, error) {
	out := map[string]competitorSet{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var currency string
		if err := tx.QueryRowContext(ctx, `SELECT currency FROM organizations WHERE id=$1`, orgID).Scan(&currency); err != nil {
			return err
		}
		// Newest observation per confirmed competitor product.
		rows, err := tx.QueryContext(ctx, `SELECT cp.confirmed_product_id, c.name, o.observed_price_kobo, o.source_url,
			o.observed_at, o.availability, o.fresh_until, o.currency, o.extraction_confidence_bps, m.match_confidence_bps
			FROM competitor_products cp
			JOIN competitor_match_reviews m ON m.competitor_product_id=cp.id AND m.new_state='confirmed'
			JOIN competitors c ON c.id=cp.competitor_id
			JOIN LATERAL (
				SELECT observed_price_kobo, source_url, observed_at, availability, fresh_until, currency, extraction_confidence_bps
				FROM competitor_price_observations
				WHERE competitor_product_id=cp.id ORDER BY observed_at DESC LIMIT 1
			) o ON TRUE
			WHERE cp.organization_id=$1 AND cp.confirmed_product_id IS NOT NULL`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var productID string
			var name, sourceURL string
			var observedAt time.Time
			var availability, observedCurrency string
			var freshUntil time.Time
			var extraction, matchConfidence int
			var price sql.NullInt64
			if err := rows.Scan(&productID, &name, &price, &sourceURL, &observedAt, &availability, &freshUntil,
				&observedCurrency, &extraction, &matchConfidence); err != nil {
				return err
			}
			set := out[productID]
			set.total++
			usable := price.Valid && price.Int64 > 0 &&
				availability == "instock" &&
				strings.EqualFold(observedCurrency, currency) &&
				freshUntil.After(time.Now().UTC()) &&
				extraction >= 5000 &&
				matchConfidence >= 5000
			if usable {
				set.usable = append(set.usable, CompetitorSignal{Name: name, PriceKobo: price.Int64, ObservedAt: observedAt,
					SourceURL: sourceURL, ExtractionBPS: extraction, ConfidenceBPS: matchConfidence})
			}
			out[productID] = set
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func usableSignals(signals []CompetitorSignal) int {
	count := 0
	for _, s := range signals {
		if s.PriceKobo > 0 && s.ExtractionBPS >= 5000 {
			count++
		}
	}
	return count
}
func (s *Service) ttl() time.Duration {
	if s.RecommendationTTL > 0 {
		return s.RecommendationTTL
	}
	return 30 * time.Minute
}

func (s *Service) persist(ctx context.Context, orgID string, rec Recommendation) error {
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(ctx, `INSERT INTO price_recommendations (organization_id, generation_key, product_id, variant_id, state,
			previous_price_kobo, recommended_price_kobo, minimum_profitable_price_kobo, maximum_price_kobo,
			lowest_confirmed_competitor_kobo, change_bps, confidence_bps, urgency, margin_risk, opportunity_kobo, explanation, expires_at, generated_at)
			VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,now())
			ON CONFLICT (organization_id, generation_key) DO UPDATE SET state=EXCLUDED.state,
				recommended_price_kobo=EXCLUDED.recommended_price_kobo, change_bps=EXCLUDED.change_bps,
				confidence_bps=EXCLUDED.confidence_bps, urgency=EXCLUDED.urgency, margin_risk=EXCLUDED.margin_risk,
				opportunity_kobo=EXCLUDED.opportunity_kobo, explanation=EXCLUDED.explanation,
				expires_at=EXCLUDED.expires_at, generated_at=now()
			RETURNING id`, orgID, rec.GenerationKey, rec.ProductID, rec.VariantID, rec.State, rec.PreviousPriceKobo,
			rec.RecommendedPriceKobo, rec.MinimumProfitablePriceKobo, rec.MaximumPriceKobo, rec.LowestConfirmedCompetitorKobo,
			rec.ChangeBPS, rec.ConfidenceBPS, urgencyFor(rec), marginRiskFor(rec), rec.OpportunityKobo,
			rec.Explanation, rec.ExpiresAt).Scan(&id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM price_recommendation_reasons WHERE organization_id=$1 AND recommendation_id=$2`, orgID, id); err != nil {
			return err
		}
		for _, reason := range rec.Reasons {
			if _, err := tx.ExecContext(ctx, `INSERT INTO price_recommendation_reasons (organization_id, recommendation_id, reason_type, message, amount_kobo)
				VALUES ($1,$2,$3,$4,$5)`, orgID, id, reason.Type, reason.Message, reason.Amount); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) expireStale(ctx context.Context, orgID string) error {
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE price_change_requests SET status='expired', updated_at=now()
			WHERE organization_id=$1 AND status IN ('draft','pending') AND expires_at < now()`, orgID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_change_requests SET status='failed', updated_at=now()
			WHERE organization_id=$1 AND status='approved' AND expires_at < now()`, orgID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE price_change_executions SET status='cancelled', last_error='expired', updated_at=now()
			WHERE organization_id=$1 AND status IN ('queued','running') AND scheduled_for IS NULL AND created_at < now() - interval '24 hours'`, orgID)
		return err
	})
}

// List returns the recommendation inbox with filters.
func (s *Service) List(ctx context.Context, orgID string, filter InboxFilter) ([]Recommendation, error) {
	if filter.Limit < 1 || filter.Limit > 200 {
		filter.Limit = 100
	}
	out := []Recommendation{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT r.id, r.generation_key, r.product_id, p.name, p.sku, COALESCE(r.variant_id::text,''), r.state,
			r.previous_price_kobo, r.recommended_price_kobo, r.minimum_profitable_price_kobo, r.maximum_price_kobo,
			r.lowest_confirmed_competitor_kobo, r.change_bps, r.confidence_bps, r.urgency, r.margin_risk, r.opportunity_kobo,
			r.explanation, r.generated_at, r.expires_at,
			COALESCE(p.stock_status,''), p.stock_quantity,
			COALESCE(ARRAY(SELECT m.category_id FROM product_category_memberships m WHERE m.product_id=p.id),'{}')
			FROM price_recommendations r JOIN products p ON p.id=r.product_id WHERE r.organization_id=$1`
		args := []any{orgID}
		if filter.State != "" {
			args = append(args, filter.State)
			query += fmt.Sprintf(" AND r.state=$%d", len(args))
		}
		if filter.ProductID != "" {
			args = append(args, filter.ProductID)
			query += fmt.Sprintf(" AND r.product_id=$%d", len(args))
		}
		if filter.CategoryID != "" {
			args = append(args, filter.CategoryID)
			query += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM product_category_memberships m WHERE m.product_id=r.product_id AND m.category_id=$%d)", len(args))
		}
		if filter.Urgency != "" {
			args = append(args, filter.Urgency)
			query += fmt.Sprintf(" AND r.urgency=$%d", len(args))
		}
		if filter.MarginRisk != "" {
			args = append(args, filter.MarginRisk)
			query += fmt.Sprintf(" AND r.margin_risk=$%d", len(args))
		}
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" ORDER BY r.generated_at DESC LIMIT $%d", len(args))
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rec Recommendation
			if err := rows.Scan(&rec.ID, &rec.GenerationKey, &rec.ProductID, &rec.ProductName, &rec.ProductSKU, &rec.VariantID, &rec.State,
				&rec.PreviousPriceKobo, &rec.RecommendedPriceKobo, &rec.MinimumProfitablePriceKobo, &rec.MaximumPriceKobo,
				&rec.LowestConfirmedCompetitorKobo, &rec.ChangeBPS, &rec.ConfidenceBPS, &rec.Urgency, &rec.MarginRisk, &rec.OpportunityKobo,
				&rec.Explanation, &rec.GeneratedAt, &rec.ExpiresAt, &rec.StockStatus, &rec.StockQuantity, &rec.CategoryIDs); err != nil {
				return err
			}
			rec.Reasons = []Reason{}
			reasonRows, err := tx.QueryContext(ctx, `SELECT reason_type, message, amount_kobo, COALESCE(source,'') FROM price_recommendation_reasons WHERE organization_id=$1 AND recommendation_id=$2 ORDER BY created_at`, orgID, rec.ID)
			if err != nil {
				return err
			}
			for reasonRows.Next() {
				var reason Reason
				if err := reasonRows.Scan(&reason.Type, &reason.Message, &reason.Amount, &reason.Source); err != nil {
					reasonRows.Close()
					return err
				}
				rec.Reasons = append(rec.Reasons, reason)
			}
			reasonRows.Close()
			out = append(out, rec)
		}
		return rows.Err()
	})
	return out, err
}

func urgencyFor(rec Recommendation) string {
	if rec.State == StateInvestigate || rec.State == StateRaise {
		return "high"
	}
	if rec.StockQuantity != nil && *rec.StockQuantity <= 5 {
		return "high"
	}
	return "normal"
}

func marginRiskFor(rec Recommendation) string {
	if rec.State == StateRaise || rec.State == StateInvestigate {
		return "critical"
	}
	if rec.State == StateLower {
		return "warning"
	}
	return "none"
}

func (s *Service) Get(ctx context.Context, orgID, id string) (Recommendation, error) {
	all, err := s.List(ctx, orgID, InboxFilter{Limit: 200})
	if err != nil {
		return Recommendation{}, err
	}
	for _, rec := range all {
		if rec.ID == id {
			return rec, nil
		}
	}
	return Recommendation{}, ErrNotFound
}

// SubmitForApproval creates a pending price-change request from a recommendation.
func (s *Service) SubmitForApproval(ctx context.Context, orgID, actor, recommendationID string) (ChangeRequest, error) {
	var out ChangeRequest
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var rec Recommendation
		if err := tx.QueryRowContext(ctx, `SELECT state, previous_price_kobo, recommended_price_kobo, minimum_profitable_price_kobo, maximum_price_kobo, expires_at
			FROM price_recommendations WHERE organization_id=$1 AND id=$2`, orgID, recommendationID).
			Scan(&rec.State, &rec.PreviousPriceKobo, &rec.RecommendedPriceKobo, &rec.MinimumProfitablePriceKobo, &rec.MaximumPriceKobo, &rec.ExpiresAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if rec.State != StateRaise && rec.State != StateLower {
			return ErrNotActionable
		}
		if time.Now().UTC().After(rec.ExpiresAt) {
			return ErrStale
		}
		if rec.RecommendedPriceKobo < rec.MinimumProfitablePriceKobo {
			return ErrBelowFloor
		}
		if rec.MaximumPriceKobo != nil && rec.RecommendedPriceKobo > *rec.MaximumPriceKobo {
			return ErrAboveCeiling
		}
		var productID string
		if err := tx.QueryRowContext(ctx, `SELECT product_id FROM price_recommendations WHERE organization_id=$1 AND id=$2`, orgID, recommendationID).Scan(&productID); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO price_change_requests (organization_id, recommendation_id, product_id, previous_price_kobo, requested_price_kobo, status, expires_at, created_by_user_id)
			VALUES ($1,$2,$3,$4,$5,'pending',$6,$7) RETURNING id, created_at, updated_at`, orgID, recommendationID, productID,
			rec.PreviousPriceKobo, rec.RecommendedPriceKobo, rec.ExpiresAt, actor).Scan(&out.ID, &out.CreatedAt, &out.UpdatedAt); err != nil {
			return err
		}
		out.RecommendationID, out.ProductID = recommendationID, productID
		out.PreviousPriceKobo, out.RequestedPriceKobo, out.Status, out.ExpiresAt = rec.PreviousPriceKobo, rec.RecommendedPriceKobo, "pending", rec.ExpiresAt
		if err := notifyOwners(ctx, tx, orgID, "price_change.pending", "Price change awaiting approval", "A price change was submitted and needs approval."); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "price_change.submitted", ResourceType: "price_change_request", ResourceID: out.ID, NewState: out})
	})
	return out, err
}

// Approve records a role-limited approval decision.
func (s *Service) Approve(ctx context.Context, orgID, actor, requestID, decision, note string, roles []string) (ChangeRequest, error) {
	role := highestApprovalRole(roles)
	if decision != "approved" && decision != "rejected" {
		return ChangeRequest{}, errors.New("decision must be approved or rejected")
	}
	var out ChangeRequest
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var previous, requested int64
		var status string
		var expires time.Time
		if err := tx.QueryRowContext(ctx, `SELECT previous_price_kobo, requested_price_kobo, status, expires_at FROM price_change_requests
			WHERE organization_id=$1 AND id=$2 FOR UPDATE`, orgID, requestID).Scan(&previous, &requested, &status, &expires); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != "pending" && status != "draft" {
			return errors.New("request is not awaiting approval")
		}
		if time.Now().UTC().After(expires) {
			return ErrStale
		}
		if decision == "approved" {
			limit, err := s.approvalLimitFor(ctx, tx, orgID, role)
			if err != nil {
				return err
			}
			change := absInt64(changeBetween(previous, requested))
			if int64(limit.MaximumChangeBPS) < change {
				return ErrRoleLimit
			}
			if limit.MaximumPriceKobo != nil && requested > *limit.MaximumPriceKobo {
				return ErrRoleLimit
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO price_change_approvals (organization_id, price_change_request_id, approved_by_user_id, role_id, decision, note)
			VALUES ($1,$2,$3,$4,$5,$6)`, orgID, requestID, actor, role, decision, strings.TrimSpace(note)); err != nil {
			return err
		}
		nextStatus := "rejected"
		if decision == "approved" {
			nextStatus = "approved"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_change_requests SET status=$3, updated_at=now() WHERE organization_id=$1 AND id=$2`, orgID, requestID, nextStatus); err != nil {
			return err
		}
		out.Status = nextStatus
		if decision == "approved" {
			if err := notifyOwners(ctx, tx, orgID, "price_change.approved", "Price change approved", "An approved price change is ready to publish."); err != nil {
				return err
			}
		}
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "price_change." + decision, ResourceType: "price_change_request", ResourceID: requestID, NewState: map[string]any{"role": role, "change_bps": changeBetween(previous, requested)}})
	})
	return out, err
}

func highestApprovalRole(roles []string) string {
	for _, preferred := range []string{permissions.RoleStoreOwner, permissions.RolePricingManager} {
		for _, role := range roles {
			if role == preferred {
				return preferred
			}
		}
	}
	if len(roles) > 0 {
		return roles[0]
	}
	return ""
}

func (s *Service) approvalLimitFor(ctx context.Context, tx *sql.Tx, orgID, role string) (ApprovalLimit, error) {
	var limit ApprovalLimit
	err := tx.QueryRowContext(ctx, `SELECT role_id, maximum_change_bps, maximum_price_kobo FROM approval_limits WHERE organization_id=$1 AND role_id=$2`, orgID, role).
		Scan(&limit.RoleID, &limit.MaximumChangeBPS, &limit.MaximumPriceKobo)
	if errors.Is(err, sql.ErrNoRows) {
		// No configured limit means this role cannot approve price changes.
		return ApprovalLimit{}, ErrRoleLimit
	}
	return limit, err
}

func changeBetween(previous, next int64) int64 {
	if previous <= 0 {
		return 0
	}
	diff := next - previous
	if diff < 0 {
		diff = -diff
	}
	return diff * 10000 / previous
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func (s *Service) ListRequests(ctx context.Context, orgID, status string) ([]ChangeRequest, error) {
	out := []ChangeRequest{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT r.id, r.recommendation_id, r.product_id, p.name, r.previous_price_kobo, r.requested_price_kobo, r.status,
			r.scheduled_for, r.expires_at, r.created_at, r.updated_at,
			COALESCE((SELECT a.approved_by_user_id::text FROM price_change_approvals a WHERE a.price_change_request_id=r.id AND a.decision='approved' ORDER BY a.created_at DESC LIMIT 1),''),
			COALESCE((SELECT a.role_id FROM price_change_approvals a WHERE a.price_change_request_id=r.id AND a.decision='approved' ORDER BY a.created_at DESC LIMIT 1),''),
			COALESCE((SELECT a.note FROM price_change_approvals a WHERE a.price_change_request_id=r.id AND a.decision='approved' ORDER BY a.created_at DESC LIMIT 1),''),
			COALESCE(e.id::text,''), COALESCE(e.status,''), e.verified_after_price_kobo,
			COALESCE(b.id::text,''), COALESCE(b.status,''), COALESCE(e.last_error,'')
			FROM price_change_requests r JOIN products p ON p.id=r.product_id
			LEFT JOIN price_change_executions e ON e.price_change_request_id=r.id
			LEFT JOIN price_rollbacks b ON b.execution_id=e.id
			WHERE r.organization_id=$1`
		args := []any{orgID}
		if status != "" {
			args = append(args, status)
			query += fmt.Sprintf(" AND r.status=$%d", len(args))
		}
		query += " ORDER BY r.created_at DESC LIMIT 100"
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r ChangeRequest
			if err := rows.Scan(&r.ID, &r.RecommendationID, &r.ProductID, &r.ProductName, &r.PreviousPriceKobo, &r.RequestedPriceKobo, &r.Status,
				&r.ScheduledFor, &r.ExpiresAt, &r.CreatedAt, &r.UpdatedAt, &r.ApprovedByUserID, &r.ApprovedRole, &r.ApprovalNote,
				&r.ExecutionID, &r.ExecutionStatus, &r.VerifiedAfterKobo, &r.RollbackID, &r.RollbackStatus, &r.LastError); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// QueuePublish creates the publish execution with an idempotency key.
func (s *Service) QueuePublish(ctx context.Context, orgID, actor, requestID string, scheduledFor *time.Time) (Execution, error) {
	var out Execution
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		enabled, err := s.killSwitchEnabled(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if enabled {
			return ErrKillSwitchOn
		}
		var status string
		var productID, variantID string
		var previousKobo, requestedKobo int64
		var expires time.Time
		if err := tx.QueryRowContext(ctx, `SELECT status, product_id, previous_price_kobo, requested_price_kobo, expires_at,
			COALESCE((SELECT variant_id::text FROM price_recommendations WHERE id=recommendation_id),'')
			FROM price_change_requests WHERE organization_id=$1 AND id=$2 FOR UPDATE`, orgID, requestID).
			Scan(&status, &productID, &previousKobo, &requestedKobo, &expires, &variantID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != "approved" {
			return errors.New("only an approved request can be published")
		}
		if time.Now().UTC().After(expires) {
			return ErrStale
		}
		// One active execution per request: the idempotency key is the request id.
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_change_executions WHERE organization_id=$1 AND price_change_request_id=$2 AND status IN ('queued','running')`, orgID, requestID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return ErrActiveExecution
		}
		idem := requestID
		if err := tx.QueryRowContext(ctx, `INSERT INTO price_change_executions (organization_id, price_change_request_id, product_id, idempotency_key,
			status, before_price_kobo, requested_price_kobo, scheduled_for)
			VALUES ($1,$2,$3,$4,'queued',$5,$6,$7)
			ON CONFLICT (organization_id, idempotency_key) DO UPDATE SET status='queued', updated_at=now()
			RETURNING id, attempts, created_at, updated_at, scheduled_for`, orgID, requestID, productID, idem, previousKobo, requestedKobo, scheduledFor).
			Scan(&out.ID, &out.Attempts, &out.CreatedAt, &out.UpdatedAt, &out.ScheduledFor); err != nil {
			return err
		}
		out.RequestID, out.ProductID, out.VariantID, out.IdempotencyKey = requestID, productID, variantID, idem
		out.Status, out.BeforePriceKobo, out.RequestedPriceKobo, out.ScheduledFor = "queued", previousKobo, requestedKobo, scheduledFor
		if _, err := tx.ExecContext(ctx, `UPDATE price_change_requests SET scheduled_for=$3, updated_at=now() WHERE organization_id=$1 AND id=$2`, orgID, requestID, scheduledFor); err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "price_change.publish_queued", ResourceType: "price_change_execution", ResourceID: out.ID, NewState: map[string]any{"idempotency_key": idem}})
	})
	return out, err
}

func (s *Service) killSwitchEnabled(ctx context.Context, tx *sql.Tx, orgID string) (bool, error) {
	var enabled bool
	err := tx.QueryRowContext(ctx, `SELECT enabled FROM pricing_kill_switches
		WHERE scope='platform' OR (scope='tenant' AND organization_id=$1) ORDER BY scope DESC LIMIT 1`, orgID).Scan(&enabled)
	return enabled, err
}

func (s *Service) killSwitchEnabledDB(ctx context.Context, orgID string) (bool, error) {
	var enabled bool
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT enabled FROM pricing_kill_switches
			WHERE scope='platform' OR (scope='tenant' AND organization_id=$1) ORDER BY scope DESC LIMIT 1`, orgID).Scan(&enabled)
	})
	return enabled, err
}

// GetKillSwitch returns the effective kill-switch state for a tenant.
func (s *Service) GetKillSwitch(ctx context.Context, orgID string) (KillSwitch, error) {
	out := KillSwitch{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT scope, enabled, reason, updated_at FROM pricing_kill_switches
			WHERE scope='platform' OR (scope='tenant' AND organization_id=$1) ORDER BY scope DESC LIMIT 1`, orgID).
			Scan(&out.Scope, &out.Enabled, &out.Reason, &out.UpdatedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return KillSwitch{Scope: "platform", Enabled: false}, nil
	}
	return out, err
}

// GetPlatformKillSwitch reads the platform-scope switch for admin diagnostics.
func (s *Service) GetPlatformKillSwitch(ctx context.Context) (KillSwitch, error) {
	database := s.JobDB
	if database == nil {
		database = s.DB
	}
	out := KillSwitch{Scope: "platform"}
	err := database.QueryRowContext(ctx, `SELECT enabled, reason, updated_at FROM pricing_kill_switches WHERE scope='platform' AND organization_id IS NULL`).
		Scan(&out.Enabled, &out.Reason, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

// SetTenantKillSwitch toggles publishing for one tenant.
func (s *Service) SetTenantKillSwitch(ctx context.Context, orgID, actor string, enabled bool, reason string) (KillSwitch, error) {
	return s.setKillSwitch(ctx, orgID, actor, "tenant", enabled, reason)
}

// SetPlatformKillSwitch toggles publishing for every tenant. Platform scope only.
func (s *Service) SetPlatformKillSwitch(ctx context.Context, actor string, enabled bool, reason string) (KillSwitch, error) {
	return s.setKillSwitch(ctx, "", actor, "platform", enabled, reason)
}

func (s *Service) setKillSwitch(ctx context.Context, orgID, actor, scope string, enabled bool, reason string) (KillSwitch, error) {
	if len(strings.TrimSpace(reason)) < 3 {
		return KillSwitch{}, errors.New("a kill-switch reason is required")
	}
	out := KillSwitch{Scope: scope, Enabled: enabled, Reason: strings.TrimSpace(reason)}
	var updatedAt time.Time
	// Platform-scope rows may only be written through the controlled admin role;
	// tenant rows go through the tenant-scoped application role.
	apply := func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pricing_kill_switches (organization_id, scope, enabled, reason, changed_by_user_id)
			VALUES (NULLIF($1,'')::uuid,$2,$3,$4,NULLIF($5,'')::uuid)
			ON CONFLICT (scope, COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid))
			DO UPDATE SET enabled=EXCLUDED.enabled, reason=EXCLUDED.reason, changed_by_user_id=EXCLUDED.changed_by_user_id, updated_at=now()`,
			orgID, scope, enabled, out.Reason, actor); err != nil {
			return err
		}
		if enabled {
			// The kill switch cancels queued jobs safely; nothing is published.
			cancel := `UPDATE price_change_executions SET status='cancelled', last_error='kill switch', updated_at=now()
				WHERE status IN ('queued','running')`
			if orgID != "" {
				cancel += ` AND organization_id=$1`
			}
			if _, err := tx.ExecContext(ctx, cancel, orgID); err != nil {
				return err
			}
		}
		if err := tx.QueryRowContext(ctx, `SELECT updated_at FROM pricing_kill_switches
			WHERE scope=$1 AND organization_id IS NOT DISTINCT FROM NULLIF($2,'')::uuid`, scope, orgID).Scan(&updatedAt); err != nil {
			return err
		}
		// Audit follows the same rule: tenant switches are tenant events, the
		// platform switch is a platform event with no tenant.
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "kill_switch." + scope,
			ResourceType: "pricing_kill_switch", ResourceID: scope,
			NewState: map[string]any{"enabled": enabled, "reason": out.Reason}})
	}
	var err error
	if scope == "platform" {
		database := s.JobDB
		if database == nil {
			database = s.DB
		}
		tx, beginErr := database.BeginTx(ctx, nil)
		if beginErr != nil {
			return out, beginErr
		}
		defer tx.Rollback()
		if applyErr := apply(tx); applyErr != nil {
			return out, applyErr
		}
		err = tx.Commit()
	} else {
		err = db.WithTenant(ctx, s.DB, orgID, apply)
	}
	out.UpdatedAt = updatedAt
	return out, err
}

func (s *Service) ListApprovalLimits(ctx context.Context, orgID string) ([]ApprovalLimit, error) {
	out := []ApprovalLimit{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT role_id, maximum_change_bps, maximum_price_kobo FROM approval_limits WHERE organization_id=$1 ORDER BY role_id`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l ApprovalLimit
			if err := rows.Scan(&l.RoleID, &l.MaximumChangeBPS, &l.MaximumPriceKobo); err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}

func notifyOwners(ctx context.Context, tx *sql.Tx, orgID, kind, title, body string) error {
	rows, err := tx.QueryContext(ctx, `SELECT u.id::text FROM users u
		JOIN memberships m ON m.user_id=u.id AND m.organization_id=$1 AND m.status='active'
		JOIN member_role_assignments r ON r.user_id=u.id AND r.organization_id=$1 AND r.role_id='store_owner'`, orgID)
	if err != nil {
		return err
	}
	owners := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		owners = append(owners, id)
	}
	rows.Close()
	for _, owner := range owners {
		var notificationID string
		if err := tx.QueryRowContext(ctx, `INSERT INTO notifications (organization_id, user_id, type, title, body) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			orgID, owner, kind, title, body).Scan(&notificationID); err != nil {
			return err
		}
		// In-app is delivered immediately; email/WhatsApp adapters plug in later (Phase 6).
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries (notification_id, channel, status) VALUES ($1,'inapp','sent')`, notificationID); err != nil {
			return err
		}
	}
	return nil
}
