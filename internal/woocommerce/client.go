package woocommerce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"automation/internal/reliability"
)

const apiRoot = "/wp-json/wc/v3"

type Client struct {
	BaseURL    string
	Key        string
	Secret     string
	HTTPClient *http.Client
	// Breaker guards this store. A nil breaker disables circuit breaking, which
	// is what a caller gets when it has no registry.
	Breaker *reliability.Breaker
}

type APIError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
	RequestID  string
	Malformed  bool
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("woocommerce API returned HTTP %d", e.Status)
	}
	return e.Message
}

func IsAuthError(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden)
}

func IsRateLimited(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusTooManyRequests
}

func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

func NormalizeStoreURL(raw string, allowHTTP, allowPrivate bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", errors.New("enter a valid WooCommerce store URL")
	}
	if u.User != nil {
		return "", errors.New("store URL must not contain credentials")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return "", errors.New("WooCommerce URL must use HTTPS")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("store URL must not include query parameters or fragments")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" && !allowPrivate {
		return "", errors.New("private and local store addresses are blocked")
	}
	if strings.HasSuffix(host, ".local") && !allowPrivate {
		return "", errors.New("private and local store addresses are blocked")
	}
	if ip := net.ParseIP(host); ip != nil && !allowPrivate {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return "", errors.New("private and local store addresses are blocked")
		}
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

func NewClient(baseURL, key, secret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = NewHTTPClient(false)
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, Secret: secret, HTTPClient: httpClient}
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	// A tripped breaker short-circuits before any network call. This is the
	// difference between graceful degradation and a retry storm: an unreachable
	// store stops costing timeouts instead of multiplying them.
	if c.Breaker != nil && !c.Breaker.Allow() {
		return nil, fmt.Errorf("%w: this store's connection is temporarily paused after repeated failures", reliability.ErrCircuitOpen)
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Key, c.Secret)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "PricingIntelligence/2.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		c.recordBreaker(fmt.Errorf("woocommerce request failed: %w", err))
		return nil, fmt.Errorf("woocommerce request failed: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		ae := &APIError{Status: res.StatusCode, Message: safeProviderMessage(raw), RetryAfter: parseRetryAfter(res.Header.Get("Retry-After"))}
		if len(raw) > 0 && !json.Valid(raw) {
			ae.Malformed = true
		}
		// Only infrastructure failures count toward the breaker. A 404 for a
		// deleted product, or a 400 for bad input, says nothing about whether
		// the store is reachable, and tripping on those would disable a
		// perfectly healthy store.
		if isInfrastructureFailure(ae.Status) {
			c.recordBreaker(ae)
		} else {
			c.recordBreaker(nil)
		}
		return nil, ae
	}
	c.recordBreaker(nil)
	return res, nil
}

func (c *Client) recordBreaker(err error) {
	if c.Breaker != nil {
		c.Breaker.Record(err)
	}
}

// isInfrastructureFailure reports whether an HTTP status indicates the store or
// the path to it is unhealthy, rather than that the request itself was wrong.
func isInfrastructureFailure(status int) bool {
	switch {
	case status == http.StatusTooManyRequests:
		return true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		// Credentials can be revoked by the store owner at any time, which
		// genuinely is an outage for this integration.
		return true
	case status >= 500:
		return true
	default:
		return false
	}
}

func safeProviderMessage(raw []byte) string {
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil && body.Message != "" {
		return body.Message
	}
	return ""
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func (c *Client) GetProduct(ctx context.Context, id int64) (Product, error) {
	res, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/products/%d", apiRoot, id), nil)
	if err != nil {
		return Product{}, err
	}
	defer res.Body.Close()
	var product Product
	if err := json.NewDecoder(res.Body).Decode(&product); err != nil {
		return Product{}, &APIError{Status: 200, Message: "WooCommerce returned malformed product data", Malformed: true}
	}
	return product, nil
}

// UpdateRegularPrice changes only the regular price. Sale-price policy conflicts
// are checked by the publishing service before this method is called.
func (c *Client) UpdateRegularPrice(ctx context.Context, id int64, priceKobo int64) (Product, error) {
	if priceKobo < 0 {
		return Product{}, errors.New("price cannot be negative")
	}
	res, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/products/%d", apiRoot, id), map[string]any{"regular_price": KoboToMoney(priceKobo)})
	if err != nil {
		return Product{}, err
	}
	defer res.Body.Close()
	var product Product
	if err := json.NewDecoder(res.Body).Decode(&product); err != nil {
		return Product{}, &APIError{Status: 200, Message: "WooCommerce returned malformed update response", Malformed: true}
	}
	return product, nil
}

// GetVariation fetches the current server-side variant state.
func (c *Client) GetVariation(ctx context.Context, productID, variationID int64) (Variant, error) {
	res, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/products/%d/variations/%d", apiRoot, productID, variationID), nil)
	if err != nil {
		return Variant{}, err
	}
	defer res.Body.Close()
	var v Variant
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		return Variant{}, &APIError{Status: 200, Message: "WooCommerce returned malformed variant data", Malformed: true}
	}
	return v, nil
}

