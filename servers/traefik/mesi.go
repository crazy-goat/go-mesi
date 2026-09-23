package traefik

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
)

const PluginName = "mesi"

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

type Config struct {
	// MaxDepth limits ESI nesting. A nil pointer is "unset" (default 5).
	// Explicit 0 is passthrough (disable ESI), matching Caddy / Apache #166
	// and the README. Valid range is [0, mesi.MaxMaxDepth].
	MaxDepth *int `json:"maxDepth" yaml:"maxDepth"`
	// Timeout is the per-include fetch budget as a Go duration string
	// (e.g. "10s", "5m") — the same grammar as Caddy's `timeout` and
	// this plugin's cacheTTL. A nil pointer is "unset" (default 10s,
	// the value this plugin has used since its inception — 10s from
	// day one). A non-nil pointer
	// must parse as a duration in [1s, 24h] or New() rejects it — an
	// explicit value is never silently replaced by the default.
	Timeout                        *string  `json:"timeout" yaml:"timeout"`
	SharedHTTPClient               bool     `json:"sharedHTTPClient" yaml:"sharedHTTPClient"`
	IncludeErrorMarker             string   `json:"includeErrorMarker" yaml:"includeErrorMarker"`
	CacheBackend                   string   `json:"cacheBackend" yaml:"cacheBackend"`
	CacheTTL                       string   `json:"cacheTTL" yaml:"cacheTTL"`
	CacheSize                      int      `json:"cacheSize" yaml:"cacheSize"`
	CacheRedisAddr                 string   `json:"cacheRedisAddr" yaml:"cacheRedisAddr"`
	CacheRedisPassword             string   `json:"cacheRedisPassword" yaml:"cacheRedisPassword"`
	CacheRedisDB                   int      `json:"cacheRedisDb" yaml:"cacheRedisDb"`
	CacheMemcachedServers          []string `json:"cacheMemcachedServers" yaml:"cacheMemcachedServers"`
	CacheKeyTemplate               string   `json:"cacheKeyTemplate" yaml:"cacheKeyTemplate"`
	BlockPrivateIPs                bool     `json:"blockPrivateIPs" yaml:"blockPrivateIPs"`
	AllowedHosts                   []string `json:"allowedHosts" yaml:"allowedHosts"`
	AllowPrivateIPsForAllowedHosts bool     `json:"allowPrivateIPsForAllowedHosts" yaml:"allowPrivateIPsForAllowedHosts"`
	// MaxResponseSize caps the HTTP response body size in bytes of a
	// single <esi:include> fetch (per-SINGLE-include, not per page).
	// 0 — the zero value and the absent option alike — is "unlimited"
	// (the core only limits when MaxResponseSize > 0, mesi/fetch.go),
	// byte-identical to this plugin's pre-#210 behaviour: ServeHTTP's
	// EsiParserConfig literal never set the field. There is NO implicit
	// 10 MB default here — that value only exists in
	// mesi.CreateDefaultConfig(), which this Go-direct plugin never
	// calls. A negative or a value above [0, math.MaxInt64-1] fails
	// New() with an error naming the option — an explicit value is
	// never silently replaced by a default.
	MaxResponseSize int64 `json:"maxResponseSize" yaml:"maxResponseSize"`
	// MaxConcurrentRequests caps the number of concurrent
	// <esi:include> HTTP fetches within one page render — one
	// mesi.MESIParse call (the admission-control semaphore the core
	// installs only when the value is > 0, mesi/parser.go:78).
	// 0 — the zero value and the absent option alike — is
	// "unlimited", byte-identical to this plugin's pre-#215
	// behaviour: ServeHTTP's EsiParserConfig literal never set the
	// field. A negative or a value above [0, 999999999] fails New()
	// with an error naming the option — an explicit value is never
	// silently normalized the way the core would (it only warns
	// "max_concurrent_requests_invalid" and rewrites a negative to
	// 0 = unlimited, #329).
	MaxConcurrentRequests int `json:"maxConcurrentRequests" yaml:"maxConcurrentRequests"`
}

func CreateConfig() *Config {
	return &Config{
		MaxDepth:        intPtr(5),
		Timeout:         strPtr("10s"),
		BlockPrivateIPs: true,
	}
}

