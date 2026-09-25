package competitor

import "time"

type Competitor struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	SourceType     string           `json:"source_type"`
	Domain         string           `json:"domain"`
	LocationMarket string           `json:"location_market"`
	Status         string           `json:"status"`
	Policy         MonitoringPolicy `json:"monitoring_policy"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

type MonitoringPolicy struct {
	IntervalMinutes  int `json:"interval_minutes"`
	FreshnessMinutes int `json:"freshness_minutes"`
	BackoffMinutes   int `json:"backoff_minutes"`
}

type Product struct {
	ID                    string     `json:"id"`
	CompetitorID          string     `json:"competitor_id"`
	CompetitorName        string     `json:"competitor_name"`
	URL                   string     `json:"url"`
	Name                  string     `json:"name"`
	SKU                   string     `json:"sku"`
	MatchState            string     `json:"match_state"`
	SuggestedProductID    string     `json:"suggested_product_id,omitempty"`
	SuggestedProductName  string     `json:"suggested_product_name,omitempty"`
	ConfirmedProductID    string     `json:"confirmed_product_id,omitempty"`
	ConfirmedProductName  string     `json:"confirmed_product_name,omitempty"`
	MatchConfidenceBPS    int        `json:"match_confidence_bps"`
	LastObservedPriceKobo *int64     `json:"last_observed_price_kobo,omitempty"`
	LastObservedAt        *time.Time `json:"last_observed_at,omitempty"`
	NextCheckAt           *time.Time `json:"next_check_at,omitempty"`
	ConsecutiveFailures   int        `json:"consecutive_failures"`
	LastError             string     `json:"last_error"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type Observation struct {
	ID                      string    `json:"id"`
	CompetitorProductID     string    `json:"competitor_product_id"`
	SourceURL               string    `json:"source_url"`
	ObservedPriceKobo       *int64    `json:"observed_price_kobo,omitempty"`
	RegularPriceKobo        *int64    `json:"regular_price_kobo,omitempty"`
	SalePriceKobo           *int64    `json:"sale_price_kobo,omitempty"`
	Currency                string    `json:"currency,omitempty"`
	Availability            string    `json:"availability"`
	ExtractionConfidenceBPS int       `json:"extraction_confidence_bps"`
	RawEvidenceSHA256       string    `json:"raw_evidence_sha256"`
	ObservedAt              time.Time `json:"observed_at"`
	FreshUntil              time.Time `json:"fresh_until"`
}

type Alert struct {
	ID                  string     `json:"id"`
	CompetitorProductID string     `json:"competitor_product_id,omitempty"`
	AlertType           string     `json:"alert_type"`
	Message             string     `json:"message"`
	PreviousValue       string     `json:"previous_value,omitempty"`
	CurrentValue        string     `json:"current_value,omitempty"`
	AcknowledgedAt      *time.Time `json:"acknowledged_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

type SourceHealth struct {
	CompetitorProductID string     `json:"competitor_product_id"`
	URL                 string     `json:"url"`
	Status              string     `json:"status"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastError           string     `json:"last_error"`
	NextCheckAt         *time.Time `json:"next_check_at,omitempty"`
}

type ExtractedProduct struct {
	Name                    string
	SKU                     string
	PriceKobo               *int64
	RegularPriceKobo        *int64
	SalePriceKobo           *int64
	Currency                string
	Availability            string
	ExtractionConfidenceBPS int
}
