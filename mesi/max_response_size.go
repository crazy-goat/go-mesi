package mesi

import (
	"fmt"
	"math"
)

// MaxMaxResponseSize is the largest per-include response cap the core can
// honour, i.e. the upper bound of the accepted range for
// EsiParserConfig.MaxResponseSize.
//
// The cap is derived from the way the core consumes the value. To tell an
// at-the-limit body from an over-limit one, the fetch path reads
// MaxResponseSize+1 bytes through io.LimitReader and compares the result with
// MaxResponseSize (see singleFetchUrlWithContext). At math.MaxInt64 that +1
// wraps to math.MinInt64, and io.LimitedReader.Read returns (0, io.EOF)
// whenever its N <= 0 — so io.ReadAll returned an empty slice with a nil
// error, the over-limit check passed, and the include rendered an EMPTY body
// instead of failing. That silent-wrong outcome is why the only unacceptable
// value is rejected outright instead of being clamped (#448).
//
// Every documented entry point already caps at the same bound — libgomesi
// `config.MaxMaxResponseSize`, Apache `MESI_MAX_MAX_RESPONSE_SIZE`, and the
// nginx / CLI / Traefik / RoadRunner mirrors — so keeping it here makes the
// core the single place that defines the contract.
const MaxMaxResponseSize int64 = math.MaxInt64 - 1

// ErrInvalidMaxResponseSize is the typed error returned for a
// MaxResponseSize value outside the accepted range. Size is the offending
// value, Why explains the rejection; callers see both so log output shows
// exactly what the operator (or upstream template) set.
//
// Integrations must surface it — a Caddyfile directive, a JSON config key or a
// CLI flag cannot report a silent empty include. Go callers can match it with
// errors.As.
type ErrInvalidMaxResponseSize struct {
	Size int64
	Why  string
}

func (e *ErrInvalidMaxResponseSize) Error() string {
	return fmt.Sprintf("invalid MaxResponseSize %d: %s", e.Size, e.Why)
}

// validateMaxResponseSize rejects a response-size cap the fetch path cannot
// enforce, so the `MaxResponseSize+1` read bound never wraps negative.
//
// Only the unrepresentable upper boundary is rejected: math.MaxInt64 is the
// single int64 value above MaxMaxResponseSize. Zero keeps its documented
// "unlimited" meaning (the fetch path only limits when MaxResponseSize > 0),
// and a negative cap keeps its historical behaviour of falling into that same
// unlimited branch — the integration entry points reject negatives before they
// reach the core, so tightening that here is a separate, deliberate change.
func validateMaxResponseSize(size int64) error {
	if size <= MaxMaxResponseSize {
		return nil
	}
	return &ErrInvalidMaxResponseSize{
		Size: size,
		Why: fmt.Sprintf("value must be at most %d (the fetch path reads MaxResponseSize+1 bytes to detect an over-limit body, and that bound overflows at %d)",
			MaxMaxResponseSize, int64(math.MaxInt64)),
	}
}
