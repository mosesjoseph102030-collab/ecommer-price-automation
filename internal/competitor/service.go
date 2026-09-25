package competitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"automation/internal/audit"
	"automation/internal/db"
)

var ErrNotFound = errors.New("competitor record not found")

type Service struct {
	DB            *sql.DB
	JobDB         *sql.DB
	Provider      *Provider
	Logger        *slog.Logger
	CheckInterval time.Duration
	Freshness     time.Duration
}

type CompetitorInput struct {
	Name, SourceType, Domain, LocationMarket string
	Policy                                   MonitoringPolicy
}
type ProductInput struct{ CompetitorID, URL, Name, SKU string }

func (s *Service) CreateCompetitor(ctx context.Context, orgID, actor string, input CompetitorInput) (Competitor, error) {
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Name) < 2 || len(input.Name) > 120 {
		return Competitor{}, errors.New("competitor name must be 2-120 characters")
	}
	if input.SourceType == "" {
		input.SourceType = "approved_web"
	}
	if input.SourceType != "approved_web" && input.SourceType != "data_provider" {
		return Competitor{}, errors.New("source_type must be approved_web or data_provider")
	}
	input.Domain = strings.ToLower(strings.TrimSpace(input.Domain))
	if input.Domain == "" {
		return Competitor{}, errors.New("competitor domain is required")
	}
	probe := "https://" + input.Domain
	if _, err := s.Provider.ValidateURL(probe); err != nil {
		return Competitor{}, err
	}
	if input.Policy.IntervalMinutes < 1 {
		input.Policy.IntervalMinutes = 360
	}
	if input.Policy.FreshnessMinutes < 1 {
		input.Policy.FreshnessMinutes = 720
	}
	if input.Policy.BackoffMinutes < 1 {
		input.Policy.BackoffMinutes = 60
	}
	policy, _ := json.Marshal(input.Policy)
	out := Competitor{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `INSERT INTO competitors (organization_id,name,source_type,domain,location_market,monitoring_policy,created_by_user_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,status,created_at,updated_at`, orgID, input.Name, input.SourceType, input.Domain, strings.TrimSpace(input.LocationMarket), policy, actor).Scan(&out.ID, &out.Status, &out.CreatedAt, &out.UpdatedAt)
		if err != nil {
			return err
		}
		out.Name, out.SourceType, out.Domain, out.LocationMarket, out.Policy = input.Name, input.SourceType, input.Domain, input.LocationMarket, input.Policy
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "competitor.created", ResourceType: "competitor", ResourceID: out.ID, NewState: map[string]any{"domain": input.Domain, "source_type": input.SourceType}})
	})
	return out, err
}

