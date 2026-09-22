package config

import (
	"fmt"
	"strconv"
)

// MaxMaxConcurrentRequests is the documented upper bound for the
// per-parse concurrent-fetch limit passed through libgomesi's ParseJson
// entry point (#170). Neither the core (`mesi.EsiParserConfig.
// MaxConcurrentRequests` is a bare int documented only as "0 =
// unlimited"; negatives are surfaced through the logger and normalized
// to 0, mesi/parser.go — #329) nor Caddy (`max_concurrent_requests`
// runs through strconv.Atoi uncapped) defines a maximum, so the bound
// is derived from the transport: the value crosses into libgomesi as a
// JSON number and out to the Apache module as a C `int` (32-bit on
// every platform Apache 2.4 supports), whose shared strict parser
// (parse_nonneg_int) already guards at 9 digits. 999999999 is the
// largest value both sides can represent without a wrap, so Apache's
// config-load validation and this Go-side check can never disagree
// (same keep-in-sync pattern as MaxTimeoutSeconds /
// MaxMaxResponseSize, mirrored as MESI_MAX_MAX_CONCURRENT_REQUESTS in
// servers/apache/mod_mesi.c). The value bounds a `chan struct{}`
// admission semaphore (zero-size elements — no per-slot allocation),
// so the cap exists for portability, not memory.
const MaxMaxConcurrentRequests = 999999999

// InvalidMaxConcurrentRequestsError is the typed error for a malformed
// or out-of-range maxConcurrentRequests value. Input is the offending
// value rendered as text; Why explains the rejection. Callers must
// surface it (warn + NULL / config-load failure) and never substitute a
// default.
type InvalidMaxConcurrentRequestsError struct {
	Input string
	Why   string
}

func (e *InvalidMaxConcurrentRequestsError) Error() string {
	return fmt.Sprintf("invalid maxConcurrentRequests %q: %s", e.Input, e.Why)
}

// ValidateMaxConcurrentRequests rejects values outside
// [0, MaxMaxConcurrentRequests].
//
// 0 is a legitimate documented value: the core only installs the
// admission-control semaphore when MaxConcurrentRequests > 0
// (mesi/parser.go), so 0 means "unlimited" — the same contract Caddy's
// `max_concurrent_requests 0` and Apache's `MesiMaxConcurrentRequests
// 0` document. Negatives are rejected instead of passed through: the
// core warns ("max_concurrent_requests_invalid") and normalizes them
// to 0 (#329), i.e. a malformed explicit value would silently behave
// like the documented "unlimited" — libgomesi fails loud (warn + NULL)
// per the no-silent-default rule. The upper bound is
// MaxMaxConcurrentRequests — see the constant for the portability
// rationale.
func ValidateMaxConcurrentRequests(v int) error {
	if v < 0 {
		return &InvalidMaxConcurrentRequestsError{
			Input: strconv.Itoa(v),
			Why:   fmt.Sprintf("negative value %d", v),
		}
	}
	if v > MaxMaxConcurrentRequests {
		return &InvalidMaxConcurrentRequestsError{
			Input: strconv.Itoa(v),
			Why:   fmt.Sprintf("value %d exceeds maximum %d", v, MaxMaxConcurrentRequests),
		}
	}
	return nil
}
