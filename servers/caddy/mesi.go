package caddy

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/crazy-goat/go-mesi/mesi"
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
	// SharedHTTPClient enables TCP connection reuse for ESI includes.
	// When true, a shared http.Transport with SSRF protection is created
	// in Provision() and reused for all requests. Without this, each
	// <esi:include> creates a fresh http.Client + http.Transport,
	// incurring N × TCP+TLS handshake overhead for multi-include pages.
	SharedHTTPClient bool `json:"shared_http_client,omitempty"`

	// CacheBackend selects the cache backend: "" (off), "memory".
	// Memory backend uses an in-process LRU cache with TTL support.
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

	// CacheRedisAddr is the Redis server address (host:port).
	// Required when CacheBackend is "redis". Default: "localhost:6379".
	CacheRedisAddr string `json:"cache_redis_addr,omitempty"`
	// CacheRedisPassword is the Redis AUTH password. Empty means no auth.
	CacheRedisPassword string `json:"cache_redis_password,omitempty"`
	// CacheRedisDB is the Redis database number. Default: 0.
	CacheRedisDB int `json:"cache_redis_db,omitempty"`

	sharedTransport *http.Transport `json:"-"`
	cache           mesi.Cache      `json:"-"`
	cacheTTL        time.Duration   `json:"-"`
	redisClient     *redis.Client   `json:"-"`
}

func (m *MesiMiddleware) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mesi",
		New: func() caddy.Module { return new(MesiMiddleware) },
	}
}

// Provision implements caddy.Provisioner. Called once at config load.
func (m *MesiMiddleware) Provision(ctx caddy.Context) error {
	if m.SharedHTTPClient {
		m.sharedTransport = mesi.NewSSRFSafeTransport(mesi.EsiParserConfig{
			BlockPrivateIPs: true,
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
		m.cache = mesi.NewRedisCache(rdb, m.cacheTTL)
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
		config := mesi.EsiParserConfig{
			Context:         r.Context(),
			MaxDepth:        5,
			DefaultUrl:      middleware.GetDefaultUrl(r),
			Timeout:         10 * time.Second,
			BlockPrivateIPs: true,
		}

		if m.cache != nil {
			config.Cache = m.cache
			config.CacheTTL = m.cacheTTL
			if m.CacheKeyTemplate != "" {
				tmpl := m.CacheKeyTemplate
				config.CacheKeyFunc = func(url string) string {
					result := strings.ReplaceAll(tmpl, "${url}", url)

					// ${header:Name} substitution
					// Supports canonical, lowercase, and uppercase forms.
					for key, vals := range r.Header {
						val := vals[0]
						result = strings.ReplaceAll(result, "${header:"+key+"}", val)
						result = strings.ReplaceAll(result, "${header:"+strings.ToLower(key)+"}", val)
						result = strings.ReplaceAll(result, "${header:"+strings.ToUpper(key)+"}", val)
					}

					// ${cookie:Name} substitution
					for _, c := range r.Cookies() {
						result = strings.ReplaceAll(result, "${cookie:"+c.Name+"}", c.Value)
						result = strings.ReplaceAll(result, "${cookie:"+strings.ToLower(c.Name)+"}", c.Value)
						result = strings.ReplaceAll(result, "${cookie:"+strings.ToUpper(c.Name)+"}", c.Value)
					}

					return result
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
			case "shared_http_client":
				m.SharedHTTPClient = true
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
			default:
				return d.Errf("unrecognized directive: %s", d.Val())
			}
		}
	}
	return nil
}
