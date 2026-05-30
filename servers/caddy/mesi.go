package caddy

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/mesi/cache_memcached"
	"github.com/crazy-goat/go-mesi/mesi/cache_redis"
	"github.com/crazy-goat/go-mesi/middleware"
	"github.com/redis/go-redis/v9"
)

func init() {
	httpcaddyfile.RegisterHandlerDirective("mesi", parseCaddyfile)
	caddy.RegisterModule(new(MesiMiddleware))
}

// Compile-time interface assertions
var (
	_ caddy.Provisioner  = (*MesiMiddleware)(nil)
	_ caddy.CleanerUpper = (*MesiMiddleware)(nil)
)

type MesiMiddleware struct {
	// MaxDepth limits ESI nesting depth. Pointer distinguishes "unset" (nil → default 5)
	// from "explicitly set to 0" (passthrough, ESI disabled).
	MaxDepth *int `json:"max_depth,omitempty"`

	// SharedHTTPClient enables TCP connection reuse for ESI includes.
	// When true, a shared http.Transport with SSRF protection is created
	// in Provision() and reused for all requests. Without this, each
	// <esi:include> creates a fresh http.Client + http.Transport,
	// incurring N × TCP+TLS handshake overhead for multi-include pages.
	SharedHTTPClient bool `json:"shared_http_client,omitempty"`

	// CacheBackend selects the cache backend: "" (off), "memory", "redis", "memcached".
	// Memory backend uses an in-process LRU cache with TTL support.
	// Redis and Memcached backends are shared across Caddy instances.
	CacheBackend string `json:"cache_backend,omitempty"`
	// CacheSize is the max number of entries for the memory cache.
	// Default: 10000 when CacheBackend is "memory".
	CacheSize int `json:"cache_size,omitempty"`
	// CacheTTL is the default TTL for cached entries, e.g. "60s".
	// Parsed by time.ParseDuration at Provision time.
	CacheTTL string `json:"cache_ttl,omitempty"`
	// CacheKeyTemplate is a Go template string for custom cache keys.
	// Placeholders: ${url}, ${header:Name}, ${cookie:Name}.
	// Example: "mesi:${url}:${header:Accept-Language}".
	// When empty, DefaultCacheKey (URL-only) is used.
	CacheKeyTemplate string `json:"cache_key_template,omitempty"`

	// Debug enables verbose ESI processing logs to stderr.
	Debug bool `json:"debug,omitempty"`

	// IncludeErrorMarker is rendered in place of a failed include when no
	// onerror="continue" and no fallback body is present. Default: "" (silent).
	// SECURITY: Never include raw errors or URLs in the marker.
	IncludeErrorMarker string `json:"include_error_marker,omitempty"`

	// CacheRedisAddr is the Redis server address (host:port).
	// Required when CacheBackend is "redis". Default: "localhost:6379".
	CacheRedisAddr string `json:"cache_redis_addr,omitempty"`
	// CacheRedisPassword is the Redis AUTH password. Empty means no auth.
	CacheRedisPassword string `json:"cache_redis_password,omitempty"`
	// CacheRedisDB is the Redis database number. Default: 0.
	CacheRedisDB int `json:"cache_redis_db,omitempty"`

	// CacheMemcachedServers is the list of Memcached server addresses (host:port).
	// Required when CacheBackend is "memcached".
	CacheMemcachedServers []string `json:"cache_memcached_servers,omitempty"`

	// AllowedHosts restricts ESI includes to specified domains.
	// Empty list allows all hosts (subject to BlockPrivateIPs).
	// Host matching: exact match or subdomain suffix (sub.example.com matches example.com).
	AllowedHosts []string `json:"allowed_hosts,omitempty"`

	// MaxConcurrentRequests limits the number of concurrent HTTP requests
	// made during ESI processing. When set, a semaphore limits parallelism.
	// 0 = unlimited (backward compatible).
	MaxConcurrentRequests int `json:"max_concurrent_requests,omitempty"`

	// Timeout is the maximum time allowed for ESI processing, including
	// all remote fragment fetches. Parsed by time.ParseDuration at
	// Provision time. Default: "10s".
	Timeout string `json:"timeout,omitempty"`

	// BlockPrivateIPs controls SSRF protection. When true (default),
	// ESI includes to private/reserved IPs are blocked. Set to false
	// to allow internal ESI includes (e.g. service meshes, metadata services).
	BlockPrivateIPs *bool `json:"block_private_ips,omitempty"`

	// MaxResponseSize limits the size (in bytes) of an individual ESI include
	// response. Responses exceeding this limit are treated as errors and replaced
	// with IncludeErrorMarker (or silently dropped). Pointer distinguishes "unset"
	// (nil → library default 10 MB) from "explicitly set to 0" (unlimited).
	MaxResponseSize *int64 `json:"max_response_size,omitempty"`

	// MaxWorkers limits the number of goroutines used to process ESI tokens
	// within a single MESIParse call. Zero means runtime.NumCPU()*4 (library default).
	// This controls token-processing goroutines, not HTTP fetch goroutines
	// (see max_concurrent_requests for that).
	MaxWorkers int `json:"max_workers,omitempty"`

	sharedTransport *http.Transport  `json:"-"`
	cache           mesi.Cache       `json:"-"`
	cacheTTL        time.Duration    `json:"-"`
	parsedTimeout   time.Duration    `json:"-"`
	redisClient     *redis.Client    `json:"-"`
	memcachedClient *memcache.Client `json:"-"`
}

