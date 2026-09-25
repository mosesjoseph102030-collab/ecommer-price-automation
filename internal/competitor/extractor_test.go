package competitor

import "testing"

func TestExtractJSONLDProduct(t *testing.T) {
	body := []byte(`<html><head><script type="application/ld+json">{"@type":"Product","name":"USB-C Cable","sku":"CBL-1","offers":{"@type":"Offer","price":"14.50","priceCurrency":"NGN","availability":"https://schema.org/InStock"}}</script></head></html>`)
	product, err := ExtractProduct(body)
	if err != nil {
		t.Fatal(err)
	}
	if product.Name != "USB-C Cable" || product.SKU != "CBL-1" || product.PriceKobo == nil || *product.PriceKobo != 1450 {
		t.Fatalf("unexpected %#v", product)
	}
	if product.Availability != "instock" || product.Currency != "NGN" {
		t.Fatalf("unexpected availability/currency %#v", product)
	}
}
func TestExtractIgnoresPriceRange(t *testing.T) {
	body := []byte(`<script type="application/ld+json">{"@type":"Product","name":"Cable","offers":{"lowPrice":"10.00","highPrice":"20.00","priceCurrency":"NGN"}}</script>`)
	product, err := ExtractProduct(body)
	if err != nil {
		t.Fatal(err)
	}
	if product.PriceKobo != nil {
		t.Fatalf("range became exact price: %d", *product.PriceKobo)
	}
}
func TestExtractOpenGraphFallback(t *testing.T) {
	body := []byte(`<meta property="og:title" content="Charger"><meta property="product:price:amount" content="25.00"><meta property="product:price:currency" content="NGN">`)
	product, err := ExtractProduct(body)
	if err != nil {
		t.Fatal(err)
	}
	if product.PriceKobo == nil || *product.PriceKobo != 2500 {
		t.Fatalf("unexpected %#v", product)
	}
}
func TestMoneyRejectsFloatPrecision(t *testing.T) {
	if _, err := MoneyToKobo("10.001"); err == nil {
		t.Fatal("precision accepted")
	}
}
