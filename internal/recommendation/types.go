package recommendation

import "time"

const (
	StateHold        = "hold"
	StateRaise       = "raise"
	StateLower       = "lower"
	StateInvestigate = "investigate"
	StatePause       = "pause"
)

type Reason struct {
	Type     string `json:"type"`
	Message  string `json:"message"`
	Amount   *int64 `json:"amount_kobo,omitempty"`
	Source   string `json:"source,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

type Recommendation struct {
	ID                            string    `json:"id"`
	GenerationKey                 string    `json:"generation_key"`
	ProductID                     string    `json:"product_id"`
	ProductName                   string    `json:"product_name"`
	ProductSKU                    string    `json:"product_sku"`
	VariantID                     string    `json:"variant_id,omitempty"`
	CategoryIDs                   []string  `json:"category_ids,omitempty"`
	State                         string    `json:"state"`
	PreviousPriceKobo             int64     `json:"previous_price_kobo"`
	RecommendedPriceKobo          int64     `json:"recommended_price_kobo"`
	MinimumProfitablePriceKobo    int64     `json:"minimum_profitable_price_kobo"`
	MaximumPriceKobo              *int64    `json:"maximum_price_kobo,omitempty"`
	LowestConfirmedCompetitorKobo *int64    `json:"lowest_confirmed_competitor_kobo,omitempty"`
	ChangeBPS                     int64     `json:"change_bps"`
	ConfidenceBPS                 int       `json:"confidence_bps"`
	Urgency                       string    `json:"urgency"`
	MarginRisk                    string    `json:"margin_risk"`
	OpportunityKobo               *int64    `json:"opportunity_kobo,omitempty"`
	Explanation                   string    `json:"explanation"`
	RuleID                        string    `json:"rule_id,omitempty"`
	RuleName                      string    `json:"rule_name,omitempty"`
	StockStatus                   string    `json:"stock_status"`
	StockQuantity                 *int      `json:"stock_quantity,omitempty"`
	Reasons                       []Reason  `json:"reasons"`
	GeneratedAt                   time.Time `json:"generated_at"`
	ExpiresAt                     time.Time `json:"expires_at"`
}

type InboxFilter struct {
	Urgency    string
	State      string
	MarginRisk string
	CategoryID string
	ProductID  string
	Limit      int
}

type ChangeRequest struct {
	ID                 string     `json:"id"`
	RecommendationID   string     `json:"recommendation_id"`
	ProductID          string     `json:"product_id"`
	ProductName        string     `json:"product_name"`
	PreviousPriceKobo  int64      `json:"previous_price_kobo"`
	RequestedPriceKobo int64      `json:"requested_price_kobo"`
	Status             string     `json:"status"`
	ScheduledFor       *time.Time `json:"scheduled_for,omitempty"`
	ExpiresAt          time.Time  `json:"expires_at"`
	ApprovedByUserID   string     `json:"approved_by_user_id,omitempty"`
	ApprovedRole       string     `json:"approved_role,omitempty"`
	ApprovalNote       string     `json:"approval_note,omitempty"`
	ExecutionID        string     `json:"execution_id,omitempty"`
	ExecutionStatus    string     `json:"execution_status,omitempty"`
	VerifiedAfterKobo  *int64     `json:"verified_after_price_kobo,omitempty"`
	RollbackID         string     `json:"rollback_id,omitempty"`
	RollbackStatus     string     `json:"rollback_status,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type Execution struct {
	ID                 string     `json:"id"`
	RequestID          string     `json:"price_change_request_id"`
	ProductID          string     `json:"product_id"`
	VariantID          string     `json:"variant_id,omitempty"`
	IdempotencyKey     string     `json:"idempotency_key"`
	Status             string     `json:"status"`
	BeforePriceKobo    int64      `json:"before_price_kobo"`
	RequestedPriceKobo int64      `json:"requested_price_kobo"`
	VerifiedAfterKobo  *int64     `json:"verified_after_price_kobo,omitempty"`
	Attempts           int        `json:"attempts"`
	ScheduledFor       *time.Time `json:"scheduled_for,omitempty"`
	LastError          string     `json:"last_error,omitempty"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type Rollback struct {
	ID               string     `json:"id"`
	ExecutionID      string     `json:"execution_id"`
	ProductID        string     `json:"product_id"`
	RestorePriceKobo int64      `json:"restore_price_kobo"`
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	LastError        string     `json:"last_error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

type KillSwitch struct {
	Scope     string    `json:"scope"`
	Enabled   bool      `json:"enabled"`
	Reason    string    `json:"reason"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ApprovalLimit struct {
	RoleID           string `json:"role_id"`
	MaximumChangeBPS int    `json:"maximum_change_bps"`
	MaximumPriceKobo *int64 `json:"maximum_price_kobo,omitempty"`
}
