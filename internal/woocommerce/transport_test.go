package woocommerce

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientBlocksPrivateTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	if _, err := NewHTTPClient(false).Get(server.URL); err == nil {
		t.Fatal("private target was reachable")
	}
	res, err := NewHTTPClient(true).Get(server.URL)
	if err != nil {
		t.Fatalf("development private target blocked: %v", err)
	}
	res.Body.Close()
}

func TestHTTPClientHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if _, err := NewHTTPClient(false).Do(req); err == nil {
		t.Fatal("cancelled request was not rejected")
	}
}
