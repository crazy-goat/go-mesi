//go:build redis

package roadrunner

import (
	"fmt"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/mesi/cache_redis"
	"github.com/redis/go-redis/v9"
)

func initCache(p *Plugin) error {
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
	case "redis":
		addr := p.config.CacheRedisAddr
		if addr == "" {
			addr = "localhost:6379"
		}
		rdb := redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: p.config.CacheRedisPassword,
			DB:       p.config.CacheRedisDB,
		})
		p.cache = cache_redis.NewRedisCache(rdb, p.cacheTTL)
		p.closeFn = rdb.Close
		return nil
	default:
		return fmt.Errorf("unknown cache_backend: %s", p.config.CacheBackend)
	}
}
