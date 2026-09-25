package competitor

import "testing"

func TestApprovedDomainValidation(t *testing.T) {
	provider := NewProvider([]string{"approved.example"}, false)
	if _, err := provider.ValidateURL("https://shop.approved.example/product"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ValidateURL("https://approved.example.evil.test/product"); err == nil {
		t.Fatal("lookalike domain accepted")
	}
	if _, err := provider.ValidateURL("http://approved.example/product"); err == nil {
		t.Fatal("HTTP accepted in production mode")
	}
}
func TestPrivateTargetBlocked(t *testing.T) {
	provider := NewProvider([]string{"localhost"}, false)
	if _, err := provider.ValidateURL("https://localhost/product"); err == nil {
		t.Fatal("private source accepted")
	}
}
