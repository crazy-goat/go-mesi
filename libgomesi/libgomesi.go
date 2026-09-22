package main

// #include <stdlib.h>
// #include <string.h>
import "C"
import (
	"context"
	"encoding/json"
	"net/http"
	"time"
	"unsafe"

	"github.com/crazy-goat/go-mesi/libgomesi/internal/config"
	"github.com/crazy-goat/go-mesi/mesi"
)

var (
	sharedTransport *http.Transport
	sharedClient    *http.Client
	sharedCache     mesi.Cache
	sharedCacheTTL  time.Duration
)

// InitHTTPClient creates a shared HTTP client with SSRF protection.
// Call once per worker process (e.g. in module init) before Parse.
// The blockPrivateIPs parameter controls dial-time private IP blocking.
// Subsequent Parse calls reuse this client for connection pooling.
//
//export InitHTTPClient
func InitHTTPClient(blockPrivateIPs C.int) {
	config := mesi.EsiParserConfig{
		BlockPrivateIPs: blockPrivateIPs != 0,
	}
	sharedTransport = mesi.NewSSRFSafeTransport(config)
	sharedClient = &http.Client{
		Transport: sharedTransport,
		Timeout:   30 * time.Second,
	}
}

// FreeHTTPClient closes idle connections on the shared HTTP client.
// Call in process exit handler to prevent resource leaks.
// Idempotent — safe to call multiple times.
//
//export FreeHTTPClient
func FreeHTTPClient() {
	if sharedTransport != nil {
		sharedTransport.CloseIdleConnections()
		sharedTransport = nil
	}
	sharedClient = nil
}

// InitCache initializes a shared cache for ESI parsing.
// Call once per worker process before Parse to enable caching.
// Supported backends: "memory"
// Returns 0 on success, -1 if backend is unknown or unsupported.
//
//export InitCache
func InitCache(backend *C.char, size C.int, ttlSeconds C.int) C.int {
	goBackend := C.GoString(backend)
	goSize := int(size)
	goTTL := time.Duration(ttlSeconds) * time.Second

	switch goBackend {
	case "memory":
		if goSize <= 0 {
			goSize = 10000
		}
		sharedCache = mesi.NewMemoryCache(goSize, goTTL)
		sharedCacheTTL = goTTL
		return 0
	case "":
		sharedCache = nil
		sharedCacheTTL = 0
		return 0
	default:
		return -1
	}
}

// InitCacheWithConfig initializes a shared cache for ESI parsing, with
// backend-specific configuration passed as a JSON-encoded string.
//
// Currently supported backends:
//   - "memory":  no extra config required; configJSON may be "" or "{}".
//   - "redis":   configJSON decodes to redisConfig struct
//     ({"redisAddr":"host:port","redisPassword":"…","redisDB":N}).
//     All fields are optional; defaults are localhost:6379,
//     no password, DB 0.
//   - "memcached": configJSON decodes to memcachedConfig struct
//     ({"servers":["host:port",…]}). servers is required.
//
// Returns 0 on success, -1 if backend is unknown or config is malformed.
// Use this in place of (or after) InitCache when you need redis/memcached
// configuration — InitCache only supports "memory".
//
//export InitCacheWithConfig
func InitCacheWithConfig(backend *C.char, size C.int, ttlSeconds C.int, configJSON *C.char) C.int {
	goBackend := C.GoString(backend)
	goConfigJSON := C.GoString(configJSON)
	goTTL := time.Duration(ttlSeconds) * time.Second
	// Detach from any previous cache so a failed init leaves no stale
	// cache pointer behind (matches InitCache semantics).
	sharedCache = nil
	sharedCacheTTL = 0
	cache, err := initCacheFromConfig(goBackend, int(size), int(ttlSeconds), goConfigJSON)
	if err != nil {
		return -1
	}
	sharedCache = cache
	// For the empty backend, cache == nil and sharedCacheTTL stays 0.
	// For non-empty backends, cache != nil; record the TTL so the
	// shared config picker doesn't fall back to "no TTL".
	if cache != nil {
		sharedCacheTTL = goTTL
	}
	return 0
}

