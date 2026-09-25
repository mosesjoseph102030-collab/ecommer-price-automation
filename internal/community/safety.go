// Package community implements verified-owner rooms with moderation and an
// explicit content safety policy.
//
// Safety requirement from the specification: community discussion may cover
// general strategy (margins, promotions, bundles, conversion, inventory) but
// must prohibit coordinated pricing, customer allocation, sharing confidential
// competitor agreements, and instructions to fix prices across sellers.
package community

import (
	"regexp"
	"strings"
	"unicode"
)

// ViolationCategory identifies a prohibited discussion type.
type ViolationCategory string

const (
	ViolationNone           ViolationCategory = ""
	ViolationCoordinated    ViolationCategory = "coordinated_pricing"
	ViolationAllocation     ViolationCategory = "customer_allocation"
	ViolationCompetitorDeal ViolationCategory = "competitor_confidentiality"
	ViolationPriceFixing    ViolationCategory = "cross_seller_price_fixing"
	ViolationPersonalData   ViolationCategory = "personal_data"
	ViolationAbuse          ViolationCategory = "abusive"
)

// Verdict is the result of screening community content.
type Verdict struct {
	Allowed  bool
	Category ViolationCategory
	Reason   string
}

var (
	// Each entry is a set of patterns for one prohibited category.
	coordinatedPatterns = []string{
		`(?i)\b(?:let'?s|lets)\s+all\s+(?:agree|match|set|keep)\b`,
		`(?i)\blet\b\s*'?s?\s+all\s+(?:agree|match|set|keep)\b`,
		`(?i)\b(?:we|all\s+of\s+us|everyone|all\s+sellers|competitors|other\s+stores?)\s+(?:\w+\s+)?(?:should|must|will|need\s+to|can)\s+(?:all\s+)?(?:agree|coordinate|collude|match|align|set\s+the\s+same)\b`,
		`(?i)\bshould\s+agree\s+on\s+(?:a\s+|the\s+)?(?:minimum|maximum|max|min|ceiling|floor)\b`,
		`(?i)\b(?:minimum|max\w*)\s+price\s+(?:for\s+everyone|for\s+all|across\s+sellers|across\s+the\s+market)\b`,
		`(?i)\b(?:agree(?:d|ment)?|consensus|collusion)\s+(?:on\s+)?prices?\b`,
		`(?i)\bprice[\s-]?(?:fixing|war|alliance|cartel|coordination|collusion)\b`,
		`(?i)\b(?:same|identical|uniform)\s+prices?\s+(?:for\s+all|across\s+sellers|everywhere|for\s+everyone)\b`,
		`(?i)\bco-?ordinat\w+\s+(?:our\s+|the\s+)?prices?\b`,
		`(?i)\b(?:unify|unifying|standardis\w+|standardiz\w+)\s+(?:our\s+|all\s+)?prices?\b`,
	}
	allocationPatterns = []string{
		`(?i)\b(?:split|splitting|divide|dividing|share|sharing|allocate|allocating|assign|assigning)\s+(?:the\s+|up\s+)?(?:customers?|leads?|orders?|territories|markets?)\b`,
		`(?i)\b(?:give|sending|refer|steer)\s+(?:your\s+|my\s+|the\s+)?(?:customers?|leads?|orders?)\s+to\s+me\b`,
		`(?i)\bpoach(?:ing)?\s+(?:each\s+other'?s?\s+)?customers?\b`,
		`(?i)\b(?:territory|territories)\s+(?:split|agreement|allocation)\b`,
		`(?i)\bcustomer\s+allocation\b`,
	}
	competitorPatterns = []string{
		`(?i)\b(?:our|my)\s+(?:competitor'?s?|competitors'?)\s+(?:margin|price|revenue|volume|cost|supplier|contract)\b`,
		`(?i)\b(?:under\s+nda|confidential|non-?disclos\w+|private)\s+(?:competitor|supplier|vendor)\b`,
		`(?i)\bcompetitor'?s?\s+(?:nda|contract|agreement|supplier|contact|terms)\b`,
		`(?i)\b(?:leak|leaking|share|sharing|post|posting|publish)\s+(?:the\s+|their\s+)?competitor'?s?\b`,
		`(?i)\bcompetitor\s+confidential\w*\b`,
	}
	priceFixingPatterns = []string{
		`(?i)\b(?:we|all\s+sellers|everyone|all\s+of\s+us)\s+should\s+(?:all\s+)?(?:raise|increase|cut|drop|lower|reduce)\s+(?:our\s+|the\s+)?prices?\b`,
		`(?i)\b(?:keep|holding|maintain)\s+(?:our\s+|the\s+)?prices?\s+(?:at|above|below|over)\s+\d`,
		`(?i)\b(?:never|do\s+not|don'?t)\s+go\s+below\s+\d`,
		`(?i)\b(?:set|fix|pin(?:ning)?)\s+(?:a\s+)?(?:price\s+)?(?:floor|minimum|ceiling)\s+(?:at|for|to)\b`,
		`(?i)\bfollow\s+(?:each\s+other|one\s+another|the\s+leader)\s+(?:on\s+)?prices?\b`,
	}
	personalDataPatterns = []string{
		`(?i)\b[\w.+-]+@[\w-]+\.[a-z]{2,}\b`,
		`(?i)\b(?:\+?234|0)[789]\d{9}\b`,
	}
	abusePatterns = []string{
		`(?i)\b(?:idiot|moron|stupid|scammer|shut up|fuck you|fu+ck off)\b`,
	}

	compiled = map[ViolationCategory][]*regexp.Regexp{}
)

