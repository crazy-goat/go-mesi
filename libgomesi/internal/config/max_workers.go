package config

import (
	"fmt"
	"strconv"
)

// MaxMaxWorkers is the documented upper bound for the per-parse
// token-processing worker-pool size passed through libgomesi's
// ParseJson entry point (#171). Neither the core
// (`mesi.EsiParserConfig.MaxWorkers` is a bare int documented only as
// "Zero means runtime.NumCPU()*4"; values <= 0 are silently replaced
// by that default in mesi/parser.go — no warning, unlike the
// MaxConcurrentRequests #329 normalization) nor Caddy (`max_workers`
// runs through an uncapped strconv.Atoi — the bare-Atoi gap family
// tracked in #452) defines a maximum, so the bound is derived from
// the transport: the value crosses into libgomesi as a JSON number
// and out to the Apache module as a C `int` (32-bit on every platform
// Apache 2.4 supports), whose shared strict parser (parse_nonneg_int)
// already guards at 9 digits. 999999999 is the largest value both
// sides can represent without a wrap, so Apache's config-load
// validation and this Go-side check can never disagree (same
// keep-in-sync pattern as MaxTimeoutSeconds / MaxMaxResponseSize /
// MaxMaxConcurrentRequests, mirrored as MESI_MAX_MAX_WORKERS in
// servers/apache/mod_mesi.c). The value only bounds a drain pool that
// is additionally clamped to the job count
// (`workerCount = min(maxWorkers, len(esiJobs))`, mesi/parser.go), so
// an over-large value is a no-op — the cap exists for portability,
// not semantics.
const MaxMaxWorkers = 999999999

// InvalidMaxWorkersError is the typed error for a malformed or
// out-of-range maxWorkers value. Input is the offending value rendered
// as text; Why explains the rejection. Callers must surface it (warn +
// NULL / config-load failure) and never substitute a default.
type InvalidMaxWorkersError struct {
	Input string
	Why   string
}

func (e *InvalidMaxWorkersError) Error() string {
	return fmt.Sprintf("invalid maxWorkers %q: %s", e.Input, e.Why)
}

// ValidateMaxWorkers rejects values outside [0, MaxMaxWorkers].
//
// 0 is a legitimate documented value: the core substitutes
// runtime.NumCPU()*4 whenever maxWorkers <= 0 (mesi/parser.go), so 0
// means "library default" — the same contract Caddy's `max_workers 0`
// and Apache's `MesiMaxWorkers 0` document. Negatives are rejected
// instead of passed through: for values <= 0 the core silently
// substitutes that default with NO warning (there is no
// #329-style warn+normalize here — the substitution is entirely
// quiet, mesi/parser.go), i.e. a malformed explicit value would
// silently behave like the documented "library default" — libgomesi
// fails loud (warn + NULL) per the no-silent-default rule. The upper
// bound is MaxMaxWorkers — see the constant for the portability
// rationale.
func ValidateMaxWorkers(v int) error {
	if v < 0 {
		return &InvalidMaxWorkersError{
			Input: strconv.Itoa(v),
			Why:   fmt.Sprintf("negative value %d", v),
		}
	}
	if v > MaxMaxWorkers {
		return &InvalidMaxWorkersError{
			Input: strconv.Itoa(v),
			Why:   fmt.Sprintf("value %d exceeds maximum %d", v, MaxMaxWorkers),
		}
	}
	return nil
}
