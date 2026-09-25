package woocommerce

import "testing"

func TestNormalizeStoreURL(t *testing.T) {
	got, err := NormalizeStoreURL(" https://Store.Example/shop/ ", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://store.example/shop" {
		t.Fatalf("got %q", got)
	}
	for _, raw := range []string{"http://store.example", "https://user:pass@store.example", "https://127.0.0.1", "https://localhost", "ftp://store.example"} {
		if _, err := NormalizeStoreURL(raw, false, false); err == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
}