// obfuscationChars are replaced with spaces before screening so that
// "c-o-o-r-d-i-n-a-t-e" and "p r i c e f i x i n g" cannot bypass the filter.
// Apostrophes are preserved so possessive forms ("competitor's") still match.
var obfuscationChars = regexp.MustCompile(`[^a-z0-9@.+' ]+`)

// singleLetterRun is retained for documentation of the evasion class; the
// joining logic itself lives in DespaceLetters.
var singleLetterRun = regexp.MustCompile(`(?:^| )([a-z](?: [a-z]){2,})(?: |$)`)

var (
	emailPattern = regexp.MustCompile(`(?i)\b[\w.+-]+@[\w-]+\.[a-z]{2,}\b`)
	phonePattern = regexp.MustCompile(`\b(?:\+?234|0)[789]\d{9}\b`)
)

func init() {
	compiled[ViolationCoordinated] = compileAll(coordinatedPatterns)
	compiled[ViolationAllocation] = compileAll(allocationPatterns)
	compiled[ViolationCompetitorDeal] = compileAll(competitorPatterns)
	compiled[ViolationPriceFixing] = compileAll(priceFixingPatterns)
	compiled[ViolationPersonalData] = compileAll(personalDataPatterns)
	compiled[ViolationAbuse] = compileAll(abusePatterns)
}

func compileAll(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			out = append(out, re)
		}
	}
	return out
}

// ScreenContent decides whether a community post or reply is allowed.
// Screening is conservative: ambiguous coordination is blocked and sent to
// moderation rather than published.
func ScreenContent(text string) Verdict {
	normalized := Normalize(text)
	if strings.TrimSpace(normalized) == "" {
		return Verdict{Allowed: false, Category: ViolationNone, Reason: "post is empty"}
	}
	// Screen both the plain normalization and a de-obfuscated variant so that
	// "p r i c e  f i x i n g" is caught as well as "price fixing".
	variants := []string{normalized, DespaceLetters(normalized)}
	// Order matters: the most serious categories are evaluated first.
	order := []ViolationCategory{
		ViolationPersonalData,
		ViolationCompetitorDeal,
		ViolationCoordinated,
		ViolationPriceFixing,
		ViolationAllocation,
		ViolationAbuse,
	}
	reasons := map[ViolationCategory]string{
		ViolationPersonalData:   "do not share personal contact details in community",
		ViolationCompetitorDeal: "do not share confidential competitor or supplier information",
		ViolationCoordinated:    "coordinated pricing between sellers is not allowed",
		ViolationPriceFixing:    "agreeing prices across sellers is not allowed",
		ViolationAllocation:     "allocating or sharing customers is not allowed",
		ViolationAbuse:          "abusive language is not allowed",
	}
	for _, category := range order {
		for _, variant := range variants {
			for _, re := range compiled[category] {
				if re.MatchString(variant) {
					return Verdict{Allowed: false, Category: category, Reason: reasons[category]}
				}
			}
		}
	}
	return Verdict{Allowed: true}
}

