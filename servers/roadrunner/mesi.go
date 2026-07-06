package roadrunner

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
)

const PluginName = "mesi"

// MaxCacheSize bounds RoadRunner's cache_size config. Matches
// servers/apache/mod_mesi.c MESI_MAX_CACHE_SIZE so all server
// integrations reject the same overflow class.
const MaxCacheSize = 1_000_000

// MaxCacheTTL bounds RoadRunner's cache_ttl config. Matches
// servers/apache/mod_mesi.c MESI_MAX_CACHE_TTL_SECONDS (24h).
const MaxCacheTTL = 24 * time.Hour

// DefaultCacheSize is applied when cache_size is unset (<=0). Mirrors
// servers/apache/mod_mesi.c MESI_DEFAULT_CACHE_SIZE and the existing
// nginx / CLI / libgomesi defaults.
const DefaultCacheSize = 10000

type Config struct {
	MaxDepth              int      `mapstructure:"max_depth"`
	SharedHTTPClient      bool     `mapstructure:"shared_http_client"`
	CacheBackend          string   `mapstructure:"cache_backend"`
	CacheSize             int      `mapstructure:"cache_size"`
	CacheTTL              string   `mapstructure:"cache_ttl"`
	CacheKeyTemplate      string   `mapstructure:"cache_key_template"`
	CacheRedisAddr        string   `mapstructure:"cache_redis_addr"`
	CacheRedisPassword    string   `mapstructure:"cache_redis_password"`
	CacheRedisDB          int      `mapstructure:"cache_redis_db"`
	CacheMemcachedServers []string `mapstructure:"cache_memcached_servers"`
	Timeout               string   `mapstructure:"timeout"`
	IncludeErrorMarker    string   `mapstructure:"include_error_marker"`
}

func CreateConfig() *Config {
	return &Config{
		MaxDepth: 5,
	}
}

type Plugin struct {
	config          *Config
	cache           mesi.Cache
	cacheTTL        time.Duration
	sharedTransport *http.Transport
	closeFn         func() error
}

func (p *Plugin) Init() error {
	if p.config == nil {
		p.config = CreateConfig()
	}

	if p.config.MaxDepth == 0 {
		p.config.MaxDepth = 5
	}

	if p.config.SharedHTTPClient {
		p.sharedTransport = mesi.NewSSRFSafeTransport(mesi.EsiParserConfig{
			BlockPrivateIPs: true,
		})
	}

	if p.config.CacheBackend != "" {
		ttl, err := parseCacheTTL(p.config.CacheTTL)
		if err != nil {
			return err
		}
		p.cacheTTL = ttl
	}

	if err := initCache(p); err != nil {
		return err
	}

	return nil
}

// parseCacheTTL converts the cache_ttl string to a Duration while
// rejecting values that would silently degrade cache behaviour:
//
//   - empty string is treated as "no TTL" (Duration 0) so callers can
//     configure a backend without expiry.
//   - any other value must be a non-negative Go duration and must not
//     exceed MaxCacheTTL (24h). Negative values would flow into
//     mesi.NewMemoryCache as defaultTTL and silently translate to
//     "no expiry" (cache_memory.Set treats <0 as 0); out-of-range
//     values point to operator typos and we fail loud instead of
//     accepting surprising cache lifetimes.
func parseCacheTTL(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid cache_ttl %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("invalid cache_ttl %q: must be non-negative", raw)
	}
	if d > MaxCacheTTL {
		return 0, fmt.Errorf("invalid cache_ttl %q: exceeds max %s", raw, MaxCacheTTL)
	}
	return d, nil
}

func (p *Plugin) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Surrogate-Capability", "ESI/1.0")

		customWriter := middleware.NewResponseWriter(w)

		next.ServeHTTP(customWriter, r)

		contentType := customWriter.Header().Get("Content-Type")
		if strings.HasPrefix(contentType, "text/html") {
			config := mesi.EsiParserConfig{
				Context:            r.Context(),
				MaxDepth:           uint(p.config.MaxDepth),
				DefaultUrl:         middleware.GetDefaultUrl(r),
				Timeout:            10 * time.Second,
				BlockPrivateIPs:    true,
				IncludeErrorMarker: p.config.IncludeErrorMarker,
			}

			if p.cache != nil {
				config.Cache = p.cache
				config.CacheTTL = p.cacheTTL
				if p.config.CacheKeyTemplate != "" {
					tmpl := p.config.CacheKeyTemplate
					config.CacheKeyFunc = func(url string) string {
						return mesi.BuildCacheKey(url, tmpl, r)
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
	})
}

func (p *Plugin) Name() string {
	return PluginName
}

func (p *Plugin) Close() error {
	if p.sharedTransport != nil {
		p.sharedTransport.CloseIdleConnections()
	}
	if p.closeFn != nil {
		return p.closeFn()
	}
	return nil
}
