package traefik

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
)

const PluginName = "mesi"

func intPtr(v int) *int { return &v }

type Config struct {
	// MaxDepth limits ESI nesting. A nil pointer is "unset" (default 5).
	// Explicit 0 is passthrough (disable ESI), matching Caddy / Apache #166
	// and the README. Valid range is [0, mesi.MaxMaxDepth].
	MaxDepth                       *int     `json:"maxDepth" yaml:"maxDepth"`
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
}

func CreateConfig() *Config {
	return &Config{
		MaxDepth:        intPtr(5),
		BlockPrivateIPs: true,
	}
}

type ResponsePlugin struct {
	next            http.Handler
	name            string
	config          *Config
	cache           mesi.Cache
	cacheTTL        time.Duration
	sharedTransport *http.Transport
	closeFn         func() error
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

	p := &ResponsePlugin{
		next:   next,
		name:   name,
		config: config,
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
			Timeout:                        10 * time.Second,
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