// Normalize lowercases text and turns punctuation separators into spaces so
// that "c-o-o-r-d-i-n-a-t-e" becomes "c o o r d i n a t e" (word boundaries are
// preserved for pattern matching, and DespaceLetters re-joins them).
func Normalize(text string) string {
	lowered := strings.ToLower(text)
	lowered = obfuscationChars.ReplaceAllString(lowered, " ")
	return strings.Join(strings.FieldsFunc(lowered, unicode.IsSpace), " ")
}

// DespaceLetters joins runs of single letters back into words so that
// "c o o r d i n a t e" screens as "coordinate". Implemented by hand because a
// single regex cannot match two adjacent runs without consuming the separator.
func DespaceLetters(text string) string {
	words := strings.Split(text, " ")
	out := make([]string, 0, len(words))
	for i := 0; i < len(words); {
		if isSingleLetter(words[i]) {
			j := i
			for j < len(words) && isSingleLetter(words[j]) {
				j++
			}
			run := j - i
			if run >= 3 {
				var b strings.Builder
				for k := i; k < j; k++ {
					b.WriteString(words[k])
				}
				out = append(out, b.String())
				i = j
				continue
			}
		}
		out = append(out, words[i])
		i++
	}
	return strings.Join(out, " ")
}

func isSingleLetter(word string) bool {
	if len(word) != 1 {
		return false
	}
	r := rune(word[0])
	return r >= 'a' && r <= 'z'
}

// moneyWithSymbol matches figures that carry an explicit currency marker.
var moneyWithSymbol = regexp.MustCompile(`(?i)(?:naira|ngn|₦|\$|£|€)\s?\d[\d,]*(?:\.\d+)?|\b\d{4,}\s?(?:kobo|naira)\b`)

// moneyContextWords are the commercial terms that make a nearby bare number a
// commercial figure rather than an ordinary count.
var moneyContextWords = []string{"cost", "costs", "price", "prices", "revenue", "margin", "supplier", "landed", "profit", "turnover", "kobo"}

// bareNumber matches 3+ digit groups that may be prices or costs.
var bareNumber = regexp.MustCompile(`\b\d[\d,]{2,}\b`)

// RedactPrivateTerms removes obvious tenant-private commercial figures from
// text before it is stored in a shared table or shown outside the owner's own
// tenant. This is a defence-in-depth measure; the primary guarantee is that no
// tenant data is ever loaded into a community response in the first place.
func RedactPrivateTerms(text string) string {
	out := moneyWithSymbol.ReplaceAllString(text, "[amount]")
	// A bare "4500" is ambiguous, so only redact it when a commercial term
	// appears in the same sentence. This keeps counts like "3 products" intact
	// while still catching "my cost is 4500".
	matches := bareNumber.FindAllStringIndex(out, -1)
	var b strings.Builder
	last := 0
	for _, m := range matches {
		number := out[m[0]:m[1]]
		window := out[max(0, m[0]-60):m[0]]
		if hasMoneyContext(window) {
			b.WriteString(out[last:m[0]])
			b.WriteString("[amount]")
			last = m[1]
			continue
		}
		_ = number
	}
	b.WriteString(out[last:])
	out = b.String()
	out = emailPattern.ReplaceAllString(out, "[contact]")
	out = phonePattern.ReplaceAllString(out, "[contact]")
	return out
}

func hasMoneyContext(window string) bool {
	lowered := strings.ToLower(window)
	for _, word := range moneyContextWords {
		if strings.Contains(lowered, word) {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Truncate shortens text for previews without splitting runes.
func Truncate(text string, max int) string {
	runes := []rune(strings.TrimSpace(text))
	if max <= 0 || len(runes) <= max {
		return string(runes)
	}
	return strings.TrimRight(string(runes[:max]), " ") + "…"
}

// Excerpt builds a moderation preview.
func Excerpt(text string) string {
	return Truncate(RedactPrivateTerms(Normalize(text)), 160)
}
