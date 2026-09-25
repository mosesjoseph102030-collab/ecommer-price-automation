package woocommerce

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// NewHTTPClient blocks private, loopback, link-local, and unspecified targets
// after DNS resolution, closing SSRF and DNS-rebinding gaps. Local development
// may explicitly allow private targets for a self-hosted WordPress install.
func NewHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if ip := net.ParseIP(host); ip != nil {
				if !allowPrivate && blockedIP(ip) {
					return nil, fmt.Errorf("blocked private network target")
				}
				return dialer.DialContext(ctx, network, address)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !allowPrivate && blockedIP(ip.IP) {
					return nil, fmt.Errorf("blocked private network target")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout: 25 * time.Second, Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if _, err := NormalizeStoreURL(req.URL.String(), req.URL.Scheme == "http" && allowPrivate, allowPrivate); err != nil {
				return err
			}
			return nil
		},
	}
}

func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