// FreeCache frees the shared cache.
// Call in process exit handler to prevent resource leaks.
// Idempotent — safe to call multiple times.
//
//export FreeCache
func FreeCache() {
	sharedCache = nil
	sharedCacheTTL = 0
}

func applySharedConfig(config *mesi.EsiParserConfig) {
	if sharedClient != nil {
		client := *sharedClient
		client.Timeout = config.Timeout
		config.HTTPClient = &client
	}
	if sharedCache != nil {
		config.Cache = sharedCache
		config.CacheTTL = sharedCacheTTL
	}
}

// ParseDefault parses ESI tags using sensible defaults (maxDepth=5, defaultUrl=http://127.0.0.1/).
// Caller must free the returned string with FreeString.
//
//export ParseDefault
func ParseDefault(input *C.char) *C.char {
	goInput := C.GoString(input)
	config := mesi.EsiParserConfig{
		DefaultUrl: "http://127.0.0.1/",
		MaxDepth:   5,
		Timeout:    defaultParseTimeout(),
	}
	applySharedConfig(&config)
	result := mesi.MESIParse(goInput, config)
	return C.CString(result)
}

// resolveMaxDepth converts the C ABI maxDepth to a validated uint.
// Values outside [0, mesi.MaxMaxDepth] are rejected (0 is passthrough).
// On error the caller must return NULL to C — never uint() a negative.
func resolveMaxDepth(maxDepth C.int) (uint, error) {
	v := int(maxDepth)
	if err := config.ValidateMaxDepth(v); err != nil {
		return 0, err
	}
	return uint(v), nil
}

func rejectMaxDepth(err error) {
	mesi.DefaultLoggerNew().Warn("invalid_max_depth", "error", err.Error())
}

// Parse parses ESI tags with explicit configuration.
// Parameters:
//   - input: ESI markup string to parse
//   - maxDepth: maximum nesting depth for includes (recommended: 5).
//     Valid range is [0, mesi.MaxMaxDepth] (10000). Explicit 0 is
//     passthrough (no ESI fetch). Values outside that range return NULL.
//   - defaultUrl: base URL for relative include paths
//
// Returns parsed HTML with ESI tags replaced by their content, or NULL
// when maxDepth is out of range. Caller must free a non-NULL return
// with FreeString.
//
//export Parse
func Parse(input *C.char, maxDepth C.int, defaultUrl *C.char) *C.char {
	goInput := C.GoString(input)
	goMaxDepth, err := resolveMaxDepth(maxDepth)
	if err != nil {
		rejectMaxDepth(err)
		return nil
	}
	goDefaultUrl := C.GoString(defaultUrl)
	config := mesi.EsiParserConfig{
		DefaultUrl: goDefaultUrl,
		MaxDepth:   goMaxDepth,
		Timeout:    defaultParseTimeout(),
	}
	applySharedConfig(&config)
	result := mesi.MESIParse(goInput, config)
	return C.CString(result)
}

// ParseWithConfig parses ESI tags with full configuration.
// Parameters:
//   - input: ESI markup string to parse
//   - maxDepth: maximum nesting depth for includes (recommended: 5).
//     Same [0, mesi.MaxMaxDepth] contract as Parse; out of range returns NULL.
//   - defaultUrl: base URL for relative include paths
//   - allowedHosts: space-separated list of allowed hostnames (or empty for no restriction)
//   - blockPrivateIPs: set to 1 to block private/reserved IP addresses
//
// Returns parsed HTML with ESI tags replaced by their content, or NULL
// when maxDepth is out of range. Caller must free a non-NULL return
// with FreeString.
//
//export ParseWithConfig
func ParseWithConfig(input *C.char, maxDepth C.int, defaultUrl *C.char, allowedHosts *C.char, blockPrivateIPs C.int) *C.char {
	return parseWithConfig(input, maxDepth, defaultUrl, allowedHosts, blockPrivateIPs, 0)
}

