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

type Config struct {
	MaxDepth              int      `mapstructure:"max_depth"`
	SharedHTTPClient      bool     `mapstructure:"shared_http_client"`
	CacheBackend          string   `mapstructure:"cache_backend"`
	CacheSize             int      `mapstructure:"cache_size"`
	CacheTTL              string   `mapstructure:"cache_ttl"`
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

	if p.config.CacheBackend != "" && p.config.CacheTTL != "" {
		d, err := time.ParseDuration(p.config.CacheTTL)
		if err != nil {
			return fmt.Errorf("invalid cache_ttl %q: %w", p.config.CacheTTL, err)
		}
		p.cacheTTL = d
	}

	if err := initCache(p); err != nil {
		return err
	}

	return nil
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
