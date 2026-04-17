package mesi

import (
	"context"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

type MemcachedCache struct {
	client     *memcache.Client
	defaultTTL int32
}

func NewMemcachedCache(client *memcache.Client, defaultTTL time.Duration) *MemcachedCache {
	return &MemcachedCache{
		client:     client,
		defaultTTL: int32(defaultTTL.Seconds()),
	}
}

func (c *MemcachedCache) Get(ctx context.Context, key string) (string, bool, error) {
	item, err := c.client.Get(key)
	if err == memcache.ErrCacheMiss {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(item.Value), true, nil
}

func (c *MemcachedCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	expire := c.defaultTTL
	if ttl > 0 {
		expire = int32(ttl.Seconds())
	}
	return c.client.Set(&memcache.Item{
		Key:        key,
		Value:      []byte(value),
		Expiration: expire,
	})
}

func (c *MemcachedCache) Delete(ctx context.Context, key string) error {
	return c.client.Delete(key)
}