// UpdateVariationRegularPrice updates only the variant regular price.
func (c *Client) UpdateVariationRegularPrice(ctx context.Context, productID, variationID, priceKobo int64) (Variant, error) {
	if priceKobo < 0 {
		return Variant{}, errors.New("price cannot be negative")
	}
	res, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/products/%d/variations/%d", apiRoot, productID, variationID), map[string]any{"regular_price": KoboToMoney(priceKobo)})
	if err != nil {
		return Variant{}, err
	}
	defer res.Body.Close()
	var v Variant
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		return Variant{}, &APIError{Status: 200, Message: "WooCommerce returned malformed variant update response", Malformed: true}
	}
	return v, nil
}

func (c *Client) Test(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, apiRoot+"/products?per_page=1&page=1", nil)
	return err
}

type Product struct {
	ID            int64       `json:"id"`
	Name          string      `json:"name"`
	SKU           string      `json:"sku"`
	Status        string      `json:"status"`
	Type          string      `json:"type"`
	RegularPrice  string      `json:"regular_price"`
	SalePrice     string      `json:"sale_price"`
	Price         string      `json:"price"`
	StockStatus   string      `json:"stock_status"`
	StockQuantity *int        `json:"stock_quantity"`
	TaxClass      string      `json:"tax_class"`
	DateModified  string      `json:"date_modified"`
	Categories    []Term      `json:"categories"`
	Images        []MediaItem `json:"images"`
	Variations    []int64     `json:"variations"`
}

type Term struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type MediaItem struct {
	ID       int64  `json:"id"`
	Src      string `json:"src"`
	Alt      string `json:"alt"`
	Position int    `json:"position"`
}

type Variant struct {
	ID            int64              `json:"id"`
	SKU           string             `json:"sku"`
	RegularPrice  string             `json:"regular_price"`
	SalePrice     string             `json:"sale_price"`
	Price         string             `json:"price"`
	StockStatus   string             `json:"stock_status"`
	StockQuantity *int               `json:"stock_quantity"`
	DateModified  string             `json:"date_modified"`
	Attributes    []VariantAttribute `json:"attributes"`
}

type VariantAttribute struct {
	Name   string `json:"name"`
	Option string `json:"option"`
}

type Page[T any] struct {
	Items      []T
	Total      int
	TotalPages int
}

func (c *Client) ListCategories(ctx context.Context) ([]Term, error) {
	var all []Term
	for page := 1; page <= 1000; page++ {
		res, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/products/categories?per_page=100&page=%d", apiRoot, page), nil)
		if err != nil {
			return nil, err
		}
		var batch []Term
		err = json.NewDecoder(res.Body).Decode(&batch)
		res.Body.Close()
		if err != nil {
			return nil, &APIError{Status: 200, Message: "WooCommerce returned malformed category data", Malformed: true}
		}
		all = append(all, batch...)
		totalPages := totalPages(res)
		if len(batch) == 0 || totalPages == 0 || page >= totalPages {
			break
		}
	}
	return all, nil
}

func (c *Client) ListProducts(ctx context.Context, modifiedAfter string) ([]Product, error) {
	var all []Product
	for page := 1; page <= 10000; page++ {
		path := fmt.Sprintf("%s/products?per_page=100&page=%d&status=any", apiRoot, page)
		if modifiedAfter != "" {
			path += "&modified_after=" + url.QueryEscape(modifiedAfter)
		}
		res, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var batch []Product
		err = json.NewDecoder(res.Body).Decode(&batch)
		total, pages := totals(res)
		res.Body.Close()
		if err != nil {
			return nil, &APIError{Status: 200, Message: "WooCommerce returned malformed product data", Malformed: true}
		}
		all = append(all, batch...)
		_ = total
		if len(batch) == 0 || pages == 0 || page >= pages {
			break
		}
	}
	return all, nil
}

func (c *Client) ListVariations(ctx context.Context, productID int64) ([]Variant, error) {
	var all []Variant
	for page := 1; page <= 1000; page++ {
		res, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/products/%d/variations?per_page=100&page=%d", apiRoot, productID, page), nil)
		if err != nil {
			return nil, err
		}
		var batch []Variant
		err = json.NewDecoder(res.Body).Decode(&batch)
		pages := totalPages(res)
		res.Body.Close()
		if err != nil {
			return nil, &APIError{Status: 200, Message: "WooCommerce returned malformed variation data", Malformed: true}
		}
		all = append(all, batch...)
		if len(batch) == 0 || pages == 0 || page >= pages {
			break
		}
	}
	return all, nil
}

func (c *Client) CreateWebhook(ctx context.Context, callback, topic, secret string) error {
	body := map[string]any{
		"name":         "Pricing Intelligence catalog updates",
		"topic":        topic,
		"delivery_url": callback,
		"secret":       secret,
		"status":       "active",
	}
	res, err := c.do(ctx, http.MethodPost, apiRoot+"/webhooks", body)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func totals(res *http.Response) (int, int) {
	total, _ := strconv.Atoi(res.Header.Get("X-WP-Total"))
	pages, _ := strconv.Atoi(res.Header.Get("X-WP-TotalPages"))
	return total, pages
}

func totalPages(res *http.Response) int {
	_, pages := totals(res)
	return pages
}
