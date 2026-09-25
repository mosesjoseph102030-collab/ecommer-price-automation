package themes

import (
	"fmt"
	"math"
)

// Theme is a curated platform-approved theme. No arbitrary user hex at Phase 1.
type Theme struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Primary      string `json:"primary"`
	PrimaryHover string `json:"primary_hover"`
	Soft         string `json:"soft"`
}

// Curated returns the 3 launch themes.
func Curated() []Theme {
	return []Theme{
		{ID: "teal-ledger", Name: "Teal Ledger", Primary: "#0F766E", PrimaryHover: "#115E59", Soft: "#CCFBF1"},
		{ID: "navy-commerce", Name: "Navy Commerce", Primary: "#1E3A8A", PrimaryHover: "#1E40AF", Soft: "#DBEAFE"},
		{ID: "forest-margin", Name: "Forest Margin", Primary: "#166534", PrimaryHover: "#14532D", Soft: "#DCFCE7"},
	}
}

func Get(id string) (Theme, error) {
	for _, t := range Curated() {
		if t.ID == id {
			return t, nil
		}
	}
	return Theme{}, fmt.Errorf("unknown theme %q: choose from teal-ledger, navy-commerce, forest-margin", id)
}

// ContrastRatio computes WCAG relative-luminance contrast between two hex colors.
// Used to gate theme selection: normal text needs >= 4.5, large text >= 3.
func ContrastRatio(hexA, hexB string) (float64, error) {
	la, err := luminance(hexA)
	if err != nil {
		return 0, err
	}
	lb, err := luminance(hexB)
	if err != nil {
		return 0, err
	}
	hi, lo := la, lb
	if lb > la {
		hi, lo = lb, la
	}
	return (hi + 0.05) / (lo + 0.05), nil
}

func luminance(hex string) (float64, error) {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return 0, fmt.Errorf("invalid hex color %q", hex)
	}
	lin := func(c float64) float64 {
		c /= 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(float64(r)) + 0.7152*lin(float64(g)) + 0.0722*lin(float64(b)), nil
}
