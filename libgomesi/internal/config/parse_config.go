package config

import (
	"encoding/json"
	"time"
)

// DefaultMaxDepth is the documented depth used when the JSON config
// omits maxDepth — 5, the same literal the positional Parse* entry
// points and CreateDefaultConfig use.
const DefaultMaxDepth = 5

// ParseConfig is the schema of the JSON config blob accepted by
// libgomesi's ParseJson entry point (#167). It is the additive,
// future-proof alternative to growing the positional
// ParseWithConfig*/ParseWithConfigCtx signatures: new options become
// new keys instead of new ABI parameters.
//
// Every field is optional. Absent keys resolve to documented
// defaults — maxDepth 5, no allowed-hosts restriction, SSRF
// blockPrivateIPs ON (secure default, matching the core/Caddy/PHP/
// RoadRunner defaults), no bypass, URL-only cache keys,
// timeoutSeconds 30 (libgomesi's historical hardcoded value),
// maxResponseSize 0 (unlimited — the value every positional Parse*
// entry point leaves in EsiParserConfig.MaxResponseSize, NOT the
// 10 MB of mesi.CreateDefaultConfig, which only applies to Go
// callers using that constructor), and maxConcurrentRequests 0
// (unlimited — likewise the value every positional Parse* entry
// point leaves in EsiParserConfig.MaxConcurrentRequests).
//
// Unknown keys are ignored (forward compatibility — a newer caller
// must be able to talk to an older libgomesi). Type mismatches and
// malformed JSON are errors: the caller must fail loud (warn + NULL),
// never coerce.
type ParseConfig struct {
	// MaxDepth is the global ESI nesting depth. Pointer so an explicit
	// 0 (valid passthrough) is distinguishable from an absent key
	// (default 5). Validated against [0, mesi.MaxMaxDepth].
	MaxDepth *int `json:"maxDepth"`
	// DefaultUrl is the base URL for relative <esi:include> paths.
	DefaultUrl string `json:"defaultUrl"`
	// AllowedHosts is the whitespace-separated include-host whitelist
	// ("" = no restriction).
	AllowedHosts string `json:"allowedHosts"`
	// BlockPrivateIPs enables dial-time SSRF blocking of
	// private/reserved addresses. Absent → true (secure default).
	BlockPrivateIPs *bool `json:"blockPrivateIPs"`
	// AllowPrivateIPsForAllowedHosts lets hosts in AllowedHosts
	// resolve to private/reserved addresses. Absent → false.
	AllowPrivateIPsForAllowedHosts bool `json:"allowPrivateIPsForAllowedHosts"`
	// CacheKeyTemplate enables template-based cache keys ("" = the
	// URL-only DefaultCacheKey).
	CacheKeyTemplate string `json:"cacheKeyTemplate"`
	// RequestCtx is the raw {"headers":{...},"cookies":[...]} request
	// context handed to mesi.BuildCacheKey; consumed verbatim by
	// buildRequestFromJSON. Absent → nil (dummy request).
	RequestCtx json.RawMessage `json:"requestCtx"`
	// TimeoutSeconds is the global per-include fetch budget in
	// seconds. Pointer so an explicit 0 (invalid — rejected) is
	// distinguishable from an absent key (default 30). Validated
	// against [1, MaxTimeoutSeconds].
	TimeoutSeconds *int `json:"timeoutSeconds"`
	// MaxResponseSize caps a single <esi:include> response body in
	// BYTES. Pointer so an explicit 0 — a legitimate documented
	// value meaning "unlimited", see mesi/fetch.go — is
	// distinguishable from an absent key (which also resolves to 0,
	// keeping the key's absence byte-identical to the positional
	// Parse* paths). Validated against [0, MaxMaxResponseSize];
	// negatives are rejected rather than silently behaving like 0.
	MaxResponseSize *int64 `json:"maxResponseSize"`
	// MaxConcurrentRequests caps the number of concurrent
	// <esi:include> HTTP fetches within one parse (the
	// admission-control semaphore mesi/parser.go installs when > 0).
	// Pointer so an explicit 0 — a legitimate documented value
	// meaning "unlimited", see the core's "0 = unlimited" contract —
	// is distinguishable from an absent key (both resolve to 0,
	// keeping the key's absence byte-identical to the positional
	// Parse* paths, which leave EsiParserConfig.MaxConcurrentRequests
	// at its zero value). Validated against
	// [0, MaxMaxConcurrentRequests]; negatives are rejected rather
	// than silently behaving like 0 (the core only warns and
	// normalizes them — #329 — libgomesi fails loud instead).
	MaxConcurrentRequests *int `json:"maxConcurrentRequests"`
}

// ParseConfigFromJSON decodes the ParseJson config blob. Malformed
// JSON and type mismatches (e.g. "timeoutSeconds":"10" or a fractional
// maxDepth) return an error so the caller can fail loud.
func ParseConfigFromJSON(data []byte) (ParseConfig, error) {
	var c ParseConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return ParseConfig{}, err
	}
	return c, nil
}

// ResolvedMaxDepth returns the validated nesting depth: the explicit
// value when set (0 = valid passthrough), otherwise DefaultMaxDepth.
// An out-of-range explicit value errors — it is never silently
// replaced by the default.
func (c ParseConfig) ResolvedMaxDepth() (uint, error) {
	v := DefaultMaxDepth
	if c.MaxDepth != nil {
		v = *c.MaxDepth
	}
	if err := ValidateMaxDepth(v); err != nil {
		return 0, err
	}
	return uint(v), nil
}

// ResolvedTimeout returns the validated parse timeout: the explicit
// value when set, otherwise DefaultTimeoutSeconds (30s). An
// out-of-range explicit value (including 0) errors — it is never
// silently replaced by the default.
func (c ParseConfig) ResolvedTimeout() (time.Duration, error) {
	return ResolveTimeout(c.TimeoutSeconds)
}

// ResolvedMaxResponseSize returns the validated per-include response
// body cap in bytes: the explicit value when set (0 = unlimited — the
// documented core contract), otherwise 0. Absent → 0 is byte-identical
// to every positional Parse* entry point, which leaves
// EsiParserConfig.MaxResponseSize at its zero value (unlimited).
// An out-of-range explicit value errors — it is never silently
// replaced by the default.
func (c ParseConfig) ResolvedMaxResponseSize() (int64, error) {
	if c.MaxResponseSize == nil {
		return 0, nil
	}
	if err := ValidateMaxResponseSize(*c.MaxResponseSize); err != nil {
		return 0, err
	}
	return *c.MaxResponseSize, nil
}

// ResolvedMaxConcurrentRequests returns the validated per-parse
// concurrent-fetch cap: the explicit value when set (0 = unlimited —
// the documented core contract), otherwise 0. Absent → 0 is
// byte-identical to every positional Parse* entry point, which leaves
// EsiParserConfig.MaxConcurrentRequests at its zero value
// (unlimited). An out-of-range explicit value (including negatives)
// errors — it is never silently replaced by the default.
func (c ParseConfig) ResolvedMaxConcurrentRequests() (int, error) {
	if c.MaxConcurrentRequests == nil {
		return 0, nil
	}
	if err := ValidateMaxConcurrentRequests(*c.MaxConcurrentRequests); err != nil {
		return 0, err
	}
	return *c.MaxConcurrentRequests, nil
}

// ResolvedBlockPrivateIPs returns the effective SSRF dial-time block:
// the explicit value when set, otherwise true (secure default).
func (c ParseConfig) ResolvedBlockPrivateIPs() bool {
	if c.BlockPrivateIPs != nil {
		return *c.BlockPrivateIPs
	}
	return true
}
