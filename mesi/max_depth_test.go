package mesi

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestParseMaxDepthEmptyAttributeIsNoOverride(t *testing.T) {
	// Empty / whitespace-only attributes are documented as "no override";
	// they must not error (operators expect the attribute to simply be
	// treated as absent) and must not change the parent config's depth.
	cases := []string{"", " ", "   ", "\t", " \t  "}
	for _, in := range cases {
		t.Run("input="+in, func(t *testing.T) {
			got, err := parseMaxDepth(in)
			if err != nil {
				t.Fatalf("empty-ish input %q must not error, got: %v", in, err)
			}
			if got != 0 {
				t.Errorf("empty-ish input %q must return 0, got %d", in, got)
			}
		})
	}
}

func TestParseMaxDepthAcceptsValidValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want uint64
	}{
		{"zero", "0", 0},
		{"small", "1", 1},
		{"mid", "42", 42},
		{"around max", "9999", 9_999},
		{"max allowed", "10000", 10_000},
		{"with surrounding whitespace", "  7  ", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMaxDepth(tc.in)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseMaxDepth(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseMaxDepthRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name            string
		in              string
		wantMsgContains string
	}{
		// Non-numeric input. strconv.ParseUint already returns
		// "invalid syntax" for these; our wrapper surfaces them as
		// "not an unsigned integer".
		{"alpha", "abc", "not an unsigned integer"},
		{"alpha after digits", "12abc", "not an unsigned integer"},
		{"trailing junk", "5x", "not an unsigned integer"},

		// Negative inputs are surfaced explicitly so operators do not
		// have to read the underlying ParseUint error.
		{"negative", "-1", "negative"},
		{"negative large", "-9223372036854775808", "negative"},

		// Decimal inputs are surfaced explicitly so operators do not
		// have to read the underlying ParseUint error.
		{"decimal", "5.5", "non-integer"},
		{"many decimals", "0.0001", "non-integer"},

		// One above the documented maximum is rejected.
		{"max plus one", "10001", "exceeds maximum"},

		// Values much larger than MaxMaxDepth still get caught by the
		// upper-bound check. They never reach the strconv overflow
		// branch because uint64 fits them; the point of these cases is
		// that the legacy "silent clamp to anything-fits-in-uint"
		// behaviour can no longer return. (The DoS-by-config
		// scenario in #317 used a non-numeric or out-of-uint64
		// value; both are caught here.)
		{"huge but in uint64", "18446744073709551615", "exceeds maximum"},
		{"huge but in uint64 minus one", "18446744073709551614", "exceeds maximum"},
		{"huge but in uint64 minus two", "18446744073709551613", "exceeds maximum"},

		// A value that genuinely exceeds uint64 must also be rejected.
		// strconv.ParseUint of "999999...x17" fails with ErrRange and
		// surfaces under the "exceeds uint64 maximum" reason so the
		// legacy silent-fallback path cannot return.
		{"uint64 large 70 digits", "99999999999999999999999999999999999999999999999999999999999999999999999999", "exceeds"},
		{"uint64 large 100 digits", strings.Repeat("9", 100), "exceeds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseMaxDepth(tc.in)
			if err == nil {
				t.Fatalf("expected error for input %q, got nil (silent fallback to parent)", tc.in)
			}
			var ierr *ErrInvalidMaxDepth
			if !errors.As(err, &ierr) {
				t.Fatalf("error type = %T, want *ErrInvalidMaxDepth", err)
			}
			if !strings.Contains(err.Error(), tc.wantMsgContains) {
				t.Errorf("error message %q does not contain %q", err.Error(), tc.wantMsgContains)
			}
			if ierr.Input != tc.in {
				t.Errorf("ErrInvalidMaxDepth.Input = %q, want %q (operator must see what they typed)", ierr.Input, tc.in)
			}
		})
	}
}

func TestParseMaxDepthBoundaryValues(t *testing.T) {
	// The accepted-maximum boundary is the largest defensible value. Any
	// attempt to set MaxMaxDepth+1 must produce ErrInvalidMaxDepth so the
	// legacy "silent clamp to anything-fits-in-uint" path cannot return.
	{
		got, err := parseMaxDepth(strconvFormatUint(MaxMaxDepth))
		if err != nil {
			t.Fatalf("MaxMaxDepth must be accepted, got: %v", err)
		}
		if got != MaxMaxDepth {
			t.Errorf("MaxMaxDepth parse = %d, want %d", got, MaxMaxDepth)
		}
	}
	{
		_, err := parseMaxDepth(strconvFormatUint(MaxMaxDepth + 1))
		var ierr *ErrInvalidMaxDepth
		if err == nil || !errors.As(err, &ierr) {
			t.Errorf("MaxMaxDepth+1 must produce ErrInvalidMaxDepth, got: %v", err)
		}
	}
	// Sanity — make sure the docs and the test agree that all values from
	// 0 to MaxMaxDepth are accepted without error.
	for v := uint64(0); v <= MaxMaxDepth; v += 1 + MaxMaxDepth/32 {
		_, err := parseMaxDepth(strconvFormatUint(v))
		if err != nil {
			t.Errorf("value %d must be accepted, got: %v", v, err)
		}
	}
}

