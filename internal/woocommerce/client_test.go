package woocommerce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientPaginationAndBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "ck" || pass != "cs" {
			t.Errorf("bad basic auth")
		}
		w.Header().Set("X-WP-Total", "2")
		w.Header().Set("X-WP-TotalPages", "2")
		page := r.URL.Query().Get("page")
		if page == "1" {
			_ = json.NewEncoder(w).Encode([]Product{{ID: 1, Name: "One"}})
			return
		}
		_ = json.NewEncoder(w).Encode([]Product{{ID: 2, Name: "Two"}})
	}))
	defer server.Close()
	client := NewClient(server.URL, "ck", "cs", server.Client())
	products, err := client.ListProducts(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 2 || products[1].ID != 2 {
		t.Fatalf("unexpected products: %#v", products)
	}
}

func TestClientAuthAndRateLimitErrors(t *testing.T) {
	t.Run("auth", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "denied", http.StatusUnauthorized) }))
		defer server.Close()
		err := NewClient(server.URL, "x", "y", server.Client()).Test(context.Background())
		if !IsAuthError(err) {
			t.Fatalf("expected auth error, got %v", err)
		}
	})
	t.Run("rate limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "12")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()
		err := NewClient(server.URL, "x", "y", server.Client()).Test(context.Background())
		if !IsRateLimited(err) {
			t.Fatalf("expected rate limit error, got %v", err)
		}
	})
}
