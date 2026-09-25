package themes

import "testing"

func TestCuratedCount(t *testing.T) {
	if n := len(Curated()); n < 2 || n > 4 {
		t.Errorf("launch with 2-4 themes, got %d", n)
	}
}

func TestContrastGate(t *testing.T) {
	for _, th := range Curated() {
		ratio, err := ContrastRatio(th.Primary, "#FFFFFF")
		if err != nil {
			t.Fatalf("%s: %v", th.ID, err)
		}
		// Primary is used on soft backgrounds + large text; enforce 3:1 minimum.
		if ratio < 3.0 {
			t.Errorf("%s contrast %f below 3:1", th.ID, ratio)
		}
	}
}
