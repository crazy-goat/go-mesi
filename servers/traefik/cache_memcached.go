//go:build memcached

package traefik

import (
	"fmt"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/mesi/cache_memcached"
)

func initCache(p *ResponsePlugin) error {
	switch p.config.CacheBackend {
	case "":
		return nil
	case "memory":
		size := p.config.CacheSize
		if size <= 0 {
			size = 10000
		}
		p.cache = mesi.NewMemoryCache(size, p.cacheTTL)
		return nil
	case "memcached":
		if len(p.config.CacheMemcachedServers) == 0 {
			return fmt.Errorf("cacheMemcachedServers is required for memcached backend")
		}
		mc := memcache.New(p.config.CacheMemcachedServers...)
		p.cache = cache_memcached.NewMemcachedCache(mc, p.cacheTTL)
		return nil
	default:
		return fmt.Errorf("unknown cacheBackend: %s", p.config.CacheBackend)
	}
}
