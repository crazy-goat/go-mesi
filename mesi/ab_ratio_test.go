package mesi

import (
	"errors"
	"strings"
	"testing"
)

func TestParseABValidRatio(t *testing.T) {
	cases := []struct {
		name   string
		ratio  string
		wantA  uint64
		wantB  uint64
	}{
		{"typical 70:30", "70:30", 70, 30},
		{"trivial 50:50", "50:50", 50, 50},
		{"asymmetric 0:100", "0:100", 0, 100},
		{"asymmetric 100:0", "100:0", 100, 0},
		{"with surrounding whitespace", "  90:10  ", 90, 10},
		{"single side allowed at max", "0:1000000", 0, 1_000_000},
		{"single side allowed at max swapped", "1000000:0", 1_000_000, 0},
		{"both at max", "1000000:1000000", 1_000_000, 1_000_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := &esiIncludeToken{ABRatio: tc.ratio}
			ratio, err := token.parseAB()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ratio.A != tc.wantA {
				t.Errorf("A = %d, want %d", ratio.A, tc.wantA)
			}
			if ratio.B != tc.wantB {
				t.Errorf("B = %d, want %d", ratio.B, tc.wantB)
			}
		})
	}
}

func TestParseABEmptyDefaultsTo5050(t *testing.T) {
	token := &esiIncludeToken{ABRatio: ""}
	ratio, err := token.parseAB()
	if err != nil {
		t.Fatalf("empty ratio should default to 50:50 without error, got: %v", err)
	}
	if ratio.A != 50 || ratio.B != 50 {
		t.Errorf("default = (%d,%d), want (50,50)", ratio.A, ratio.B)
	}
}

func TestParseABWhitespaceOnlyDefaultsTo5050(t *testing.T) {
	token := &esiIncludeToken{ABRatio: "   "}
	ratio, err := token.parseAB()
	if err != nil {
		t.Fatalf("whitespace-only ratio should default to 50:50 without error, got: %v", err)
	}
	if ratio.A != 50 || ratio.B != 50 {
		t.Errorf("default = (%d,%d), want (50,50)", ratio.A, ratio.B)
	}
}

func TestParseABRejectsInvalidRatio(t *testing.T) {
	cases := []struct {
		name            string
		ratio           string
		wantMsgContains string
	}{
		{"missing colon", "7030", "':' separator"},
		{"too many parts", "70:30:10", "got 3"},
		{"non-numeric A", "abc:def", "A side"},
		{"non-numeric B", "70:abc", "B side"},
		{"both zero", "0:0", "no traffic"},
		{"decimal A", "70.5:29.5", "non-integer"},
		{"decimal B", "70:29.5", "non-integer"},
		{"negative A", "-5:10", "negative"},
		{"negative B", "5:-10", "negative"},
		// MaxUint64 (a valid unsigned integer) is rejected by the value-bounds
		// check because it exceeds MaxABRatio. The point of this case is
		// "a face-plausible number that doesn't fit our policy is refused".
		{"uint64 overflow", "18446744073709551615:1", "exceeds maximum"},
		{"A above max", "1000001:1", "exceeds maximum"},
		{"B above max", "1:1000001", "exceeds maximum"},
		{"empty A side", ":50", "empty value"},
		{"empty B side", "50:", "empty value"},
		{"untrimmed negative", "-1 : 2", "negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := &esiIncludeToken{ABRatio: tc.ratio}
			_, err := token.parseAB()
			if err == nil {
				t.Fatalf("expected error for input %q, got nil (silent fallback to default)", tc.ratio)
			}
			var ierr *ErrInvalidABRatio
			if !errors.As(err, &ierr) {
				t.Fatalf("error type = %T, want *ErrInvalidABRatio", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsgContains) {
				t.Errorf("error message %q does not contain %q", err.Error(), tc.wantMsgContains)
			}
			if ierr.Input != tc.ratio {
				t.Errorf("ErrInvalidABRatio.Input = %q, want %q (operator must see what they typed)", ierr.Input, tc.ratio)
			}
		})
	}
}

func TestParseABBoundaryValuesTakeExactlyMaxPlusOne(t *testing.T) {
	// Accepted: MaxABRatio on both sides
	{
		token := &esiIncludeToken{ABRatio: "1000000:1000000"}
		ratio, err := token.parseAB()
		if err != nil {
			t.Fatalf("MaxABRatio:MaxABRatio must be accepted, got: %v", err)
		}
		if ratio.A != MaxABRatio || ratio.B != MaxABRatio {
			t.Errorf("got (%d,%d), want (%d,%d)", ratio.A, ratio.B, MaxABRatio, MaxABRatio)
		}
	}
	// Rejected: MaxABRatio+1 on either side
	{
		token := &esiIncludeToken{ABRatio: "1000001:1"}
		if _, err := token.parseAB(); err == nil {
			t.Error("MaxABRatio+1 must be rejected")
		}
	}
	{
		token := &esiIncludeToken{ABRatio: "1:1000001"}
		if _, err := token.parseAB(); err == nil {
			t.Error("MaxABRatio+1 on B side must be rejected")
		}
	}
}

func TestSelectUrlChoosesBasedOnRatio(t *testing.T) {
	token := &esiIncludeToken{Src: "/src.html", Alt: "/alt.html"}

	t.Run("rng_in_a_range_selects_src", func(t *testing.T) {
		ratio := abRatio{A: 80, B: 20}
		for i := uint64(0); i < 80; i++ {
			rng := func(int) int { return int(i) }
			selected := ratio.selectUrl(token, rng)
			if selected != "/src.html" {
				t.Errorf("rng=%d: expected src, got %q", i, selected)
			}
		}
	})

	t.Run("rng_in_b_range_selects_alt", func(t *testing.T) {
		ratio := abRatio{A: 80, B: 20}
		for i := uint64(80); i < 100; i++ {
			rng := func(int) int { return int(i) }
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

	t.Run("max_sum_a_saturating_rng_returns_src", func(t *testing.T) {
		ratio := abRatio{A: MaxABRatio, B: 0}
		// A path always picks Src regardless of rng
		selected := ratio.selectUrl(token, func(int) int { return int(MaxABRatio) - 1 })
		if selected != "/src.html" {
			t.Errorf("expected src when A=MaxABRatio, got %q", selected)
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
