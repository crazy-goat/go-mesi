package cache_memcached

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
		defaultTTL: durationToSeconds(defaultTTL),
	}
}

func durationToSeconds(d time.Duration) int32 {
	s := d.Seconds()
	if s <= 0 {
		return 0
	}
	if s < 1 {
		return 1
	}
	return int32(s)
}

func (c *MemcachedCache) Get(ctx context.Context, key string) (string, bool, error) {
	type result struct {
		item *memcache.Item
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		item, err := c.client.Get(key)
		resultCh <- result{item: item, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", false, ctx.Err()
	case result := <-resultCh:
		if result.err == memcache.ErrCacheMiss {
			return "", false, nil
		}
		if result.err != nil {
			return "", false, result.err
		}
		return string(result.item.Value), true, nil
	}
}

func (c *MemcachedCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	expire := c.defaultTTL
	if ttl > 0 {
		expire = durationToSeconds(ttl)
	}
	item := &memcache.Item{
		Key:        key,
		Value:      []byte(value),
		Expiration: expire,
	}
	return c.runWithContext(ctx, func() error {
		return c.client.Set(item)
	})
}

func (c *MemcachedCache) Delete(ctx context.Context, key string) error {
	return c.runWithContext(ctx, func() error {
		return c.client.Delete(key)
	})
}

// runWithContext bounds how long a caller waits for gomemcache's synchronous
// operation. The dependency has no context-taking command methods; its own
// socket timeout eventually releases the worker after the caller's context
// expires. A write may still take effect after the caller receives the
// context error; the synchronous dependency cannot roll back an in-flight
// command. The buffered result channel lets that worker finish without
// blocking after the caller has returned.
func (c *MemcachedCache) runWithContext(ctx context.Context, operation func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- operation()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-resultCh:
		return err
	}
}
