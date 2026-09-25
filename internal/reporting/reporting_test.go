package reporting

import "testing"

// CSV exports are opened in spreadsheet software, so a cell beginning with
// =, +, -, or @ would execute as a formula. This is a real injection vector
// because report cells include user-authored names and error strings.
func TestSanitizeCellNeutralizesFormulaInjection(t *testing.T) {
	dangerous := []string{"=1+1", "+cmd|calc", "-2+3", "@SUM(A1)", "\tvalue", "\rvalue"}
	for _, cell := range dangerous {
		got := SanitizeCell(cell)
		if got == cell {
			t.Errorf("formula prefix not neutralized: %q", cell)
		}
		if got[0] != '\'' {
			t.Errorf("expected leading apostrophe for %q, got %q", cell, got)
		}
	}
}

func TestSanitizeCellKeepsSafeValues(t *testing.T) {
	safe := []string{"Cable", "12000.00", "below_floor", "2026-01-02T03:04:05Z", "in stock", ""}
	for _, cell := range safe {
		if got := SanitizeCell(cell); got != cell {
			t.Errorf("safe cell must be unchanged: %q -> %q", cell, got)
		}
	}
}

func TestMoneyIsIntegerKoboOnly(t *testing.T) {
	cases := map[int64]string{0: "0.00", 5: "0.05", 100: "1.00", 1450000: "14500.00", -250: "-2.50"}
	for kobo, want := range cases {
		if got := Money(kobo); got != want {
			t.Errorf("Money(%d) = %q, want %q", kobo, got, want)
		}
	}
}

func TestKindListIsClosedSet(t *testing.T) {
	for _, kind := range []string{"margin", "below_floor", "competitor_gap", "price_change_history",
		"publish_reliability", "competitor_health", "recommendation_outcomes", "stock_opportunity"} {
		if !ValidKind(kind) {
			t.Errorf("expected %q to be a supported report", kind)
		}
	}
	for _, kind := range []string{"", "drop table", "margin; --", "unknown"} {
		if ValidKind(kind) {
			t.Errorf("expected %q to be rejected", kind)
		}
	}
}

func TestCSVHasHeaderAndRows(t *testing.T) {
	report := Report{
		Kind:    "margin",
		Columns: []string{"product_name", "price"},
		Rows:    [][]string{{"=HYPERLINK(\"http://evil\",\"x\")", "100.00"}},
	}
	out, err := report.CSV()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("expected CSV output")
	}
	if got := string(out); !contains(got, "product_name,price") {
		t.Errorf("missing header: %q", got)
	}
	if !contains(string(out), "'=HYPERLINK") {
		t.Errorf("formula was not sanitized in CSV output: %q", string(out))
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
