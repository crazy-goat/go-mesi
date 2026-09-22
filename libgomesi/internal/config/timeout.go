package config

import (
	"fmt"
	"strconv"
	"time"
)

// MaxTimeoutSeconds is the documented upper bound for the global ESI
// parse timeout passed through libgomesi's ParseJson entry point
// (#167). Core `mesi` has no MaxTimeout-style constant (unlike
// mesi.MaxMaxDepth), so the bound is defined here from the underlying
// type: the value is integer seconds that feed a time.Duration
// (int64 nanoseconds) — anything from 24h up is a unit-confusion
// misconfiguration, and 86400 matches the MesiCacheTTL ceiling
// precedent (Apache MESI_MAX_CACHE_TTL_SECONDS). It also keeps
// `time.Duration(v) * time.Second` far below int64 overflow.
const MaxTimeoutSeconds = 86400

// DefaultTimeoutSeconds is the documented default used when no timeout
// is configured: 30s — the value libgomesi has always hardcoded for
// its C entry points (Parse / ParseWithConfig*), kept so an unset
// directive is byte-identical to previous behaviour.
const DefaultTimeoutSeconds = 30

// InvalidTimeoutError is the typed error for a malformed or
// out-of-range timeout value. Input is the offending value rendered as
// text; Why explains the rejection. Callers must surface it (warn +
// NULL / config-load failure) and never substitute a default.
type InvalidTimeoutError struct {
	Input string
	Why   string
}

func (e *InvalidTimeoutError) Error() string {
	return fmt.Sprintf("invalid timeout %q: %s", e.Input, e.Why)
}

// ValidateTimeout rejects values outside [1, MaxTimeoutSeconds].
//
// 0 is deliberately rejected: in the core, Timeout <= 0 makes EVERY
// include fetch fail immediately with ErrTimeBudgetExceeded
// (mesi/fetch.go) — it does not mean "unlimited" or "no timeout" —
// and Caddy likewise rejects a non-positive global `timeout`
// ("timeout must be positive"). Negatives must never reach the
// duration multiplication, and values above the ceiling would be a
// silent unit error.
func ValidateTimeout(v int) error {
	if v < 1 {
		return &InvalidTimeoutError{
			Input: strconv.Itoa(v),
			Why:   fmt.Sprintf("value %d is below minimum 1", v),
		}
	}
	if v > MaxTimeoutSeconds {
		return &InvalidTimeoutError{
			Input: strconv.Itoa(v),
			Why:   fmt.Sprintf("value %d exceeds maximum %d", v, MaxTimeoutSeconds),
		}
	}
	return nil
}

// ResolveTimeout returns the effective parse timeout for a config:
// the explicit value when set, otherwise DefaultTimeoutSeconds. The
// explicit value is validated — an out-of-range explicit value is an
// error, never silently replaced by the default.
func ResolveTimeout(explicit *int) (time.Duration, error) {
	v := DefaultTimeoutSeconds
	if explicit != nil {
		v = *explicit
	}
	if err := ValidateTimeout(v); err != nil {
		return 0, err
	}
	return time.Duration(v) * time.Second, nil
}
