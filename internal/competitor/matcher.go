package competitor

import (
	"strings"
	"unicode"
)

type CatalogProduct struct{ ID, Name, SKU string }
type MatchSuggestion struct {
	ProductID     string
	ConfidenceBPS int
	Reason        string
}

func SuggestMatch(observedName, observedSKU string, catalog []CatalogProduct) *MatchSuggestion {
	name := normalizeName(observedName)
	sku := strings.ToLower(strings.TrimSpace(observedSKU))
	best := (*MatchSuggestion)(nil)
	for _, product := range catalog {
		confidence, reason := 0, ""
		if sku != "" && strings.EqualFold(sku, strings.TrimSpace(product.SKU)) {
			confidence, reason = 9200, "exact SKU"
		} else if name != "" && name == normalizeName(product.Name) {
			confidence, reason = 9500, "exact normalized name"
		} else {
			score := tokenSimilarity(name, normalizeName(product.Name))
			if score > 0 {
				confidence, reason = score, "name token similarity"
			}
		}
		if confidence >= 5000 && (best == nil || confidence > best.ConfidenceBPS) {
			best = &MatchSuggestion{ProductID: product.ID, ConfidenceBPS: confidence, Reason: reason}
		}
	}
	return best
}

func normalizeName(value string) string {
	var b strings.Builder
	previousSpace := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			previousSpace = false
		} else if !previousSpace && b.Len() > 0 {
			b.WriteRune(' ')
			previousSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}
func tokenSimilarity(a, b string) int {
	at := strings.Fields(a)
	bt := strings.Fields(b)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, v := range at {
		set[v] = true
	}
	intersection := 0
	for _, v := range bt {
		if set[v] {
			intersection++
		}
	}
	union := len(set) + len(bt) - intersection
	if union == 0 {
		return 0
	}
	return intersection * 8000 / union
}
