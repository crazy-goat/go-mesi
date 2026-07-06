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
		size, err := normalizeCacheSize(p.config.CacheSize)
		if err != nil {
			return err
		}
		p.cache = mesi.NewMemoryCache(size, p.cacheTTL)
		return nil
	case "redis":
		size, err := normalizeCacheSize(p.config.CacheSize)
		if err != nil {
			return err
		}
		_ = size // cache_size is documented but unused for redis backend
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
