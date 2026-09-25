package competitor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Provider struct {
	AllowedDomains []string
	AllowPrivate   bool
	Client         *http.Client
}

func NewProvider(domains []string, allowPrivate bool) *Provider {
	allowed := append([]string(nil), domains...)
	for i := range allowed {
		allowed[i] = strings.ToLower(strings.TrimSpace(allowed[i]))
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !allowPrivate && blockedIP(ip.IP) {
					return nil, fmt.Errorf("blocked private target")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &Provider{AllowedDomains: allowed, AllowPrivate: allowPrivate, Client: &http.Client{
		Timeout: 20 * time.Second, Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			host := strings.ToLower(req.URL.Hostname())
			for _, domain := range allowed {
				if host == domain || strings.HasSuffix(host, "."+domain) {
					return nil
				}
			}
			return fmt.Errorf("redirect left approved source domains")
		},
	}}
}

func (p *Provider) ValidateURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid competitor URL")
	}
	if u.Scheme != "https" && !(p.AllowPrivate && u.Scheme == "http") {
		return "", fmt.Errorf("competitor URL must use HTTPS")
	}
	host := strings.ToLower(u.Hostname())
	allowed := false
	for _, domain := range p.AllowedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("competitor domain is not on the approved source allowlist")
	}
	if !p.AllowPrivate && (host == "localhost" || strings.HasSuffix(host, ".local")) {
		return "", fmt.Errorf("private competitor target is blocked")
	}
	ip := net.ParseIP(host)
	if !p.AllowPrivate && ip != nil && blockedIP(ip) {
		return "", fmt.Errorf("private competitor target is blocked")
	}
	u.Fragment = ""
	return u.String(), nil
}

func (p *Provider) Fetch(ctx context.Context, rawURL string) ([]byte, string, int, error) {
	validated, err := p.ValidateURL(rawURL)
	if err != nil {
		return nil, "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, validated, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("User-Agent", "PricingIntelligenceBot/1.0 (+approved-source-monitoring)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	res, err := p.Client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, "", res.StatusCode, fmt.Errorf("competitor source returned HTTP %d", res.StatusCode)
	}
	contentType := res.Header.Get("Content-Type")
	lowerType := strings.ToLower(contentType)
	if contentType != "" && !strings.Contains(lowerType, "text/html") && !strings.Contains(lowerType, "application/xhtml+xml") {
		return nil, contentType, res.StatusCode, fmt.Errorf("unsupported competitor content type")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil {
		return nil, contentType, res.StatusCode, err
	}
	if len(body) > 2<<20 {
		return nil, contentType, res.StatusCode, fmt.Errorf("competitor response exceeds 2MB")
	}
	return body, contentType, res.StatusCode, nil
}

func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
