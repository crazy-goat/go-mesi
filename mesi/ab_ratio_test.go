package mesi

import (
	"testing"
)

func TestParseABValidRatio(t *testing.T) {
	token := &esiIncludeToken{ABRatio: "70:30"}
	ratio := token.parseAB()
	if ratio.A != 70 {
		t.Errorf("A = %d, want 70", ratio.A)
	}
	if ratio.B != 30 {
		t.Errorf("B = %d, want 30", ratio.B)
	}
}

func TestParseABInvalidRatioDefaults(t *testing.T) {
	cases := []struct {
		name  string
		ratio string
		wantA uint
		wantB uint
	}{
		{"missing colon", "7030", 50, 50},
		{"too many parts", "70:30:10", 50, 50},
		{"non-numeric", "abc:def", 50, 50},
		{"empty string", "", 50, 50},
		{"both zero", "0:0", 50, 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := &esiIncludeToken{ABRatio: tc.ratio}
			ratio := token.parseAB()
			if ratio.A != tc.wantA {
				t.Errorf("A = %d, want %d", ratio.A, tc.wantA)
			}
			if ratio.B != tc.wantB {
				t.Errorf("B = %d, want %d", ratio.B, tc.wantB)
			}
		})
	}
}

func TestSelectUrlChoosesBasedOnRatio(t *testing.T) {
	token := &esiIncludeToken{Src: "/src.html", Alt: "/alt.html"}

	t.Run("rng_in_a_range_selects_src", func(t *testing.T) {
		ratio := abRatio{A: 80, B: 20}
		for i := 0; i < 80; i++ {
			rng := func(int) int { return i }
			selected := ratio.selectUrl(token, rng)
			if selected != "/src.html" {
				t.Errorf("rng=%d: expected src, got %q", i, selected)
			}
		}
	})

	t.Run("rng_in_b_range_selects_alt", func(t *testing.T) {
		ratio := abRatio{A: 80, B: 20}
		for i := 80; i < 100; i++ {
			rng := func(int) int { return i }
			selected := ratio.selectUrl(token, rng)
			if selected != "/alt.html" {
				t.Errorf("rng=%d: expected alt, got %q", i, selected)
			}
		}
	})

	t.Run("no_alt_returns_src_regardless_of_rng", func(t *testing.T) {
		noAltToken := &esiIncludeToken{Src: "/src.html", Alt: ""}
		ratio := abRatio{A: 50, B: 50}
		for _, v := range []int{0, 49, 50, 99} {
			rng := func(int) int { return v }
			selected := ratio.selectUrl(noAltToken, rng)
			if selected != "/src.html" {
				t.Errorf("rng=%d: expected src when no alt, got %q", v, selected)
			}
		}
	})

	t.Run("zero_sum_returns_src", func(t *testing.T) {
		ratio := abRatio{A: 0, B: 0}
		for _, v := range []int{0, 50} {
			rng := func(int) int { return v }
			selected := ratio.selectUrl(token, rng)
			if selected != "/src.html" {
				t.Errorf("rng=%d: expected src when sum=0, got %q", v, selected)
			}
		}
	})
}

func TestSelectUrlNoAltReturnsSrc(t *testing.T) {
	token := &esiIncludeToken{Src: "/src.html", Alt: ""}
	ratio := abRatio{A: 50, B: 50}
	selected := ratio.selectUrl(token, nil)
	if selected != "/src.html" {
		t.Errorf("selectUrl() = %q, want %q", selected, "/src.html")
	}
}