func (s *Service) ListCompetitors(ctx context.Context, orgID string) ([]Competitor, error) {
	out := []Competitor{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,name,source_type,domain,location_market,status,monitoring_policy,created_at,updated_at FROM competitors WHERE organization_id=$1 ORDER BY name`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Competitor
			var raw []byte
			if err := rows.Scan(&c.ID, &c.Name, &c.SourceType, &c.Domain, &c.LocationMarket, &c.Status, &raw, &c.CreatedAt, &c.UpdatedAt); err != nil {
				return err
			}
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &c.Policy)
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) AddProduct(ctx context.Context, orgID, actor string, input ProductInput) (Product, error) {
	validated, err := s.Provider.ValidateURL(input.URL)
	if err != nil {
		return Product{}, err
	}
	var out Product
	err = db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var domain, status string
		if err := tx.QueryRowContext(ctx, `SELECT domain,status FROM competitors WHERE organization_id=$1 AND id=$2`, orgID, input.CompetitorID).Scan(&domain, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != "active" {
			return errors.New("competitor source is not active")
		}
		host, _ := url.Parse(validated)
		if !strings.EqualFold(host.Hostname(), domain) && !strings.HasSuffix(strings.ToLower(host.Hostname()), "."+domain) {
			return errors.New("competitor URL does not match the selected source domain")
		}
		err := tx.QueryRowContext(ctx, `INSERT INTO competitor_products (organization_id,competitor_id,url,name,sku,match_state,next_check_at,created_by_user_id) VALUES($1,$2,$3,$4,$5,'unmatched',now(),$6) RETURNING id,match_state,created_at,updated_at`, orgID, input.CompetitorID, validated, strings.TrimSpace(input.Name), strings.TrimSpace(input.SKU), actor).Scan(&out.ID, &out.MatchState, &out.CreatedAt, &out.UpdatedAt)
		if err != nil {
			return err
		}
		out.CompetitorID, out.URL, out.Name, out.SKU = input.CompetitorID, validated, strings.TrimSpace(input.Name), strings.TrimSpace(input.SKU)
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "competitor.product_added", ResourceType: "competitor_product", ResourceID: out.ID, NewState: map[string]any{"url": validated}})
	})
	if err == nil {
		_, err = s.QueueCheck(ctx, orgID, out.ID)
	}
	return out, err
}

func (s *Service) ListProducts(ctx context.Context, orgID, competitorID string) ([]Product, error) {
	out := []Product{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT cp.id,cp.competitor_id,c.name,cp.url,cp.name,cp.sku,cp.match_state,COALESCE(cp.suggested_product_id::text,''),COALESCE(sp.name,''),COALESCE(cp.confirmed_product_id::text,''),COALESCE(cpn.name,''),cp.match_confidence_bps,cp.last_observed_price_kobo,cp.last_observed_at,cp.next_check_at,cp.consecutive_failures,cp.last_error FROM competitor_products cp JOIN competitors c ON c.id=cp.competitor_id LEFT JOIN products sp ON sp.id=cp.suggested_product_id LEFT JOIN products cpn ON cpn.id=cp.confirmed_product_id WHERE cp.organization_id=$1`
		args := []any{orgID}
		if competitorID != "" {
			args = append(args, competitorID)
			query += " AND cp.competitor_id=$2"
		}
		query += " ORDER BY c.name,cp.name"
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p Product
			if err := rows.Scan(&p.ID, &p.CompetitorID, &p.CompetitorName, &p.URL, &p.Name, &p.SKU, &p.MatchState, &p.SuggestedProductID, &p.SuggestedProductName, &p.ConfirmedProductID, &p.ConfirmedProductName, &p.MatchConfidenceBPS, &p.LastObservedPriceKobo, &p.LastObservedAt, &p.NextCheckAt, &p.ConsecutiveFailures, &p.LastError); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) ReviewMatch(ctx context.Context, orgID, actor, competitorProductID, productID, state, note string) error {
	if state != "confirmed" && state != "rejected" && state != "needs_review" {
		return errors.New("match state must be confirmed, rejected, or needs_review")
	}
	return db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var previous string
		if err := tx.QueryRowContext(ctx, `SELECT match_state FROM competitor_products WHERE organization_id=$1 AND id=$2 FOR UPDATE`, orgID, competitorProductID).Scan(&previous); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if productID != "" {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE organization_id=$1 AND id=$2`, orgID, productID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return errors.New("matched product does not belong to this tenant")
			}
		}
		confidence := 0
		if state == "confirmed" {
			confidence = 10000
		}
		_, err := tx.ExecContext(ctx, `UPDATE competitor_products SET match_state=$3,confirmed_product_id=CASE WHEN $3='confirmed' THEN NULLIF($4,'')::uuid ELSE NULL END,match_confidence_bps=$5,updated_at=now() WHERE organization_id=$1 AND id=$2`, orgID, competitorProductID, state, productID, confidence)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO competitor_match_reviews (organization_id,competitor_product_id,product_id,previous_state,new_state,confidence_bps,note,reviewed_by_user_id) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8)`, orgID, competitorProductID, productID, previous, state, confidence, strings.TrimSpace(note), actor)
		if err != nil {
			return err
		}
		return audit.Append(ctx, tx, audit.Event{OrgID: orgID, ActorUserID: actor, Action: "competitor.match.reviewed", ResourceType: "competitor_product", ResourceID: competitorProductID, NewState: map[string]any{"state": state, "product_id": productID}})
	})
}

