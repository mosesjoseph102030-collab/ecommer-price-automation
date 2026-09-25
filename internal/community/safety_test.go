package community

import (
	"strings"
	"testing"
)

// The specification requires community to allow general strategy but prohibit
// coordination, customer allocation, competitor confidentiality, and price
// fixing across sellers. These tests pin both directions: the allowed cases must
// not be over-blocked, and the prohibited cases must not slip through.

func TestAllowsGeneralStrategyDiscussion(t *testing.T) {
	allowed := []string{
		"How do you handle Black Friday promotions without destroying your margins?",
		"I bundle a free shipping threshold at ₦25,000 to lift average order value.",
		"Conversion improved a lot after I changed the product page hero image.",
		"What inventory turnover target are you using for slow-moving SKUs?",
		"I raise prices in small steps around seasonal demand peaks.",
		"My payment provider fees went up, so I recalculated all my margins.",
	}
	for _, text := range allowed {
		if verdict := ScreenContent(text); !verdict.Allowed {
			t.Errorf("general strategy must be allowed: %q (blocked as %s)", text, verdict.Category)
		}
	}
}

func TestBlocksCoordinatedPricing(t *testing.T) {
	blocked := []string{
		"Let's all agree to keep our prices at the same level next month.",
		"We should all set the same price for this product to win the market.",
		"I posted in the price war channel to coordinate our prices.",
		"Everyone here should agree on a minimum price for the category.",
	}
	for _, text := range blocked {
		if verdict := ScreenContent(text); verdict.Allowed {
			t.Errorf("coordinated pricing must be blocked: %q", text)
		}
	}
}

func TestBlocksPriceFixingInstructions(t *testing.T) {
	blocked := []string{
		"We should all raise our prices by 10% next week.",
		"Never go below 4500 on this item, agreed?",
		"Set a price floor at 5000 for everyone selling this product.",
		"Follow each other on prices and don't undercut.",
	}
	for _, text := range blocked {
		if verdict := ScreenContent(text); verdict.Allowed {
			t.Errorf("cross-seller price fixing must be blocked: %q", text)
		}
	}
}

func TestBlocksCustomerAllocation(t *testing.T) {
	blocked := []string{
		"Let's split the customers between us by region.",
		"Give your customers to me and I'll send you mine.",
		"Territory allocation agreement for the Lagos market.",
	}
	for _, text := range blocked {
		if verdict := ScreenContent(text); verdict.Allowed {
			t.Errorf("customer allocation must be blocked: %q", text)
		}
	}
}

func TestBlocksCompetitorConfidentiality(t *testing.T) {
	blocked := []string{
		"Our competitor's margin is 40% according to their NDA.",
		"Here is our competitor's supplier pricing, confidential.",
		"I can share the competitor contract terms I have.",
	}
	for _, text := range blocked {
		if verdict := ScreenContent(text); verdict.Allowed {
			t.Errorf("competitor confidentiality must be blocked: %q", text)
		}
	}
}

func TestBlocksPersonalData(t *testing.T) {
	blocked := []string{
		"Email me at owner@example.com about the deal.",
		"Call me on 08031234567 to discuss.",
	}
	for _, text := range blocked {
		if verdict := ScreenContent(text); verdict.Allowed {
			t.Errorf("personal contact details must be blocked: %q", text)
		}
	}
}

// Obfuscation must not defeat the filter.
func TestBlocksObfuscatedCoordination(t *testing.T) {
	blocked := []string{
		"p r i c e  f i x i n g starts now",
		"let-s-all-agree-to-match-prices",
		"c-o-o-r-d-i-n-a-t-e  our  prices",
	}
	for _, text := range blocked {
		if verdict := ScreenContent(text); verdict.Allowed {
			t.Errorf("obfuscated coordination must still be blocked: %q", text)
		}
	}
}

func TestEmptyContentBlocked(t *testing.T) {
	if ScreenContent("   \n\t ").Allowed {
		t.Error("empty content must not be published")
	}
}

func TestRedactionRemovesCommercialFigures(t *testing.T) {
	redacted := RedactPrivateTerms("My landed cost is ₦10,900 and the competitor is NGN 15200, email me at a@b.com")
	for _, leak := range []string{"10,900", "15200", "a@b.com"} {
		if contains(redacted, leak) {
			t.Errorf("redaction leaked %q: %s", leak, redacted)
		}
	}
}

func TestRedactionCatchesBareFiguresNearCommercialTerms(t *testing.T) {
	cases := map[string]string{
		"my cost is 4500":     "4500",
		"price: 12000":        "12000",
		"revenue was 9500000": "9500000",
	}
	for input, leak := range cases {
		if got := RedactPrivateTerms(input); contains(got, leak) {
			t.Errorf("bare figure near a commercial term must be redacted: %q -> %q", input, got)
		}
	}
}

func TestRedactionKeepsOrdinaryCounts(t *testing.T) {
	// Over-redaction destroys useful moderation context, so plain counts stay.
	for _, input := range []string{"I have 3 products in this bundle", "5 SKUs need review", "12 orders today"} {
		if got := RedactPrivateTerms(input); strings.Contains(got, "[amount]") {
			t.Errorf("ordinary count must not be redacted: %q -> %q", input, got)
		}
	}
}

func TestExcerptIsBounded(t *testing.T) {
	long := ""
	for i := 0; i < 500; i++ {
		long += "word "
	}
	if got := len([]rune(Excerpt(long))); got > 170 {
		t.Errorf("excerpt must be bounded, got %d runes", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