// ParseWithConfigEx is an extended variant of ParseWithConfig that also
// accepts allowPrivateIPsForAllowedHosts. When set to 1, hosts present in
// allowedHosts are permitted to resolve to private/reserved IP addresses
// even when blockPrivateIPs is enabled (see EsiParserConfig.
// AllowPrivateIPsForAllowedHosts). This is the ABI-safe path for server
// integrations that need the bypass; ParseWithConfig keeps its original
// 5-argument signature so existing callers (nginx, php-ext) are unaffected.
//
// Bypass-flagged parses detach from the shared HTTP client (if one was
// initialized with InitHTTPClient): its transport has blockPrivateIPs baked
// in at startup, which would otherwise silently negate the per-host bypass
// (the core's fetchClientForURL consults HTTPClient before the bypass
// branch). Such parses therefore use per-request clients; the shared client
// itself is untouched and still serves non-bypass parses.
//
//export ParseWithConfigEx
func ParseWithConfigEx(input *C.char, maxDepth C.int, defaultUrl *C.char, allowedHosts *C.char, blockPrivateIPs C.int, allowPrivateIPsForAllowedHosts C.int) *C.char {
	return parseWithConfig(input, maxDepth, defaultUrl, allowedHosts, blockPrivateIPs, allowPrivateIPsForAllowedHosts)
}

// headerValue is a string or []string; when an array is provided the first
// element is used as the header value (documented choice). This matches the
// PHP extension's request_headers contract where a header may be a string or
// array-of-string.
type headerValue struct {
	Values []string
}

func (h *headerValue) UnmarshalJSON(data []byte) error {
	// Try string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		h.Values = []string{s}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		h.Values = arr
		return nil
	}
	// Unknown or empty: treat as empty.
	h.Values = nil
	return nil
}

type cookieEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type requestCtx struct {
	Headers map[string]headerValue `json:"headers"`
	Cookies []cookieEntry          `json:"cookies"`
}

