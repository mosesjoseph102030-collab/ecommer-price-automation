package slug

import "testing"

func TestGenerate(t *testing.T) {
	cases := map[string]string{
		"Bright Tech Accessories": "bright-tech-accessories",
		"Aboki Home Store!":       "aboki-home-store",
		"Solar Pro Offa":          "solar-pro-offa",
	}
	for in, want := range cases {
		if got := Generate(in); got != want {
			t.Errorf("Generate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateReserved(t *testing.T) {
	for _, s := range []string{"admin", "api", "app", "login", "signup", "pricing", "support", "community"} {
		if err := Validate(s); err == nil {
			t.Errorf("Validate(%q) should reject reserved word", s)
		}
	}
	if err := Validate("bright-tech-accessories"); err != nil {
		t.Errorf("valid slug rejected: %v", err)
	}
}