type ResponsePlugin struct {
	next                  http.Handler
	name                  string
	config                *Config
	cache                 mesi.Cache
	cacheTTL              time.Duration
	timeout               time.Duration
	maxResponseSize       int64
	maxConcurrentRequests int
	sharedTransport       *http.Transport
	closeFn               func() error
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if config.MaxDepth == nil {
		config.MaxDepth = intPtr(5)
	} else if *config.MaxDepth < 0 || uint64(*config.MaxDepth) > mesi.MaxMaxDepth {
		return nil, fmt.Errorf("maxDepth must be in [0, %d], got %d", mesi.MaxMaxDepth, *config.MaxDepth)
	}

	timeout, err := resolveTimeout(config.Timeout)
	if err != nil {
		return nil, err
	}

	maxResponseSize, err := resolveMaxResponseSize(config.MaxResponseSize)
	if err != nil {
		return nil, err
	}

	maxConcurrentRequests, err := resolveMaxConcurrentRequests(config.MaxConcurrentRequests)
	if err != nil {
		return nil, err
	}

	p := &ResponsePlugin{
		next:                  next,
		name:                  name,
		config:                config,
		timeout:               timeout,
		maxResponseSize:       maxResponseSize,
		maxConcurrentRequests: maxConcurrentRequests,
	}

	if config.SharedHTTPClient {
		p.sharedTransport = mesi.NewSSRFSafeTransport(mesi.EsiParserConfig{
			BlockPrivateIPs: config.BlockPrivateIPs,
		})
	}

	if config.CacheBackend != "" && config.CacheTTL != "" {
		d, err := time.ParseDuration(config.CacheTTL)
		if err != nil {
			return nil, fmt.Errorf("invalid cacheTTL %q: %w", config.CacheTTL, err)
		}
		p.cacheTTL = d
	}

	if err := initCache(p); err != nil {
		return nil, err
	}

	return p, nil
}

// resolveTimeout maps the `timeout` plugin option (see Config) onto the
// per-include fetch budget. Unset (nil) → 10s: the budget this plugin has
// always used (= mesi.CreateDefaultConfig()'s 10s and Caddy's default
// — the Go-direct platforms' default; libgomesi's C-entry-point 30s
// (config.DefaultTimeoutSeconds) never applies here because traefik calls
// the Go mesi package directly). An explicit value must parse as a Go
// duration and fall in [1s, 24h], mirroring libgomesi's
// config.ValidateTimeout range of [1, 86400] seconds
// (config.MaxTimeoutSeconds): 0 or negative is rejected because the core
// fails EVERY include with ErrTimeBudgetExceeded when Timeout <= 0
// (mesi/fetch.go) — it is not "unlimited" — and values above 24h are a
// unit-error misconfiguration. Malformed or out-of-range explicit values
// return an error that fails middleware creation — never a silent
// fallback to the default (project rule: no silent defaults in parsers;
// same fail-loud contract as the maxDepth range check above).
func resolveTimeout(v *string) (time.Duration, error) {
	if v == nil {
		return 10 * time.Second, nil
	}
	d, err := time.ParseDuration(*v)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: %w", *v, err)
	}
	if d < time.Second {
		return 0, fmt.Errorf("invalid timeout %q: value must be at least 1s", *v)
	}
	if d > 24*time.Hour {
		return 0, fmt.Errorf("invalid timeout %q: value must be at most 24h (86400s)", *v)
	}
	return d, nil
}

// maxMaxResponseSize is the upper bound for the `maxResponseSize`
// plugin option: math.MaxInt64 - 1, the largest value for which the
// core's `MaxResponseSize + 1` io.LimitReader bound stays positive —
// at math.MaxInt64 it wraps negative, LimitedReader reports EOF
// immediately, and the include would silently render an EMPTY body
// instead of failing (#448). Mirrors libgomesi's
// config.MaxMaxResponseSize, which lives in a separate module and is
// unimportable here — the same keep-in-sync pattern as the CLI's local
// maxMaxResponseSize (#186) and Apache's MESI_MAX_MAX_RESPONSE_SIZE.
const maxMaxResponseSize int64 = math.MaxInt64 - 1

