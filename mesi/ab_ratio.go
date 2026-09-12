package mesi

import (
	"fmt"
	// NOTE: math/rand is used instead of math/rand/v2 for Traefik plugin
	// compatibility. Yaegi (Traefik's Go interpreter) does not support
	// packages introduced in Go 1.22+ like math/rand/v2.
	// See: https://github.com/traefik/yaegi/issues/1674
	"math"
	"math/rand"
	"strconv"
	"strings"
)

// MaxABRatio is the documented upper bound for either side of an
// `<esi:include ab-ratio="A:B">` value. It is intentionally generous — well
// above any realistic A/B-test traffic split — but small enough to make
// downstream arithmetic safe on every Go target platform (A+B <= 2*MaxABRatio
// fits comfortably in any int, including int32).
//
// Rejecting values outside [0, MaxABRatio] surfaces operator mistakes
// (typos, units confusion) instead of silently rebalancing to 50:50.
const MaxABRatio uint64 = 1_000_000

// ErrInvalidABRatio is the sentinel for any malformed `ab-ratio` attribute.
// Callers receive it wrapped with the offending value, so log output shows
// exactly what the operator set.
type ErrInvalidABRatio struct {
	Input string
	Why   string
}

func (e *ErrInvalidABRatio) Error() string {
	return fmt.Sprintf("invalid ab-ratio %q: %s", e.Input, e.Why)
}

// abRatio holds the parsed outcome of an `ab-ratio` attribute.
//
// Both fields are bounded by [0, MaxABRatio] and at least one of them is
// non-zero; operations downstream of parseAB rely on those invariants.
type abRatio struct {
	A uint64
	B uint64
}

// parseAB turns the raw `ab-ratio` attribute into a validated abRatio.
//
// An empty attribute is treated as "operator did not set a ratio" and
// returns the documented default of {A:50, B:50} with a nil error.
// Any present-but-malformed attribute returns an *ErrInvalidABRatio
// describing what went wrong; callers surface it as an include error so
// the operator actually sees it.
func (token *esiIncludeToken) parseAB() (abRatio, error) {
	const defaultA uint64 = 50
	const defaultB uint64 = 50

	if token.ABRatio == "" {
		return abRatio{A: defaultA, B: defaultB}, nil
	}

	raw := strings.TrimSpace(token.ABRatio)

	// An attribute that is set but contains only whitespace is treated the
	// same as an unset attribute: the operator hasn't supplied a ratio, so
	// we fall back to the documented default. Anything beyond whitespace
	// is treated as a deliberate attempt to set a ratio and must validate.
	if raw == "" {
		return abRatio{A: defaultA, B: defaultB}, nil
	}

	if !strings.Contains(raw, ":") {
		return abRatio{}, &ErrInvalidABRatio{
			Input: token.ABRatio,
			Why:   "missing ':' separator",
		}
	}

	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return abRatio{}, &ErrInvalidABRatio{
			Input: token.ABRatio,
			Why:   fmt.Sprintf("expected exactly 2 parts, got %d", len(parts)),
		}
	}

	a, err := parseABSide(parts[0])
	if err != nil {
		return abRatio{}, &ErrInvalidABRatio{
			Input: token.ABRatio,
			Why:   fmt.Sprintf("A side: %s", err.Error()),
		}
	}

	b, err := parseABSide(parts[1])
	if err != nil {
		return abRatio{}, &ErrInvalidABRatio{
			Input: token.ABRatio,
			Why:   fmt.Sprintf("B side: %s", err.Error()),
		}
	}

	if a == 0 && b == 0 {
		return abRatio{}, &ErrInvalidABRatio{
			Input: token.ABRatio,
			Why:   "both sides are zero (no traffic would ever reach B)",
		}
	}

	return abRatio{A: a, B: b}, nil
}

// parseABSide validates one half of the `A:B` ratio. Negative and decimal
// inputs are rejected explicitly (ParseUint on "-5" fails with a
// confusing "invalid syntax" message); we surface a clean error.
func parseABSide(side string) (uint64, error) {
	side = strings.TrimSpace(side)
	if side == "" {
		return 0, fmt.Errorf("empty value")
	}
	if strings.HasPrefix(side, "-") {
		return 0, fmt.Errorf("negative value %q", side)
	}
	if strings.Contains(side, ".") {
		return 0, fmt.Errorf("non-integer value %q", side)
	}
	n, err := strconv.ParseUint(side, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not an unsigned integer (got %q)", side)
	}
	if n > MaxABRatio {
		return 0, fmt.Errorf("value %d exceeds maximum %d", n, MaxABRatio)
	}
	return n, nil
}

func (ratio abRatio) selectUrl(token *esiIncludeToken, rng func(int) int) string {
	if token.Alt == "" {
		return token.Src
	}

	sum := ratio.A + ratio.B

	// sum is bounded by 2*MaxABRatio (2_000_000), well inside any int.
	// The overflow guard documents the invariant for future maintainers
	// who raise MaxABRatio past math.MaxInt32 / math.MaxInt64.
	if sum == 0 {
		return token.Src
	}
	if sum > math.MaxInt {
		return token.Alt
	}

	if rng == nil {
		rng = rand.Intn
	}

	randomValue := rng(int(sum))

	if randomValue < int(ratio.A) {
		return token.Src
	}
	return token.Alt
}

func fetchAB(token *esiIncludeToken, config EsiParserConfig) (string, bool, error) {
	logger := config.getLogger()
	ratio, err := token.parseAB()
	if err != nil {
		logger.Debug("ab_ratio_invalid", "src", token.Src, "ab_ratio", token.ABRatio, "error", err.Error())
		return "", false, err
	}
	selected := ratio.selectUrl(token, nil)
	logger.Debug("ab_ratio_select", "src", token.Src, "alt", token.Alt, "selected", selected)
	return singleFetchUrlWithContext(selected, config, config.Context)
}
