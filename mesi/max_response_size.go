package mesi

import (
	"fmt"
	"math"
)

// MaxMaxResponseSize is the largest per-include response cap the core can
// honour, i.e. the upper bound of the range documented for
// EsiParserConfig.MaxResponseSize (the core also still accepts a negative cap,
// which means unlimited — see validateMaxResponseSize).
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
// "unlimited" meaning (the fetch path only limits when MaxResponseSize > 0).
//
// A negative cap keeps its historical behaviour of falling into that same
// unlimited branch, so the range the core actually accepts is
// [math.MinInt64, MaxMaxResponseSize] while [0, MaxMaxResponseSize] is the
// range documented for configuration values. Rejecting negatives here would
// change behaviour for existing Go callers, and it is not the same failure
// class as the wrapped bound — a negative delivers the full body, exactly like
// the documented `0`, instead of a silently wrong one — so the integration
// entry points keep rejecting them (libgomesi, Apache, nginx, the CLI, Traefik,
// RoadRunner) and the core decision is left to a separate, deliberate change.
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

// parseScopeKey marks a context that belongs to an in-flight MESIParse render.
// The render puts it on the context it derives, so the nested MESIParse calls
// made for include bodies can tell that they are inside a render the caller
// already reported on.
type parseScopeKey struct{}

// warnInvalidMaxResponseSize reports a cap that validateMaxResponseSize rejects
// as a single max_response_size_invalid warning.
//
// It is a no-op for an accepted cap. MESIParse calls it once per render, so a
// page with many includes reports one operator mistake once instead of once per
// include — the same convention #329 uses for a rejected configuration value.
// The fetch path itself (mesi/fetch.go) is the enforcement point and stays
// silent; every include of such a render still fails with the typed error.
func warnInvalidMaxResponseSize(config EsiParserConfig) {
	if err := validateMaxResponseSize(config.MaxResponseSize); err != nil {
		config.warn("max_response_size_invalid", "max_response_size", config.MaxResponseSize, "error", err.Error())
	}
}