// resolveMaxResponseSize maps the `maxResponseSize` plugin option (see
// Config) onto the per-include response body cap in bytes.
//
// Absent and explicit 0 both resolve to 0 — the core only limits when
// MaxResponseSize > 0 (mesi/fetch.go:288), so 0 (and therefore the
// absent option too) means "unlimited": byte-identical to the pre-#210
// behaviour, where ServeHTTP's EsiParserConfig literal simply left the
// field at its zero value. There is NO implicit 10 MB default on this
// path — the 10 * 1024 * 1024 of mesi.CreateDefaultConfig()
// (mesi/config.go:106) only reaches Go callers of that constructor,
// which this Go-direct plugin never is (the same premise correction as
// #169 / #201 / #208 for the libgomesi / Caddy / nginx paths).
//
// A positive value caps each single include; an over-limit include
// fails closed through the include-error path (empty
// IncludeErrorMarker / fallback body / onerror="continue") — never a
// truncated body. Negatives are rejected because the core's `> 0`
// check would silently treat them exactly like 0 (unlimited), i.e. a
// malformed explicit value would pass as a documented one. Values
// above math.MaxInt64-1 are rejected for the #448 wrap reason (see
// maxMaxResponseSize). Range therefore [0, math.MaxInt64-1] — the
// same cap as Apache MesiMaxResponseSize, nginx mesi_max_response_size
// (#208), the PHP extension's max_response_size (#201) and the CLI
// -max-response-size (#186); Caddy's uncapped strconv.ParseInt
// (servers/caddy/mesi.go:365-373) belongs to the #452 gap family and
// is deliberately not inherited (same reasoning as #187's timeout
// upper bound). A malformed or out-of-range EXPLICIT value fails
// middleware creation with an error naming the option — never a silent
// fallback to a default (project rule: no silent defaults in parsers;
// same fail-loud contract as the maxDepth range check and
// resolveTimeout above; the traefik plugin API has no ParseConfig, so
// New() is the failure site).
func resolveMaxResponseSize(v int64) (int64, error) {
	if v < 0 {
		return 0, fmt.Errorf("invalid maxResponseSize %d: value must not be negative (0 = unlimited, range [0, %d])", v, maxMaxResponseSize)
	}
	if v > maxMaxResponseSize {
		return 0, fmt.Errorf("invalid maxResponseSize %d: value must be at most %d (the core's MaxResponseSize+1 LimitReader bound wraps at MaxInt64)", v, maxMaxResponseSize)
	}
	return v, nil
}

// maxMaxConcurrentRequests is the upper bound for the
// `maxConcurrentRequests` plugin option: 999999999, the #170
// transport-derived cap shared by every landed implementation —
// libgomesi's config.MaxMaxConcurrentRequests
// (libgomesi/internal/config/max_concurrent_requests.go:26, a separate
// module unimportable here — keep-in-sync pattern), Apache's
// MESI_MAX_MAX_CONCURRENT_REQUESTS (servers/apache/mod_mesi.c:214),
// nginx's MESI_MAX_MAX_CONCURRENT_REQUESTS (#214,
// servers/nginx/ngx_http_mesi_module.c:76), the PHP extension's
// MESI_MAX_MAX_CONCURRENT_REQUESTS (php-ext/mesi.c:71) and the CLI's
// maxMaxConcurrentRequests (cli/mesi-cli.go:72). Neither the core (a
// bare int documented only as "0 = unlimited", mesi/config.go:33) nor
// Caddy (uncapped strconv.Atoi, servers/caddy/mesi.go:347-355 — the
// #452 gap family, deliberately not inherited, same reasoning as
// #187's timeout upper bound) defines a maximum: the bound is derived
// from the transport — the value crosses to Apache as a C `int`
// (32-bit on every platform Apache 2.4 supports) guarded at 9 digits
// by the shared strict parsers. The semaphore is a `chan struct{}` of
// zero-size elements, so the cap exists for portability, not memory.
const maxMaxConcurrentRequests = 999999999

