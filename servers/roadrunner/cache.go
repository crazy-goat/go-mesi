//go:build !redis && !memcached

package roadrunner

import "fmt"

func initCache(p *Plugin) error {
	switch p.config.CacheBackend {
	case "":
		return nil
	case "memory":
		size, err := normalizeCacheSize(p.config.CacheSize)
		if err != nil {
			return err
		}
		p.cache = newMemoryCache(size, p.cacheTTL)
		return nil
	default:
		return fmt.Errorf("cache backend %q requires building with -tags redis or -tags memcached", p.config.CacheBackend)
	}
}
