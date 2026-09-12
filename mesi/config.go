package mesi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type EsiParserConfig struct {
	Context       context.Context
	DefaultUrl    string
	MaxDepth      uint
	Timeout       time.Duration
	ParseOnHeader bool
	// AllowedHosts restricts ESI includes to specified domains.
	// Empty list allows all hosts (subject to BlockPrivateIPs).
	//
	// Note: AllowedHosts does NOT bypass BlockPrivateIPs by default.
	// Use AllowPrivateIPsForAllowedHosts to enable private-IP bypass.
	AllowedHosts    []string
	BlockPrivateIPs bool
	// AllowPrivateIPsForAllowedHosts allows hosts in AllowedHosts to bypass
	// the BlockPrivateIPs check.
	//
	// SECURITY WARNING: This creates a potential SSRF vector if an attacker
	// can control DNS for a host in AllowedHosts. Only use in trusted environments.
	//
	// Default: false (private IPs always blocked regardless of AllowedHosts).
	AllowPrivateIPsForAllowedHosts bool
	MaxResponseSize                int64         // 0 = unlimited, default 10MB
	MaxConcurrentRequests          int           // 0 = unlimited (backward compatible)
	HTTPClient                     *http.Client  // shared client for connection pooling, nil = create per request
	Cache                          Cache         // nil = no caching (backward compatible)
	CacheTTL                       time.Duration // Default TTL for cached entries
	CacheKeyFunc                   CacheKeyFunc  // Custom cache key function (nil = DefaultCacheKey)
	Debug                          bool          // Enable debug logging
	Logger                         Logger        // Custom logger (nil = DiscardLogger when Debug is false)
	// IncludeErrorMarker is rendered in place of a failed include when no
	// onerror="continue" and no fallback <esi:include> body is present.
	// Default: "" (silent). Set to something like "<!-- esi error -->" for
	// debugging, but NEVER include the raw error — see security advisory.
	IncludeErrorMarker string
	// MaxWorkers caps the number of goroutines used to process ESI tokens
	// within a single MESIParse call. Zero means runtime.NumCPU()*4.
	// Static tokens do not count against this limit.
	MaxWorkers         int
	requestSemaphore   chan struct{} // semaphore for limiting HTTP requests

	// Variables holds ESI variable definitions from <esi:vars> blocks and
	// can be pre-populated by callers. Variables are resolved via $(NAME)
	// syntax in include URLs, text content, and test expressions.
	Variables map[string]string
	// RequestHeaders are available for $(HTTP_HEADER{Name}) resolution.
	RequestHeaders http.Header
	// RequestCookies are available for $(HTTP_COOKIE{name}) resolution.
	RequestCookies map[string]string
	// RequestQuery is available for $(QUERY_STRING{param}) resolution.
	RequestQuery map[string]string
}

func (c EsiParserConfig) getSemaphore() chan struct{} {
	return c.requestSemaphore
}

func (c EsiParserConfig) setSemaphore(s chan struct{}) EsiParserConfig {
	c.requestSemaphore = s
	return c
}

var discardLogger = DiscardLogger{}

func (c EsiParserConfig) getLogger() Logger {
	if c.Logger != nil {
		return c.Logger
	}
	if c.Debug {
		return DefaultLoggerNew()
	}
	return discardLogger
}

func (c EsiParserConfig) warn(msg string, keyvals ...interface{}) {
	logger := c.getLogger()
	if w, ok := logger.(LoggerWarn); ok {
		w.Warn(msg, keyvals...)
	} else {
		logger.Debug(msg, keyvals...)
	}
}

func (c EsiParserConfig) SetContext(ctx context.Context) EsiParserConfig {
	c.Context = ctx
	return c
}

func CreateDefaultConfig() EsiParserConfig {
	return EsiParserConfig{
		Context:         context.Background(),
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        5,
		Timeout:         10 * time.Second,
		ParseOnHeader:   false,
		BlockPrivateIPs: true,
		MaxResponseSize: 10 * 1024 * 1024, // 10MB default
		CacheKeyFunc:    DefaultCacheKey,
		Logger:          DiscardLogger{},
	}
}

func (c EsiParserConfig) CanGoDeeper(t time.Duration) bool {
	return c.MaxDepth >= 1 && c.Timeout > t
}

func (c EsiParserConfig) ParseOnly() bool {
	return c.MaxDepth < 1
}

func (c EsiParserConfig) DecreaseMaxDepth() EsiParserConfig {
	if c.MaxDepth > 0 {
		c.MaxDepth--
	}

	return c
}

func (c EsiParserConfig) WithElapsedTime(t time.Duration) EsiParserConfig {
	if c.Timeout-t > 0 {
		c.Timeout = c.Timeout - t
	} else {
		c.Timeout = 0
	}

	return c
}

// OverrideConfig layers per-`<esi:include>` overrides on top of the parent
// EsiParserConfig. Only fields actually present on the token are touched;
// every other field is preserved verbatim.
//
// The `max-depth` override is validated through parseMaxDepth so operator
// typos (non-numeric, negative, decimal, oversized values — including the
// historical MaxUint64-1 wrap-to-zero boundary) are surfaced through the
// configured logger rather than silently substituted. A validated override
// then clamps the parent's MaxDepth to (depth+1), matching the documented
// "token override can only tighten, never widen" semantics. An invalid
// override is dropped: the parent's MaxDepth is preserved, which keeps a
// single misconfigured include from silently disabling all nested ESI
// processing downstream of it.
func (c EsiParserConfig) OverrideConfig(token esiIncludeToken) EsiParserConfig {
	if token.Timeout != "" {
		tokenTimeout, err := strconv.ParseFloat(token.Timeout, 64)
		if err == nil && tokenTimeout > 0 {
			timeoutLimit := time.Duration(tokenTimeout * float64(time.Second))
			if c.Timeout > timeoutLimit {
				c.Timeout = timeoutLimit
			}
		}
	}

	// The empty / whitespace-only case is documented as "no override":
	// the operator has not asked for any change, so the parent's
	// MaxDepth must survive untouched. Historically both `strconv.Atoi`
	// rejected these inputs and the legacy code's "skip if err != nil"
	// short-circuit produced the same behaviour — restore that contract
	// here before parseMaxDepth normalises whitespace into a "0" that
	// would otherwise clamp the parent down to depth+1=1.
	if token.MaxDepth == "" || strings.TrimSpace(token.MaxDepth) == "" {
		return c
	}
	depth, err := parseMaxDepth(token.MaxDepth)
	if err != nil {
		// Surface the invalid value through c.warn so a logger
		// implementing LoggerWarn receives it at warn severity;
		// loggers that do not (including the default
		// DiscardLogger) record it under Debug. The override
		// is dropped: we keep the parent's MaxDepth, which is
		// safer than silently substituting a parsed-but-
		// clamped value and is required to keep a malformed
		// attribute from silently disabling nested ESI
		// processing under this include.
		c.warn("max_depth_invalid", "src", token.Src, "max_depth", token.MaxDepth, "error", err.Error())
		return c
	}
	// For an accepted (non-empty, non-whitespace, parseable, in-
	// range) value, clamp the parent MaxDepth to (depth+1) —
	// matching the historical "token override can only tighten
	// the parent's depth, never widen it" semantics. This also
	// covers the documented "max-depth=0 → depth+1=1" signal
	// from the legacy contract.
	limit := uint(depth) + 1
	if c.MaxDepth > limit {
		c.MaxDepth = limit
	}

	return c
}
