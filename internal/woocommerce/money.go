package woocommerce

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// MoneyToKobo converts a provider decimal string to integer minor units without
// ever using float64. WooCommerce normally returns two decimal places.
func MoneyToKobo(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, fmt.Errorf("invalid money value %q", raw)
	}
	r.Mul(r, big.NewRat(100, 1))
	if !r.IsInt() {
		// More precision than the tenant currency supports: reject rather than round silently.
		return 0, fmt.Errorf("money value %q has more than two decimal places", raw)
	}
	if r.Sign() < 0 {
		return 0, fmt.Errorf("money value cannot be negative")
	}
	n := r.Num()
	if !n.IsInt64() {
		return 0, fmt.Errorf("money value %q is out of range", raw)
	}
	return n.Int64(), nil
}

func KoboToMoney(k int64) string {
	return strconv.FormatInt(k/100, 10) + "." + fmt.Sprintf("%02d", absInt64(k%100))
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