func buildRequestFromJSON(jsonStr string) *http.Request {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://dummy/", nil)
	if req == nil {
		return nil
	}
	if jsonStr == "" {
		return req
	}
	var ctx requestCtx
	if err := json.Unmarshal([]byte(jsonStr), &ctx); err != nil {
		mesi.DefaultLoggerNew().Warn("request_ctx_json_malformed", "error", err.Error())
		return req
	}
	for k, hv := range ctx.Headers {
		if len(hv.Values) == 0 {
			continue
		}
		// Use first element as documented.
		v := hv.Values[0]
		req.Header.Set(k, v)
		// Also store additional values if present as separate header values
		// so BuildCacheKey's vals[0] still reflects the first.
		for _, extra := range hv.Values[1:] {
			req.Header.Add(k, extra)
		}
	}
	for _, c := range ctx.Cookies {
		if c.Name == "" {
			continue
		}
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	return req
}

// defaultParseTimeout is the per-include fetch budget used by every
// positional Parse* entry point: 30s, the value libgomesi has always
// hardcoded. Single source of truth (config.DefaultTimeoutSeconds) so
// the positional exports and ParseJson's absent-timeoutSeconds default
// can never drift apart (#167).
func defaultParseTimeout() time.Duration {
	return time.Duration(config.DefaultTimeoutSeconds) * time.Second
}

// parseWithConfigInternal is the common implementation for ParseWithConfigEx,
// ParseWithConfigCtx and ParseJson. When cacheKeyTemplate is non-empty it
// installs a CacheKeyFunc that delegates to mesi.BuildCacheKey with a request
// built from requestCtxJSON. When empty the behaviour is identical to
// ParseWithConfigEx. timeout is the global per-include fetch budget — every
// caller but ParseJson passes defaultParseTimeout() (the historical 30s), so
// the positional exports keep their exact pre-#167 behaviour.
func parseWithConfigInternal(input *C.char, maxDepth C.int, defaultUrl *C.char, allowedHosts *C.char, blockPrivateIPs C.int, allowPrivateIPsForAllowedHosts C.int, cacheKeyTemplate *C.char, requestCtxJSON *C.char, timeout time.Duration) *C.char {
	goInput := C.GoString(input)
	goMaxDepth, err := resolveMaxDepth(maxDepth)
	if err != nil {
		rejectMaxDepth(err)
		return nil
	}
	goDefaultUrl := C.GoString(defaultUrl)

	hostsStr := ""
	if allowedHosts != nil {
		hostsStr = C.GoString(allowedHosts)
	}
	hosts := config.AllowedHosts(hostsStr, mesi.DefaultLoggerNew())

	tmpl := ""
	if cacheKeyTemplate != nil {
		tmpl = C.GoString(cacheKeyTemplate)
	}
	ctxJSON := ""
	if requestCtxJSON != nil {
		ctxJSON = C.GoString(requestCtxJSON)
	}

	cfg := mesi.EsiParserConfig{
		DefaultUrl:                     goDefaultUrl,
		MaxDepth:                       goMaxDepth,
		Timeout:                        timeout,
		AllowedHosts:                   hosts,
		BlockPrivateIPs:                blockPrivateIPs != 0,
		AllowPrivateIPsForAllowedHosts: allowPrivateIPsForAllowedHosts != 0,
	}
	applySharedConfig(&cfg)
	if allowPrivateIPsForAllowedHosts != 0 {
		cfg.HTTPClient = nil
	}
	if tmpl != "" {
		req := buildRequestFromJSON(ctxJSON)
		if req == nil {
			req, _ = http.NewRequestWithContext(context.Background(), "GET", "http://dummy/", nil)
		}
		capturedTmpl := tmpl
		capturedReq := req
		cfg.CacheKeyFunc = func(url string) string {
			return mesi.BuildCacheKey(url, capturedTmpl, capturedReq)
		}
	}
	result := mesi.MESIParse(goInput, cfg)
	return C.CString(result)
}

func parseWithConfig(input *C.char, maxDepth C.int, defaultUrl *C.char, allowedHosts *C.char, blockPrivateIPs C.int, allowPrivateIPsForAllowedHosts C.int) *C.char {
	return parseWithConfigInternal(input, maxDepth, defaultUrl, allowedHosts, blockPrivateIPs, allowPrivateIPsForAllowedHosts, nil, nil, defaultParseTimeout())
}

// ParseWithConfigCtx extends ParseWithConfigEx with cache key templating.
// Exact C param order: (char* input, int maxDepth, char* defaultUrl, char* allowedHosts, int blockPrivateIPs, int allowPrivateIPsForAllowedHosts, char* cacheKeyTemplate, char* requestCtxJSON).
// Semantics: if cacheKeyTemplate is "" → behave EXACTLY like ParseWithConfigEx (do NOT set CacheKeyFunc). Otherwise decode requestCtxJSON as {"headers":{"Name":"value" or ["v1"]},"cookies":[{"name":"n","value":"v"}]} (case-insensitive, defensive, ignore unknown fields; malformed JSON is logged via mesi.DefaultLoggerNew() and treated as empty request). A synthetic GET http.Request is built from headers+cookies and CacheKeyFunc is set to func(url string) string { return mesi.BuildCacheKey(url, template, req) }.
// The template text is part of the produced key, so distinct templates produce distinct keys — no extra fingerprint needed.
//
//export ParseWithConfigCtx
func ParseWithConfigCtx(input *C.char, maxDepth C.int, defaultUrl *C.char, allowedHosts *C.char, blockPrivateIPs C.int, allowPrivateIPsForAllowedHosts C.int, cacheKeyTemplate *C.char, requestCtxJSON *C.char) *C.char {
	return parseWithConfigInternal(input, maxDepth, defaultUrl, allowedHosts, blockPrivateIPs, allowPrivateIPsForAllowedHosts, cacheKeyTemplate, requestCtxJSON, defaultParseTimeout())
}

// ParseJson parses ESI tags with a JSON-encoded configuration blob — the
// additive, future-proof alternative to growing the positional
// ParseWithConfig* signatures (#167). configJSON decodes to config.ParseConfig:
//
//	{"maxDepth":5,"defaultUrl":"http://…/","allowedHosts":"a b",
//	 "blockPrivateIPs":true,"allowPrivateIPsForAllowedHosts":false,
//	 "cacheKeyTemplate":"mesi:${url}","requestCtx":{…},"timeoutSeconds":30}
//
// Every key is optional; an absent key resolves to the same documented
// default the corresponding positional entry point uses (timeoutSeconds
// absent → 30s — libgomesi's historical hardcoded value, so ParseJson with
// an absent timeout is byte-identical to ParseWithConfigCtx). Unknown keys
// are ignored (forward compatibility). configJSON may be NULL or "" — both
// mean "{}" (pure defaults).
//
// Malformed JSON, a type mismatch (e.g. "timeoutSeconds":"10"), or an
// out-of-range value (maxDepth outside [0, mesi.MaxMaxDepth];
// timeoutSeconds outside [1, 86400] — 0 is rejected because the core fails
// every include with ErrTimeBudgetExceeded when Timeout <= 0, see
// mesi/fetch.go) logs a warning naming the offending input and returns
// NULL. A default is NEVER substituted for a malformed explicit value.
//
// Returns parsed HTML with ESI tags replaced by their content, or NULL on
// any config error. Caller must free a non-NULL return with FreeString.
//
//export ParseJson
func ParseJson(input *C.char, configJSON *C.char) *C.char {
	raw := ""
	if configJSON != nil {
		raw = C.GoString(configJSON)
	}
	if raw == "" {
		// NULL and "" both mean "no config" — same contract as
		// InitCacheWithConfig's configJSON (documented "" or "{}").
		raw = "{}"
	}
	cfg, err := config.ParseConfigFromJSON([]byte(raw))
	if err != nil {
		mesi.DefaultLoggerNew().Warn("parse_config_json_malformed", "error", err.Error())
		return nil
	}
	depth, err := cfg.ResolvedMaxDepth()
	if err != nil {
		rejectMaxDepth(err)
		return nil
	}
	timeout, err := cfg.ResolvedTimeout()
	if err != nil {
		mesi.DefaultLoggerNew().Warn("invalid_timeout", "error", err.Error())
		return nil
	}
	// Hand the Go-side strings to parseWithConfigInternal as C strings;
	// freed when this function returns (parseWithConfigInternal copies
	// everything it needs via C.GoString).
	cDefaultUrl := C.CString(cfg.DefaultUrl)
	defer C.free(unsafe.Pointer(cDefaultUrl))
	cAllowedHosts := C.CString(cfg.AllowedHosts)
	defer C.free(unsafe.Pointer(cAllowedHosts))
	cCacheKeyTemplate := C.CString(cfg.CacheKeyTemplate)
	defer C.free(unsafe.Pointer(cCacheKeyTemplate))
	cRequestCtx := C.CString(string(cfg.RequestCtx))
	defer C.free(unsafe.Pointer(cRequestCtx))
	return parseWithConfigInternal(input, C.int(depth), cDefaultUrl, cAllowedHosts,
		boolCInt(cfg.ResolvedBlockPrivateIPs()),
		boolCInt(cfg.AllowPrivateIPsForAllowedHosts),
		cCacheKeyTemplate, cRequestCtx, timeout)
}

// boolCInt maps a Go bool to the C ABI int convention used by the
// Parse* exports (1 = true, 0 = false).
func boolCInt(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

// FreeString frees memory allocated by Parse and ParseDefault.
// Call this for every string returned by the Parse functions.
//
//export FreeString
func FreeString(str *C.char) {
	C.free(unsafe.Pointer(str))
}

func main() {}
