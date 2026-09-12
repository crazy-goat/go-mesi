//go:build memcached

package roadrunner

import (
	"fmt"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/mesi/cache_memcached"
)

func initCache(p *Plugin) error {
	switch p.config.CacheBackend {
	case "":
		return nil
	case "memory":
		size, err := normalizeCacheSize(p.config.CacheSize)
		if err != nil {
			return err
		}
		p.cache = mesi.NewMemoryCache(size, p.cacheTTL)
		return nil
	case "memcached":
		if len(p.config.CacheMemcachedServers) == 0 {
			return fmt.Errorf("cache_memcached_servers is required for memcached backend")
		}
		mc := memcache.New(p.config.CacheMemcachedServers...)
		p.cache = cache_memcached.NewMemcachedCache(mc, p.cacheTTL)
		return nil
	default:
		return fmt.Errorf("unknown cache_backend: %s", p.config.CacheBackend)
	}
}