func (m *MesiMiddleware) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mesi",
		New: func() caddy.Module { return new(MesiMiddleware) },
	}
}

// Provision implements caddy.Provisioner. Called once at config load.
func (m *MesiMiddleware) Provision(ctx caddy.Context) error {
	blockPrivateIPs := true
	if m.BlockPrivateIPs != nil {
		blockPrivateIPs = *m.BlockPrivateIPs
	}

	if m.SharedHTTPClient {
		m.sharedTransport = mesi.NewSSRFSafeTransport(mesi.EsiParserConfig{
			BlockPrivateIPs: blockPrivateIPs,
		})
	}

	// Parse TTL once — shared across cache backends.
	if m.CacheBackend != "" && m.CacheTTL != "" {
		d, err := time.ParseDuration(m.CacheTTL)
		if err != nil {
			return fmt.Errorf("invalid cache_ttl %q: %w", m.CacheTTL, err)
		}
		m.cacheTTL = d
	}

	// Parse timeout.
	m.parsedTimeout = 10 * time.Second
	if m.Timeout != "" {
		d, err := time.ParseDuration(m.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", m.Timeout, err)
		}
		if d <= 0 {
			return fmt.Errorf("timeout must be positive, got %q", m.Timeout)
		}
		m.parsedTimeout = d
	}

	switch m.CacheBackend {
	case "":
		// no cache
	case "memory":
		size := m.CacheSize
		if size <= 0 {
			size = 10000
		}
		m.cache = mesi.NewMemoryCache(size, m.cacheTTL)
	case "redis":
		addr := m.CacheRedisAddr
		if addr == "" {
			addr = "localhost:6379"
		}
		rdb := redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: m.CacheRedisPassword,
			DB:       m.CacheRedisDB,
		})
		m.redisClient = rdb
		m.cache = cache_redis.NewRedisCache(rdb, m.cacheTTL)
	case "memcached":
		if len(m.CacheMemcachedServers) == 0 {
			return fmt.Errorf("cache_memcached_servers is required for memcached backend")
		}
		mc := memcache.New(m.CacheMemcachedServers...)
		m.memcachedClient = mc
		m.cache = cache_memcached.NewMemcachedCache(mc, m.cacheTTL)
	default:
		return fmt.Errorf("unknown cache_backend: %s", m.CacheBackend)
	}

	return nil
}

// Cleanup implements caddy.CleanerUpper. Closes idle connections on the
// shared transport and Redis client during config reloads to prevent
// goroutine/resource leaks. Idempotent — safe to call multiple times.
func (m *MesiMiddleware) Cleanup() error {
	if m.sharedTransport != nil {
		m.sharedTransport.CloseIdleConnections()
	}
	if m.redisClient != nil {
		_ = m.redisClient.Close()
		m.redisClient = nil
	}
	if m.memcachedClient != nil {
		m.memcachedClient = nil
	}
	return nil
}