func TestParseMaxDepthPlusOneFitsInUint(t *testing.T) {
	// The legacy bug report centred on uint(MaxUint64-1)+1 wrapping to 0.
	// parseMaxDepth returns a uint64 bounded by MaxMaxDepth, so the
	// (depth+1) arithmetic in OverrideConfig can never wrap on any
	// platform whose uint is at least 64 bits wide. A previous version
	// of this test attempted to assert boundary+1 against math.MaxUint64,
	// but the comparison was tautological because MaxUint64 is itself
	// the largest uint64; the meaningful invariant we still pin here is
	// that MaxMaxDepth itself is well below any uint wrap boundary.
	if MaxMaxDepth >= math.MaxUint64-1 {
		t.Fatalf("MaxMaxDepth (%d) is within the uint64 wrap zone — bump the explicit overflow guard", MaxMaxDepth)
	}
}

// strconvFormatUint is a one-liner wrapper kept here so each test case
// reads as a literal — keeps the boundary / rejection table to single
// lines without dragging strconv.FormatUint boilerplate through the body.
func strconvFormatUint(v uint64) string { return strconv.FormatUint(v, 10) }

func TestOverrideConfigAcceptsValidMaxDepth(t *testing.T) {
	// A well-formed override must clamp the parent MaxDepth to depth+1,
	// matching the historical "token override can only tighten, never
	// widen" semantics.
	cases := []struct {
		name      string
		parent    uint
		tokenVal  string
		wantAfter uint
	}{
		{"parent unchanged when override wider", 5, "10", 5},       // 10+1=11 > 5, parent retained
		{"parent unchanged when override equal", 5, "4", 5},        // 4+1=5 == 5, ">" branch false
		{"parent clamped tighter to 2+1=3", 5, "2", 3},
		{"max accepted boundary clamps to 10001", 5, "10000", 5},  // 10000+1=10001 > 5, parent retained
		{"parent clamped to depth+1=2 for depth=1", 3, "1", 2},
		{"max-depth=0 reduces parent to depth+1=1", 4, "0", 1},     // historical "one more level" semantics
		{"with whitespace", 3, "  2  ", 3},                          // 2+1=3 == 3 keeps parent
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := EsiParserConfig{MaxDepth: tc.parent}
			got := cfg.OverrideConfig(esiIncludeToken{MaxDepth: tc.tokenVal})
			if got.MaxDepth != tc.wantAfter {
				t.Errorf("OverrideConfig(MaxDepth:%d) with token %q -> MaxDepth=%d, want %d",
					tc.parent, tc.tokenVal, got.MaxDepth, tc.wantAfter)
			}
		})
	}
}

func TestOverrideConfigInvalidMaxDepthPreservesParent(t *testing.T) {
	// The whole point of the rewrite: an invalid override must NOT silently
	// disable nested ESI processing under the include. The parent's
	// MaxDepth must survive untouched.
	cases := []struct {
		name     string
		tokenVal string
	}{
		{"alpha", "abc"},
		{"negative", "-1"},
		{"decimal", "5.5"},
		{"max plus one", "10001"},
		{"uint64 max", "18446744073709551615"},
		{"uint64 max minus one", "18446744073709551614"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := EsiParserConfig{MaxDepth: 5}
			got := cfg.OverrideConfig(esiIncludeToken{MaxDepth: tc.tokenVal, Src: "/fragment"})
			if got.MaxDepth != 5 {
				t.Errorf("invalid override %q: parent MaxDepth changed from 5 to %d (must preserve parent)",
					tc.tokenVal, got.MaxDepth)
			}
		})
	}
}

func TestOverrideConfigEmptyOrWhitespaceOnlyNoOverride(t *testing.T) {
	// Empty / whitespace-only attributes are documented as "no override";
	// the parent's MaxDepth must survive untouched. This mirrors the
	// legacy `strconv.Atoi` short-circuit on parse errors and is required
	// because parseMaxDepth normalises both inputs to (0, nil) — without
	// this guard they would silently clamp the parent down to depth+1=1.
	cases := []struct {
		name     string
		tokenVal string
	}{
		{"empty string", ""},
		{"single space", " "},
		{"multiple spaces", "   "},
		{"tabs and spaces", "\t  \t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := EsiParserConfig{MaxDepth: 5}
			got := cfg.OverrideConfig(esiIncludeToken{MaxDepth: tc.tokenVal, Src: "/fragment"})
			if got.MaxDepth != 5 {
				t.Errorf("empty/whitespace override %q: parent MaxDepth changed from 5 to %d (must preserve parent)",
					tc.tokenVal, got.MaxDepth)
			}
		})
	}
}

func TestOverrideConfigExplicitZeroKeepsLegacyClamp(t *testing.T) {
	// The legacy contract for an explicit `max-depth="0"` is to clamp the
	// parent's MaxDepth to (0+1)=1. The new code must preserve that
	// contract — only the empty / whitespace-only inputs are documented
	// as "no override".
	cfg := EsiParserConfig{MaxDepth: 4}
	got := cfg.OverrideConfig(esiIncludeToken{MaxDepth: "0", Src: "/fragment"})
	if got.MaxDepth != 1 {
		t.Errorf("max-depth=0: parent MaxDepth changed from 4 to %d, want 1 (legacy clamp semantics)", got.MaxDepth)
	}
}