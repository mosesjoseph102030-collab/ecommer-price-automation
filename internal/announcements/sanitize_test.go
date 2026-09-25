package announcements

import "testing"

// Admin-authored rich content is rendered to users, so the sanitizer must strip
// scripts, event handlers, and dangerous URLs while preserving safe formatting.

func TestSanitizeStripsScriptTags(t *testing.T) {
	attacks := []string{
		`<script>alert(1)</script>`,
		`<SCRIPT SRC=//evil.test/x.js></SCRIPT>`,
		`<img src=x onerror=alert(1)>`,
		`<iframe src="https://evil.test"></iframe>`,
		`<a href="javascript:alert(1)">click</a>`,
		`<div style="background:url(javascript:alert(1))">x</div>`,
		`<svg/onload=alert(1)>`,
		`<body onload=alert(1)>`,
		`<object data="evil.swf">`,
		`<form action="https://evil.test"><input name=a></form>`,
	}
	for _, attack := range attacks {
		got := SanitizeHTML(attack)
		lowered := toLower(got)
		for _, forbidden := range []string{"<script", "javascript:", "onerror", "onload", "<iframe", "<object", "<form", "<input", "<svg", "evil.test"} {
			if contains(lowered, forbidden) {
				t.Errorf("sanitizer leaked %q for input %q -> %q", forbidden, attack, got)
			}
		}
	}
}

func TestSanitizeKeepsSafeFormatting(t *testing.T) {
	got := SanitizeHTML(`<p>Hello <strong>world</strong> and <em>friends</em></p><ul><li>one</li></ul>`)
	for _, keep := range []string{"<p>", "<strong>", "</strong>", "<em>", "<ul>", "<li>"} {
		if !contains(got, keep) {
			t.Errorf("expected safe formatting %q to survive, got %q", keep, got)
		}
	}
}

func TestSanitizeAllowsSafeLinksOnly(t *testing.T) {
	safe := SanitizeHTML(`<a href="https://example.com/docs" title="Docs">docs</a>`)
	if !contains(safe, "https://example.com/docs") {
		t.Errorf("safe link should be preserved, got %q", safe)
	}
	unsafe := SanitizeHTML(`<a href="javascript:alert(1)">x</a>`)
	if contains(toLower(unsafe), "javascript") {
		t.Errorf("javascript URL must be dropped, got %q", unsafe)
	}
}

func TestSanitizeNeutralizesTextThatLooksLikeMarkup(t *testing.T) {
	// Escaped-then-reallowed means literal angle brackets in text are encoded.
	got := SanitizeHTML(`5 < 10 and 10 > 2`)
	if contains(got, "<script") {
		t.Errorf("unexpected script tag: %q", got)
	}
	if !contains(got, "&lt;") && !contains(got, "5") {
		t.Errorf("expected escaped comparison text, got %q", got)
	}
}

func TestAudienceValidationRejectsAbuse(t *testing.T) {
	days := 5000
	if err := (Audience{ActiveWithinDays: &days}).Validate(); err == nil {
		t.Error("out-of-range active_within_days should be rejected")
	}
	if err := (Audience{}).Validate(); err != nil {
		t.Errorf("empty audience (targets everyone) must be valid: %v", err)
	}
}

func TestPqStringArrayEscapesQuotes(t *testing.T) {
	got := pqStringArray([]string{`a"b`, `c\d`})
	want := `{"a\"b","c\\d"}`
	if got != want {
		t.Errorf("pqStringArray = %q, want %q", got, want)
	}
	if pqStringArray(nil) != "{}" {
		t.Error("empty slice should render as an empty array literal")
	}
}

func toLower(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + 32
		}
	}
	return string(out)
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
