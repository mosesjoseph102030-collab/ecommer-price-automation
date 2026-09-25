package competitor

import "testing"

func TestSuggestMatchExactSKU(t *testing.T) {
	match := SuggestMatch("Cable", "SKU-1", []CatalogProduct{{ID: "p1", Name: "Different", SKU: "SKU-1"}})
	if match == nil || match.ProductID != "p1" || match.ConfidenceBPS != 9200 {
		t.Fatalf("unexpected %#v", match)
	}
}
func TestSuggestMatchExactName(t *testing.T) {
	match := SuggestMatch("USB C Cable", "", []CatalogProduct{{ID: "p1", Name: "usb-c cable", SKU: ""}})
	if match == nil || match.ConfidenceBPS != 9500 {
		t.Fatalf("unexpected %#v", match)
	}
}
func TestNoLowConfidenceMatch(t *testing.T) {
	if match := SuggestMatch("Totally Different", "", []CatalogProduct{{ID: "p1", Name: "USB Cable"}}); match != nil {
		t.Fatalf("unexpected match %#v", match)
	}
}
