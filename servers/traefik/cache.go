//go:build !redis

package traefik

import "fmt"

func initCache(p *ResponsePlugin) error {
	switch p.config.CacheBackend {
	case "":
		return nil
	case "memory":
		size := p.config.CacheSize
		if size <= 0 {
			size = 10000
		}
		p.cache = newMemoryCache(size, p.cacheTTL)
		return nil
	default:
		return fmt.Errorf("cache backend %q requires building with -tags redis", p.config.CacheBackend)
	}
}