func (s *Service) History(ctx context.Context, orgID, competitorProductID string, limit int) ([]Observation, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	out := []Observation{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,competitor_product_id,source_url,observed_price_kobo,regular_price_kobo,sale_price_kobo,currency,availability,extraction_confidence_bps,raw_evidence_sha256,observed_at,fresh_until FROM competitor_price_observations WHERE organization_id=$1 AND competitor_product_id=$2 ORDER BY observed_at DESC LIMIT $3`, orgID, competitorProductID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o Observation
			var currency sql.NullString
			if err := rows.Scan(&o.ID, &o.CompetitorProductID, &o.SourceURL, &o.ObservedPriceKobo, &o.RegularPriceKobo, &o.SalePriceKobo, &currency, &o.Availability, &o.ExtractionConfidenceBPS, &o.RawEvidenceSHA256, &o.ObservedAt, &o.FreshUntil); err != nil {
				return err
			}
			o.Currency = currency.String
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) ListAlerts(ctx context.Context, orgID string, unacknowledged bool) ([]Alert, error) {
	out := []Alert{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		query := `SELECT id,COALESCE(competitor_product_id::text,''),alert_type,message,previous_value,current_value,acknowledged_at,created_at FROM competitor_alerts WHERE organization_id=$1`
		if unacknowledged {
			query += " AND acknowledged_at IS NULL"
		}
		query += " ORDER BY created_at DESC LIMIT 200"
		rows, err := tx.QueryContext(ctx, query, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Alert
			if err := rows.Scan(&a.ID, &a.CompetitorProductID, &a.AlertType, &a.Message, &a.PreviousValue, &a.CurrentValue, &a.AcknowledgedAt, &a.CreatedAt); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) QueueCheck(ctx context.Context, orgID, competitorProductID string) (string, error) {
	var id string
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		var n, active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM competitor_products WHERE organization_id=$1 AND id=$2`, orgID, competitorProductID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM competitor_source_runs WHERE organization_id=$1 AND competitor_product_id=$2 AND status IN ('queued','running')`, orgID, competitorProductID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return errors.New("competitor check already queued")
		}
		return tx.QueryRowContext(ctx, `INSERT INTO competitor_source_runs (organization_id,competitor_product_id,status) VALUES($1,$2,'queued') RETURNING id`, orgID, competitorProductID).Scan(&id)
	})
	return id, err
}

func (s *Service) Health(ctx context.Context, orgID string) ([]SourceHealth, error) {
	out := []SourceHealth{}
	err := db.WithTenant(ctx, s.DB, orgID, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT cp.id,cp.url,CASE WHEN cp.consecutive_failures>0 THEN 'error' WHEN cp.last_observed_at IS NULL THEN 'pending' WHEN cp.last_observed_at<now()-((COALESCE((c.monitoring_policy->>'freshness_minutes')::int,720)) * interval '1 minute') THEN 'stale' ELSE 'healthy' END,cp.consecutive_failures,cp.last_observed_at,cp.last_error,cp.next_check_at FROM competitor_products cp JOIN competitors c ON c.id=cp.competitor_id WHERE cp.organization_id=$1 ORDER BY c.name`, orgID)
		_ = rows
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var h SourceHealth
			if err := rows.Scan(&h.CompetitorProductID, &h.URL, &h.Status, &h.ConsecutiveFailures, &h.LastSuccessAt, &h.LastError, &h.NextCheckAt); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) AdminHealth(ctx context.Context) ([]SourceHealth, error) {
	database := s.JobDB
	if database == nil {
		database = s.DB
	}
	rows, err := database.QueryContext(ctx, `SELECT cp.id,cp.url,CASE WHEN cp.consecutive_failures>0 THEN 'error' WHEN cp.last_observed_at IS NULL THEN 'pending' WHEN cp.last_observed_at<now()-((COALESCE((c.monitoring_policy->>'freshness_minutes')::int,720)) * interval '1 minute') THEN 'stale' ELSE 'healthy' END,cp.consecutive_failures,cp.last_observed_at,cp.last_error,cp.next_check_at FROM competitor_products cp JOIN competitors c ON c.id=cp.competitor_id ORDER BY cp.updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceHealth{}
	for rows.Next() {
		var h SourceHealth
		if err := rows.Scan(&h.CompetitorProductID, &h.URL, &h.Status, &h.ConsecutiveFailures, &h.LastSuccessAt, &h.LastError, &h.NextCheckAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func safeCompetitorError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "competitor source timed out"
	}
	message := err.Error()
	if len(message) > 300 {
		message = message[:300]
	}
	return message
}
func sourceInterval(policy MonitoringPolicy) time.Duration {
	if policy.IntervalMinutes < 1 {
		return 6 * time.Hour
	}
	return time.Duration(policy.IntervalMinutes) * time.Minute
}
func sourceFreshness(policy MonitoringPolicy) time.Duration {
	if policy.FreshnessMinutes < 1 {
		return 12 * time.Hour
	}
	return time.Duration(policy.FreshnessMinutes) * time.Minute
}
func sourceBackoff(policy MonitoringPolicy) time.Duration {
	if policy.BackoffMinutes < 1 {
		return time.Hour
	}
	return time.Duration(policy.BackoffMinutes) * time.Minute
}