func (m *MesiMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	r.Header.Set("Surrogate-Capability", "ESI/1.0")

	customWriter := middleware.NewResponseWriter(w)

	err := next.ServeHTTP(customWriter, r)
	if err != nil {
		return err
	}

	contentType := customWriter.Header().Get("Content-Type")
	if strings.HasPrefix(contentType, "text/html") {
		depth := 5
		if m.MaxDepth != nil {
			depth = *m.MaxDepth
		}
		blockPrivateIPs := true
		if m.BlockPrivateIPs != nil {
			blockPrivateIPs = *m.BlockPrivateIPs
		}
		config := mesi.EsiParserConfig{
			Context:                r.Context(),
			MaxDepth:               uint(depth),
			DefaultUrl:             middleware.GetDefaultUrl(r),
			Timeout:                m.parsedTimeout,
			BlockPrivateIPs:        blockPrivateIPs,
			AllowedHosts:           m.AllowedHosts,
			IncludeErrorMarker:     m.IncludeErrorMarker,
			Debug:                  m.Debug,
			MaxConcurrentRequests:  m.MaxConcurrentRequests,
			MaxWorkers:             m.MaxWorkers,
		}

		if m.MaxResponseSize != nil {
			config.MaxResponseSize = *m.MaxResponseSize
		}

		if m.cache != nil {
			config.Cache = m.cache
			config.CacheTTL = m.cacheTTL
			if m.CacheKeyTemplate != "" {
				tmpl := m.CacheKeyTemplate
				config.CacheKeyFunc = func(url string) string {
					return mesi.BuildCacheKey(url, tmpl, r)
				}
			}
		}

		if m.sharedTransport != nil {
			config.HTTPClient = &http.Client{
				Transport: m.sharedTransport,
				Timeout:   config.Timeout,
			}
		}

		processedResponse := mesi.MESIParse(
			customWriter.Body().String(),
			config,
		)

		w.Header().Set("Content-Length", strconv.Itoa(len(processedResponse)))
		for k, v := range customWriter.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(customWriter.StatusCode())
		w.Write([]byte(processedResponse))
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(customWriter.Body().Len()))
		w.WriteHeader(customWriter.StatusCode())
		w.Write(customWriter.Body().Bytes())
	}

	return nil
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	mesi := new(MesiMiddleware)
	err := mesi.UnmarshalCaddyfile(h.Dispenser)
	if err != nil {
		return mesi, err
	}

	return mesi, err
}

func (m *MesiMiddleware) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "max_depth":
				if !d.NextArg() {
					return d.ArgErr()
				}
				v, err := strconv.Atoi(d.Val())
				if err != nil {
					return d.Errf("invalid max_depth %q: %v", d.Val(), err)
				}
				m.MaxDepth = &v
			case "timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.Timeout = d.Val()
			case "max_concurrent_requests":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				m.MaxConcurrentRequests, err = strconv.Atoi(d.Val())
				if err != nil {
					return d.Errf("invalid max_concurrent_requests %q: %v", d.Val(), err)
				}
		case "max_workers":
			if !d.NextArg() {
				return d.ArgErr()
			}
			var err error
			m.MaxWorkers, err = strconv.Atoi(d.Val())
			if err != nil {
				return d.Errf("invalid max_workers %q: %v", d.Val(), err)
			}
			case "max_response_size":
				if !d.NextArg() {
					return d.ArgErr()
				}
				v, err := strconv.ParseInt(d.Val(), 10, 64)
				if err != nil {
					return d.Errf("invalid max_response_size %q: %v", d.Val(), err)
				}
				m.MaxResponseSize = &v
		case "shared_http_client":
			m.SharedHTTPClient = true
		case "block_private_ips":
			if d.NextArg() {
				v, err := strconv.ParseBool(d.Val())
				if err != nil {
					return d.Errf("invalid block_private_ips %q: %v", d.Val(), err)
				}
				m.BlockPrivateIPs = &v
			} else {
				v := true
				m.BlockPrivateIPs = &v
			}
		case "allowed_hosts":
			m.AllowedHosts = d.RemainingArgs()
		case "debug":
			m.Debug = true
		case "cache_backend":
			if !d.NextArg() {
				return d.ArgErr()
			}
			m.CacheBackend = d.Val()
			case "cache_size":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				m.CacheSize, err = strconv.Atoi(d.Val())
				if err != nil {
					return d.Errf("invalid cache_size %q: %v", d.Val(), err)
				}
			case "cache_ttl":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.CacheTTL = d.Val()
			case "cache_key_template":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.CacheKeyTemplate = d.Val()
			case "include_error_marker":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.IncludeErrorMarker = d.Val()
			case "cache_redis_addr":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.CacheRedisAddr = d.Val()
			case "cache_redis_password":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.CacheRedisPassword = d.Val()
			case "cache_redis_db":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var err error
				m.CacheRedisDB, err = strconv.Atoi(d.Val())
				if err != nil {
					return d.Errf("invalid cache_redis_db %q: %v", d.Val(), err)
				}
			case "cache_memcached_servers":
				m.CacheMemcachedServers = d.RemainingArgs()
			default:
				return d.Errf("unrecognized directive: %s", d.Val())
			}
		}
	}
	return nil
}
