package mesi

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MaxMaxDepth is the documented upper bound for an
// `<esi:include max-depth="N">` override. It is intentionally generous —
// well above any realistic ESI recursion depth — but small enough to keep
// the `+1` math inside every Go platform's natural int range and far below
// any value where wrapping to zero could happen.
//
// Rejecting values outside [0, MaxMaxDepth] surfaces operator mistakes
// (typos, units confusion, hostile templates) instead of silently clamping
// the override to a value large enough to defeat the parser's recursion
// guard, or — historically — wrapping back to zero and turning the
// include into a "parse-only" tag.
const MaxMaxDepth uint64 = 10_000

// ErrInvalidMaxDepth is the sentinel for any malformed `max-depth`
// attribute. Callers receive it wrapped with the offending value, so log
// output shows exactly what the operator (or upstream template) set.
//
// The parser surfaces this error through the existing include-error path
// (rendered as `IncludeErrorMarker`, skipped if `onerror="continue"`, or
// replaced with the tag body if provided) and also emits it through the
// configured logger so operators debugging rendered output can correlate
// rendered placeholders back to the offending attribute value.
type ErrInvalidMaxDepth struct {
	Input string
	Why   string
}

func (e *ErrInvalidMaxDepth) Error() string {
	return fmt.Sprintf("invalid max-depth %q: %s", e.Input, e.Why)
}

// parseMaxDepth turns the raw `max-depth` attribute into a validated depth.
//
// An empty attribute is treated as "no override" and returns (0, nil).
// Anything present-but-malformed returns an *ErrInvalidMaxDepth so the
// caller can surface it instead of silently substituting the parent
// config's MaxDepth (or, worse, the wrapped-to-zero legacy default).
//
// The accepted range is [0, MaxMaxDepth]. The check rejects, explicitly:
//   - non-integer / non-numeric input;
//   - negative values;
//   - decimal values;
//   - values above MaxMaxDepth (including the historical overflow boundary
//     around MaxUint64 reported in the issue that drove this rewrite).
func parseMaxDepth(raw string) (uint64, error) {
	const defaultValue uint64 = 0

	if raw == "" {
		return defaultValue, nil
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		// Whitespace-only attribute is the same as unset.
		return defaultValue, nil
	}

	if strings.HasPrefix(trimmed, "-") {
		return 0, &ErrInvalidMaxDepth{
			Input: raw,
			Why:   fmt.Sprintf("negative value %q", trimmed),
		}
	}
	if strings.Contains(trimmed, ".") {
		return 0, &ErrInvalidMaxDepth{
			Input: raw,
			Why:   fmt.Sprintf("non-integer value %q", trimmed),
		}
	}

	n, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		// At this point we know the input isn't empty / negative /
		// decimal; any ParseUint failure is either out of range for an
		// unsigned 64-bit integer (the very issue this rewrite fixes) or
		// some remaining invalid-character sequence. Either way we
		// surface a single explicit reason to the operator.
		if errIsOverflow(err) {
			return 0, &ErrInvalidMaxDepth{
				Input: raw,
				Why:   fmt.Sprintf("value exceeds uint64 maximum (%s)", err.Error()),
			}
		}
		return 0, &ErrInvalidMaxDepth{
			Input: raw,
			Why:   fmt.Sprintf("not an unsigned integer (got %q)", trimmed),
		}
	}

	if n > MaxMaxDepth {
		return 0, &ErrInvalidMaxDepth{
			Input: raw,
			Why:   fmt.Sprintf("value %d exceeds maximum %d", n, MaxMaxDepth),
		}
	}

	// n+1 fits inside math.MaxUint64 trivially (MaxMaxDepth is far below
	// math.MaxUint64), but keep the explicit overflow guard so future
	// maintainers who raise MaxMaxDepth past math.MaxUint64-1 still get
	// a defined behaviour instead of a wrap-to-zero surprise.
	one := uint64(1)
	if n > math.MaxUint64-one {
		return 0, &ErrInvalidMaxDepth{
			Input: raw,
			Why:   fmt.Sprintf("value %d overflows when adding 1", n),
		}
	}

	return n, nil
}

// errIsOverflow reports whether a strconv error is the "value out of range"
// variant — the only ParseUint error whose root cause is "the operator
// typed something larger than the widest integer we support", which is
// exactly the legacy silent-fallback path we're closing here.
func errIsOverflow(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(*strconv.NumError); ok {
		return ne.Err == strconv.ErrRange
	}
	return false
}
