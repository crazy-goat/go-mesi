package cache

import (
	"github.com/eko/gocache/lib/v4/cache"
	gocache_store "github.com/eko/gocache/store/go_cache/v4"
	goache "github.com/patrickmn/go-cache"
	"time"
)

var cacheManager *cache.Cache[[]byte] = nil

func CreateNullCache() *cache.Cache[[]byte] {
	return CreateGoCache(0 * time.Second)
}

func CreateGoCache(defaultTime time.Duration) *cache.Cache[[]byte] {
	goCacheClient := goache.New(defaultTime, 10*time.Minute)
	goCacheStore := gocache_store.NewGoCache(goCacheClient)
	return cache.New[[]byte](goCacheStore)
}
