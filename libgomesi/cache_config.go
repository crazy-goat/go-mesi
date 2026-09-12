package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/redis/go-redis/v9"

	"github.com/crazy-goat/go-mesi/mesi"
	mesi_cache_memcached "github.com/crazy-goat/go-mesi/mesi/cache_memcached"
	mesi_cache_redis "github.com/crazy-goat/go-mesi/mesi/cache_redis"
)

// redisConfig is the parsed form of the JSON config blob accepted by
// InitCacheWithConfig for the "redis" backend.
type redisConfig struct {
	RedisAddr     string `json:"redisAddr"`
	RedisPassword string `json:"redisPassword"`
	// RedisDB is the Redis database number. Pointer so the JSON
	// "unset" case (nil) is distinguishable from an explicit 0.
	// -1 is treated as "unset" → use Redis default 0.
	RedisDB *int `json:"redisDB"`
}

// memcachedConfig is the parsed form of the JSON config blob accepted
// by InitCacheWithConfig for the "memcached" backend.
type memcachedConfig struct {
	Servers []string `json:"servers"`
}

// parseRedisConfig decodes a JSON config string for the "redis" backend.
// An empty string or "{}" produces all Redis defaults (localhost:6379,
// no password, DB 0). Returns ErrInvalidConfig when JSON is malformed
// or required fields are missing.
func parseRedisConfig(cfgJSON string) (addr, password string, db int, err error) {
	addr = "localhost:6379"
	password = ""
	db = 0

	if cfgJSON == "" {
		return
	}
	var cfg redisConfig
	if jsonErr := json.Unmarshal([]byte(cfgJSON), &cfg); jsonErr != nil {
		err = fmt.Errorf("mesi: parseRedisConfig: %w", jsonErr)
		return
	}
	if cfg.RedisAddr != "" {
		addr = cfg.RedisAddr
	}
	if cfg.RedisPassword != "" {
		password = cfg.RedisPassword
	}
	if cfg.RedisDB != nil && *cfg.RedisDB >= 0 {
		db = *cfg.RedisDB
	}
	return
}

// parseMemcachedConfig decodes a JSON config string for the "memcached"
// backend. The "servers" field is required and must contain at least
// one host:port pair.
func parseMemcachedConfig(cfgJSON string) ([]string, error) {
	if cfgJSON == "" {
		return nil, fmt.Errorf("mesi: parseMemcachedConfig: empty config (servers required)")
	}
	var cfg memcachedConfig
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return nil, fmt.Errorf("mesi: parseMemcachedConfig: %w", err)
	}
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("mesi: parseMemcachedConfig: servers required")
	}
	return cfg.Servers, nil
}

// initCacheFromConfig builds the appropriate cache instance for backend
// using cfgJSON. Exposed as a helper so tests can call it directly
// without spawning cgo.
func initCacheFromConfig(backend string, size int, ttlSeconds int, cfgJSON string) (mesi.Cache, error) {
	goTTL := time.Duration(ttlSeconds) * time.Second

	switch backend {
	case "memory":
		if size <= 0 {
			size = 10000
		}
		return mesi.NewMemoryCache(size, goTTL), nil
	case "redis":
		addr, password, db, err := parseRedisConfig(cfgJSON)
		if err != nil {
			return nil, err
		}
		rdb := redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		})
		return mesi_cache_redis.NewRedisCache(rdb, goTTL), nil
	case "memcached":
		servers, err := parseMemcachedConfig(cfgJSON)
		if err != nil {
			return nil, err
		}
		mc := memcache.New(servers...)
		return mesi_cache_memcached.NewMemcachedCache(mc, goTTL), nil
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("mesi: unknown cache backend %q", backend)
	}
}
