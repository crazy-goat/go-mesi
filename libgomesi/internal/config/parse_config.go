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
// RoadRunner defaults), no bypass, URL-only cache keys, and
// timeoutSeconds 30 (libgomesi's historical hardcoded value).
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

// ResolvedBlockPrivateIPs returns the effective SSRF dial-time block:
// the explicit value when set, otherwise true (secure default).
func (c ParseConfig) ResolvedBlockPrivateIPs() bool {
	if c.BlockPrivateIPs != nil {
		return *c.BlockPrivateIPs
	}
	return true
}
