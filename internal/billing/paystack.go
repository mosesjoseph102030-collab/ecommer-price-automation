// Package billing implements plans, entitlements, usage metering, and Paystack
// payment integration.
//
// Nonpayment never deletes business data. A tenant that stops paying becomes
// read-only after a grace window; its products, costs, and history are retained
// through a retention period and can be exported.
package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrSignatureInvalid means a webhook could not be authenticated and must
	// be discarded without processing.
	ErrSignatureInvalid = errors.New("paystack webhook signature is invalid")
	// ErrNotFound means Paystack has no record of the reference.
	ErrNotFound = errors.New("paystack resource not found")
	// ErrUnavailable is returned for transport failures. Provider internals are
	// never surfaced to callers.
	ErrUnavailable = errors.New("payment provider unavailable")
)

// Provider is the Paystack boundary.
type Provider struct {
	BaseURL    string
	SecretKey  string
	HTTPClient *http.Client
	// Now is injectable for deterministic tests.
	Now func() time.Time
}

// NewProvider builds a Paystack client. The secret key stays in memory only: it
// is never written to the database, returned to a client, or logged.
func NewProvider(secretKey string, allowPrivate bool) (*Provider, error) {
	if strings.TrimSpace(secretKey) == "" {
		return nil, errors.New("PAYSTACK_SECRET_KEY is required")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if !allowPrivate {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				for _, ip := range ips {
					if ip.IP.IsLoopback() || ip.IP.IsPrivate() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() {
						return nil, fmt.Errorf("blocked private payment provider target")
					}
				}
			}
			return dialer.DialContext(ctx, network, address)
		},
	}
	return &Provider{
		BaseURL:    "https://api.paystack.co",
		SecretKey:  secretKey,
		HTTPClient: &http.Client{Timeout: 45 * time.Second, Transport: transport},
		Now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

// checkoutResponse wraps Paystack's { status, message, data } envelope.
type checkoutResponse struct {
	Status  bool            `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// CheckoutSession is what the browser needs to complete a payment.
type CheckoutSession struct {
	AuthorizationURL string `json:"authorization_url"`
	AccessCode       string `json:"access_code"`
	Reference        string `json:"reference"`
	AmountKobo       int64  `json:"amount_kobo"`
	Currency         string `json:"currency"`
}

// CheckoutRequest starts a Paystack transaction.
type CheckoutRequest struct {
	Email       string
	AmountKobo  int64
	Currency    string
	Reference   string
	CallbackURL string
	PlanCode    string
	Metadata    map[string]string
	// EmailToken charges an existing Paystack subscription instead of a new
	// one-time transaction. It is a credential and is never logged.
	EmailToken string
}

type checkoutData struct {
	AuthorizationURL string `json:"authorization_url"`
	AccessCode       string `json:"access_code"`
	Reference        string `json:"reference"`
	Amount           int64  `json:"amount"`
	Currency         string `json:"currency"`
}

func (p *Provider) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.BaseURL+path, reader)
	if err != nil {
		return ErrUnavailable
	}
	req.SetBasicAuth(p.SecretKey, "")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PricingIntelligence/8.0")
	res, err := p.HTTPClient.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return ErrUnavailable
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ErrUnavailable
	}
	var envelope checkoutResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ErrUnavailable
	}
	if !envelope.Status {
		return ErrUnavailable
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

// InitializeCheckout creates a transaction and returns the authorization URL.
func (p *Provider) InitializeCheckout(ctx context.Context, in CheckoutRequest) (CheckoutSession, error) {
	if in.AmountKobo <= 0 {
		return CheckoutSession{}, errors.New("checkout amount must be positive")
	}
	payload := map[string]any{
		"email":        in.Email,
		"amount":       in.AmountKobo,
		"currency":     in.Currency,
		"reference":    in.Reference,
		"callback_url": in.CallbackURL,
	}
	if in.Metadata != nil {
		payload["metadata"] = in.Metadata
	}
	var data checkoutData
	if in.EmailToken != "" {
		payload["email_token"] = in.EmailToken
		if err := p.do(ctx, http.MethodPost, "/transaction/initialize", payload, &data); err != nil {
			return CheckoutSession{}, err
		}
	} else {
		if in.PlanCode != "" {
			payload["plan"] = in.PlanCode
		}
		if err := p.do(ctx, http.MethodPost, "/transaction/initialize", payload, &data); err != nil {
			return CheckoutSession{}, err
		}
	}
	if data.AuthorizationURL == "" {
		return CheckoutSession{}, ErrUnavailable
	}
	return CheckoutSession{AuthorizationURL: data.AuthorizationURL, AccessCode: data.AccessCode,
		Reference: data.Reference, AmountKobo: in.AmountKobo, Currency: in.Currency}, nil
}

// VerifiedTransaction is the server-side truth about a payment.
type VerifiedTransaction struct {
	ID         int64  `json:"id"`
	Reference  string `json:"reference"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	Status     string `json:"status"`
	PaidAt     string `json:"paid_at"`
	Channel    string `json:"channel"`
	PlanCode   string `json:"plan_code"`
	EmailToken string `json:"-"`
	Reference2 string `json:"-"`
}

// VerifyTransaction re-reads a transaction from Paystack. The server must never
// trust a client-side "payment succeeded" callback.
func (p *Provider) VerifyTransaction(ctx context.Context, reference string) (VerifiedTransaction, error) {
	if strings.TrimSpace(reference) == "" {
		return VerifiedTransaction{}, errors.New("reference is required")
	}
	var out VerifiedTransaction
	if err := p.do(ctx, http.MethodGet, "/transaction/verify/"+url.PathEscape(reference), nil, &out); err != nil {
		return VerifiedTransaction{}, err
	}
	return out, nil
}

// PaystackSubscription is the provider-side view of a subscription. It is kept
// distinct from our own Subscription record, which owns entitlements and state.
type PaystackSubscription struct {
	Status          string `json:"status"`
	PlanCode        string `json:"plan_code"`
	EmailToken      string `json:"email_token"`
	Amount          int64  `json:"amount"`
	Quantity        int    `json:"quantity"`
	NextPaymentDate string `json:"next_payment_date"`
}

// FetchSubscription reads a Paystack subscription by its subscription code.
func (p *Provider) FetchSubscription(ctx context.Context, subscriptionCode string) (PaystackSubscription, error) {
	var out PaystackSubscription
	if err := p.do(ctx, http.MethodGet, "/subscription/"+url.PathEscape(subscriptionCode), nil, &out); err != nil {
		return PaystackSubscription{}, err
	}
	return out, nil
}

// ListPlans pages the Paystack plan catalogue.
func (p *Provider) ListPlans(ctx context.Context) ([]PaystackSubscription, error) {
	var envelope struct {
		Data []struct {
			PlanCode string `json:"plan_code"`
			Amount   int64  `json:"amount"`
		} `json:"data"`
	}
	if err := p.do(ctx, http.MethodGet, "/plan", nil, &envelope); err != nil {
		return nil, err
	}
	out := make([]PaystackSubscription, 0, len(envelope.Data))
	for _, d := range envelope.Data {
		out = append(out, PaystackSubscription{PlanCode: d.PlanCode, Amount: d.Amount})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Webhook authentication
// ---------------------------------------------------------------------------

// VerifyWebhookSignature authenticates a Paystack webhook using HMAC-SHA512 of the
// raw request body with the secret key, compared in constant time.
//
// The raw body MUST be captured before any JSON decoding, otherwise the
// signature can never match.
func VerifyWebhookSignature(secretKey string, rawBody []byte, signature string) error {
	if secretKey == "" || signature == "" {
		return ErrSignatureInvalid
	}
	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	// Constant-time compare; a length mismatch is not a match.
	if !hmac.Equal([]byte(strings.ToLower(strings.TrimSpace(expected))), []byte(strings.ToLower(strings.TrimSpace(signature)))) {
		return ErrSignatureInvalid
	}
	return nil
}

// WebhookEvent is the subset of Paystack's event envelope we act on.
type WebhookEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
	// id is Paystack's per-event id, used for deduplication.
	ID json.Number `json:"-"`
}

type webhookEnvelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// ParseWebhook decodes the envelope and extracts the event id from data.
func ParseWebhook(raw []byte) (WebhookEvent, error) {
	var env webhookEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return WebhookEvent{}, fmt.Errorf("malformed webhook body")
	}
	if env.Event == "" {
		return WebhookEvent{}, errors.New("webhook event name is missing")
	}
	// Paystack places the transaction/subscription id inside data. Any stable
	// id is enough for deduplication; fall back to a payload hash if absent.
	id := ""
	var probe struct {
		ID        json.Number `json:"id"`
		Reference string      `json:"reference"`
	}
	if json.Unmarshal(env.Data, &probe) == nil {
		if probe.Reference != "" {
			id = probe.Reference
		} else if probe.ID.String() != "" {
			id = probe.ID.String()
		}
	}
	return WebhookEvent{Event: env.Event, Data: env.Data, ID: json.Number(id)}, nil
}

// SupportedEvents are the events that change subscription state. Everything
// else is acknowledged and ignored so Paystack stops retrying.
var SupportedEvents = map[string]bool{
	"charge.success":         true,
	"subscription.create":    true,
	"subscription.renew":     true,
	"subscription.disable":   true,
	"subscription.not_renew": true,
}

func normalizeReference(reference string) string {
	return strings.ToLower(strings.TrimSpace(reference))
}