// resolveMaxConcurrentRequests maps the `maxConcurrentRequests` plugin
// option (see Config) onto the per-parse admission cap.
//
// Absent and explicit 0 both resolve to 0 — the core only installs the
// admission-control semaphore when MaxConcurrentRequests > 0
// (mesi/parser.go:78), so 0 (and therefore the absent option too)
// means "unlimited": byte-identical to the pre-#215 behaviour, where
// ServeHTTP's EsiParserConfig literal simply left the field at its
// zero value (mesi.CreateDefaultConfig() never sets it either —
// mesi/config.go:98-110 — and this plugin never calls that
// constructor). Unlike `timeout` (#187), a zero cap really IS
// "unlimited" here: no core path fails on it.
//
// A positive value caps concurrent <esi:include> fetches within ONE
// MESIParse call; includes queued beyond the cap WAIT for a free slot
// (the admission wait shares the per-include timeout deadline,
// mesi/fetch.go:140-155) — they are never dropped. Negatives are
// rejected because the core would only warn
// ("max_concurrent_requests_invalid") and normalize them to 0 =
// unlimited (#329), i.e. a malformed explicit value would silently
// pass as the documented "unlimited" — New() fails loud instead
// (project rule: no silent defaults; the same config-load rejection
// as every landed sibling: Apache #170, php-ext #206, CLI #192,
// nginx #214). Values above maxMaxConcurrentRequests are rejected for
// the parity reasons documented on that constant. Range therefore
// [0, 999999999]. A malformed or out-of-range EXPLICIT value fails
// middleware creation with an error naming the option — never a
// silent fallback (the traefik plugin API has no ParseConfig, so
// New() is the failure site — same fail-loud contract as
// resolveTimeout and resolveMaxResponseSize above; decode-level
// failures like decimals/overflow/non-integers fail traefik's config
// decode before New(), which also fails middleware creation — the
// same empirical finding #210 documented for the int64 field).
func resolveMaxConcurrentRequests(v int) (int, error) {
	if v < 0 {
		return 0, fmt.Errorf("invalid maxConcurrentRequests %d: value must not be negative (0 = unlimited, range [0, %d])", v, maxMaxConcurrentRequests)
	}
	if v > maxMaxConcurrentRequests {
		return 0, fmt.Errorf("invalid maxConcurrentRequests %d: value must be at most %d", v, maxMaxConcurrentRequests)
	}
	return v, nil
}

func (p *ResponsePlugin) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	customWriter := middleware.NewResponseWriter(rw)

	_, ok := req.Header["Surrogate-Capability"]
	if ok == false {
		req.Header.Set("Surrogate-Capability", "ESI/1.0")
	}

	p.next.ServeHTTP(customWriter, req)

	contentType := customWriter.Header().Get("Content-Type")

	if strings.HasPrefix(contentType, "text/html") {
		config := mesi.EsiParserConfig{
			Context:                        req.Context(),
			MaxDepth:                       uint(p.maxDepth()),
			DefaultUrl:                     middleware.GetDefaultUrl(req),
			Timeout:                        p.timeout,
			MaxResponseSize:                p.maxResponseSize,
			MaxConcurrentRequests:          p.maxConcurrentRequests,
			BlockPrivateIPs:                p.config.BlockPrivateIPs,
			IncludeErrorMarker:             p.config.IncludeErrorMarker,
			AllowedHosts:                   p.config.AllowedHosts,
			AllowPrivateIPsForAllowedHosts: p.config.AllowPrivateIPsForAllowedHosts,
		}

		if p.cache != nil {
			config.Cache = p.cache
			config.CacheTTL = p.cacheTTL
			if p.config.CacheKeyTemplate != "" {
				tmpl := p.config.CacheKeyTemplate
				config.CacheKeyFunc = func(url string) string {
					return mesi.BuildCacheKey(url, tmpl, req)
				}
			}
		}

		if p.sharedTransport != nil {
			config.HTTPClient = &http.Client{
				Transport: p.sharedTransport,
				Timeout:   config.Timeout,
			}
		}

		processedResponse := mesi.MESIParse(
			customWriter.Body().String(),
			config,
		)
		rw.Header().Set("Content-Length", strconv.Itoa(len(processedResponse)))
		for k, v := range customWriter.Header() {
			rw.Header()[k] = v
		}
		rw.WriteHeader(customWriter.StatusCode())

		rw.Write([]byte(processedResponse))

		return
	}

	rw.Write(customWriter.Body().Bytes())
}

func (p *ResponsePlugin) maxDepth() int {
	if p.config == nil || p.config.MaxDepth == nil {
		return 5
	}
	return *p.config.MaxDepth
}

func (p *ResponsePlugin) Name() string {
	return PluginName
}

func (p *ResponsePlugin) Close() error {
	if p.sharedTransport != nil {
		p.sharedTransport.CloseIdleConnections()
	}
	if p.closeFn != nil {
		return p.closeFn()
	}
	return nil
}
