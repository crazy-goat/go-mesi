package config

import (
	"fmt"
	"math"
	"strconv"
)

// MaxMaxResponseSize is the documented upper bound for the per-include
// response body cap passed through libgomesi's ParseJson entry point
// (#169). Neither the core (`mesi.EsiParserConfig.MaxResponseSize` is
// a bare int64) nor Caddy (`max_response_size` runs through
// strconv.ParseInt uncapped) defines a maximum, so the bound is
// derived from the underlying type and the way the core consumes it:
// the value is an int64 byte count that the core feeds into
// `io.LimitReader(body, MaxResponseSize+1)` (mesi/fetch.go). At
// math.MaxInt64 that expression wraps to a negative limit, the
// LimitedReader reports EOF immediately, and the include would
// silently render an EMPTY body instead of hitting the size check —
// so the cap is math.MaxInt64-1, the largest value for which the
// core's +1 bound stays positive. The Apache-side type (apr_off_t,
// #169) is int64 on every platform Apache 2.4 supports.
const MaxMaxResponseSize int64 = math.MaxInt64 - 1

// InvalidMaxResponseSizeError is the typed error for a malformed or
// out-of-range maxResponseSize value. Input is the offending value
// rendered as text; Why explains the rejection. Callers must surface
// it (warn + NULL / config-load failure) and never substitute a
// default.
type InvalidMaxResponseSizeError struct {
	Input string
	Why   string
}

func (e *InvalidMaxResponseSizeError) Error() string {
	return fmt.Sprintf("invalid maxResponseSize %q: %s", e.Input, e.Why)
}

// ValidateMaxResponseSize rejects values outside
// [0, MaxMaxResponseSize].
//
// 0 is a legitimate documented value: the core only limits the body
// when MaxResponseSize > 0 (mesi/fetch.go), so 0 means "unlimited" —
// the same contract Caddy's `max_response_size 0` and Apache's
// `MesiMaxResponseSize 0` document. Negatives are rejected instead:
// the core's `> 0` check would silently treat them exactly like 0
// (unlimited), i.e. a malformed explicit value would pass as a
// documented one. The upper bound is MaxMaxResponseSize — see the
// constant for the overflow rationale.
func ValidateMaxResponseSize(v int64) error {
	if v < 0 {
		return &InvalidMaxResponseSizeError{
			Input: strconv.FormatInt(v, 10),
			Why:   fmt.Sprintf("negative value %d", v),
		}
	}
	if v > MaxMaxResponseSize {
		return &InvalidMaxResponseSizeError{
			Input: strconv.FormatInt(v, 10),
			Why:   fmt.Sprintf("value %d exceeds maximum %d", v, MaxMaxResponseSize),
		}
	}
	return nil
}
