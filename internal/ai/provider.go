package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Request is a single provider call. Facts are already minimized and redacted by
// the caller; Untrusted text has already passed ScreenUntrusted and is fenced by
// WrapUntrusted. The provider performs no interpretation of its own.
type Request struct {
	Feature   string
	System    string
	Prompt    string
	MaxTokens int
}

// Usage is what the provider reports back, used for cost accounting.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Result is a raw provider response plus usage metadata.
type Result struct {
	Raw     []byte
	Model   string
	Usage   Usage
	Latency time.Duration
}

// Provider is the boundary to an external model. Implementations must never log
// credentials and must never return provider errors verbatim to end users.
type Provider interface {
	Name() string
	Model() string
	Complete(ctx context.Context, req Request) (Result, error)
	TemplateVersion(feature string) string
}

// ErrProviderUnavailable is returned for transport failures so the caller can log
// a generic outcome instead of leaking provider internals.
var ErrProviderUnavailable = errors.New("model provider unavailable")

// ---------------------------------------------------------------------------
// Anthropic-compatible Messages API client
// ---------------------------------------------------------------------------

// HTTPProvider talks to an Anthropic-compatible /v1/messages endpoint. The API
// key is supplied per call site from server-side config and is never persisted,
// logged, or returned to a client.
type HTTPProvider struct {
	BaseURL   string
	APIKey    string
	ModelName string
	Client    *http.Client
	// CostPerInputToken and CostPerOutputToken are in micros of the tenant
	// currency, used for the required cost log column.
	CostPerInputTokenMicros  int64
	CostPerOutputTokenMicros int64
}

// NewHTTPProvider builds a provider with a locked-down transport: HTTPS only,
// no private networks, bounded redirects, and a hard response cap.
func NewHTTPProvider(baseURL, apiKey, model string, allowPrivate bool) (*HTTPProvider, error) {
	trimmed := strings.TrimSpace(baseURL)
	if !strings.HasPrefix(trimmed, "https://") {
		if !allowPrivate {
			return nil, errors.New("AI provider base URL must use HTTPS")
		}
		if !strings.HasPrefix(trimmed, "http://") {
			return nil, errors.New("AI provider base URL must be an absolute http(s) URL")
		}
	}
	if apiKey == "" {
		return nil, errors.New("AI provider API key is required")
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
						return nil, fmt.Errorf("blocked private provider target")
					}
				}
			}
			return dialer.DialContext(ctx, network, address)
		},
	}
	return &HTTPProvider{
		BaseURL:   strings.TrimRight(trimmed, "/"),
		APIKey:    apiKey,
		ModelName: model,
		Client:    &http.Client{Timeout: 60 * time.Second, Transport: transport},
	}, nil
}

func (p *HTTPProvider) Name() string  { return "anthropic" }
func (p *HTTPProvider) Model() string { return p.ModelName }

func (p *HTTPProvider) TemplateVersion(feature string) string {
	return "ft:" + feature + ":v1"
}

type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type string `json:"type"`
		Msg  string `json:"message"`
	} `json:"error"`
}

func (p *HTTPProvider) Complete(ctx context.Context, req Request) (Result, error) {
	if len(req.Prompt) > 200000 {
		return Result{}, fmt.Errorf("prompt too large: %d bytes", len(req.Prompt))
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body, err := json.Marshal(messagesRequest{
		Model: p.ModelName, MaxTokens: maxTokens, System: req.System,
		Messages: []message{{Role: "user", Content: req.Prompt}},
	})
	if err != nil {
		return Result{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Result{}, ErrProviderUnavailable
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("user-agent", "PricingIntelligence/7.0")

	started := time.Now()
	res, err := p.Client.Do(httpReq)
	if err != nil {
		// Never surface transport detail (which can include the URL) upward.
		return Result{}, ErrProviderUnavailable
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Result{}, ErrProviderUnavailable
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Result{}, ErrProviderUnavailable
	}
	var parsed messagesResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Result{}, ErrProviderUnavailable
	}
	if parsed.Error != nil {
		return Result{}, ErrProviderUnavailable
	}
	text := ""
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	return Result{
		Raw:     []byte(text),
		Model:   p.ModelName,
		Usage:   Usage{InputTokens: parsed.Usage.InputTokens, OutputTokens: parsed.Usage.OutputTokens},
		Latency: time.Since(started),
	}, nil
}

// CostMicros estimates cost for the required invocation cost log column.
func (p *HTTPProvider) CostMicros(usage Usage) int64 {
	return int64(usage.InputTokens)*p.CostPerInputTokenMicros + int64(usage.OutputTokens)*p.CostPerOutputTokenMicros
}

// ---------------------------------------------------------------------------
// StaticProvider
// ---------------------------------------------------------------------------

// StaticProvider returns a fixed response. It exists so the whole Phase 7
// governance path (flags, kill switch, quotas, logging, draft lifecycle) can be
// exercised end to end in development and CI without a provider key or any
// outbound network call.
type StaticProvider struct {
	Response []byte
	ModelID  string
}

func (p *StaticProvider) Name() string { return "static" }
func (p *StaticProvider) Model() string {
	if p.ModelID == "" {
		return "static-1"
	}
	return p.ModelID
}
func (p *StaticProvider) TemplateVersion(feature string) string { return "ft:" + feature + ":static" }
func (p *StaticProvider) Complete(ctx context.Context, req Request) (Result, error) {
	out := p.Response
	if len(out) == 0 {
		out = []byte(`{"summary":"Static provider response.","bullets":[]}`)
	}
	return Result{Raw: out, Model: p.Model(), Latency: time.Millisecond}, nil
}
