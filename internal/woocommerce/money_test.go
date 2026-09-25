package woocommerce

import "testing"

func TestMoneyToKoboUsesExactDecimalArithmetic(t *testing.T) {
	cases := map[string]int64{"": 0, "0": 0, "10": 1000, "10.25": 1025, "0.01": 1, "123456789.99": 12345678999}
	for input, want := range cases {
		got, err := MoneyToKobo(input)
		if err != nil {
			t.Fatalf("MoneyToKobo(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("MoneyToKobo(%q)=%d want %d", input, got, want)
		}
	}
	for _, invalid := range []string{"10.001", "NaN", "1e", "-0.01"} {
		if _, err := MoneyToKobo(invalid); err == nil {
			t.Errorf("MoneyToKobo(%q) should fail", invalid)
		}
	}
}

func TestKoboToMoney(t *testing.T) {
	if got := KoboToMoney(1025); got != "10.25" {
		t.Errorf("got %q", got)
	}
	if got := KoboToMoney(5); got != "0.05" {
		t.Errorf("got %q", got)
	}
}
