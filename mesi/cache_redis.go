package mesi

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client     *redis.Client
	defaultTTL time.Duration
}

func NewRedisCache(client *redis.Client, defaultTTL time.Duration) *RedisCache {
	return &RedisCache{
		client:     client,
		defaultTTL: defaultTTL,
	}
}

func (c *RedisCache) Get(ctx context.Context, key string) (string, bool, error) {
	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if ttl == 0 {
		ttl = c.defaultTTL
	}
	if ttl > 0 {
		return c.client.Set(ctx, key, value, ttl).Err()
	}
	return c.client.Set(ctx, key, value, 0).Err()
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}
