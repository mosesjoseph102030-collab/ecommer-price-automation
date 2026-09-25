package competitor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var (
	jsonLDPattern            = regexp.MustCompile(`(?is)<script[^>]*type\s*=\s*["']application/ld\+json["'][^>]*>(.*?)</script>`)
	priceMetaPattern         = regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)\s*=\s*["']product:price:amount["'][^>]+content\s*=\s*["']([^"']+)["']`)
	priceMetaReversedPattern = regexp.MustCompile(`(?is)<meta[^>]+content\s*=\s*["']([^"']+)["'][^>]+(?:property|name)\s*=\s*["']product:price:amount["']`)
	currencyMetaPattern      = regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)\s*=\s*["']product:price:currency["'][^>]+content\s*=\s*["']([^"']+)["']`)
	nameMetaPattern          = regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)\s*=\s*["']og:title["'][^>]+content\s*=\s*["']([^"']+)["']`)
	availabilityMetaPattern  = regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)\s*=\s*["'](?:product:availability|og:availability:amount)["'][^>]+content\s*=\s*["']([^"']+)["']`)
)

func ExtractProduct(body []byte) (ExtractedProduct, error) {
	matches := jsonLDPattern.FindAllSubmatch(body, -1)
	for _, match := range matches {
		decoder := json.NewDecoder(bytes.NewReader(match[1]))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil {
			continue
		}
		if product, ok := findJSONLDProduct(value); ok {
			if product.PriceKobo == nil && product.Name == "" {
				continue
			}
			return product, nil
		}
	}
	name := firstMatch(nameMetaPattern, body)
	priceRaw := firstMatch(priceMetaPattern, body)
	if priceRaw == "" {
		priceRaw = firstMatch(priceMetaReversedPattern, body)
	}
	if name == "" && priceRaw == "" {
		return ExtractedProduct{}, errors.New("no supported structured product data found")
	}
	result := ExtractedProduct{Name: strings.TrimSpace(name), Currency: strings.ToUpper(strings.TrimSpace(firstMatch(currencyMetaPattern, body))), Availability: normalizeAvailability(firstMatch(availabilityMetaPattern, body)), ExtractionConfidenceBPS: 6500}
	if priceRaw != "" {
		price, err := MoneyToKobo(priceRaw)
		if err != nil {
			return ExtractedProduct{}, err
		}
		result.PriceKobo = &price
	}
	return result, nil
}

func findJSONLDProduct(value any) (ExtractedProduct, bool) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if result, ok := findJSONLDProduct(item); ok {
				return result, true
			}
		}
	case map[string]any:
		if typeContainsProduct(typed["@type"]) {
			result := ExtractedProduct{Name: strings.TrimSpace(stringValue(typed["name"])), SKU: strings.TrimSpace(stringValue(typed["sku"])), ExtractionConfidenceBPS: 9000}
			if offer := firstOffer(typed["offers"]); offer != nil {
				result.Currency = strings.ToUpper(strings.TrimSpace(stringValue(offer["priceCurrency"])))
				result.Availability = normalizeAvailability(stringValue(offer["availability"]))
				priceRaw := stringValue(offer["price"])
				low, high := stringValue(offer["lowPrice"]), stringValue(offer["highPrice"])
				if priceRaw == "" && low != "" && (high == "" || low == high) {
					priceRaw = low
				}
				// A range is not an exact observed price. Do not invent a midpoint.
				if priceRaw != "" {
					if price, err := MoneyToKobo(priceRaw); err == nil {
						result.PriceKobo = &price
					}
				}
			}
			return result, true
		}
		for _, child := range typed {
			if result, ok := findJSONLDProduct(child); ok {
				return result, true
			}
		}
	}
	return ExtractedProduct{}, false
}

func firstOffer(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case []any:
		for _, item := range typed {
			if offer, ok := item.(map[string]any); ok {
				return offer
			}
		}
	}
	return nil
}
func typeContainsProduct(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.EqualFold(typed, "Product") || strings.Contains(strings.ToLower(typed), "product")
	case []any:
		for _, item := range typed {
			if typeContainsProduct(item) {
				return true
			}
		}
	}
	return false
}
func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}
func firstMatch(pattern *regexp.Regexp, body []byte) string {
	match := pattern.FindSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return string(match[1])
}
func normalizeAvailability(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "instock") || strings.Contains(value, "in_stock"):
		return "instock"
	case strings.Contains(value, "outofstock") || strings.Contains(value, "out_of_stock"):
		return "outofstock"
	default:
		return "unknown"
	}
}

func MoneyToKobo(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("empty price")
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok || value.Sign() < 0 {
		return 0, fmt.Errorf("invalid price %q", raw)
	}
	value.Mul(value, big.NewRat(100, 1))
	if !value.IsInt() {
		return 0, fmt.Errorf("price %q has more than two decimal places", raw)
	}
	if !value.Num().IsInt64() {
		return 0, errors.New("price out of range")
	}
	return value.Num().Int64(), nil
}
