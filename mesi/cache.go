package mesi

import (
	"context"
	"time"
)

type Cache interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

type CacheKeyFunc func(url string) string

func DefaultCacheKey(url string) string {
	return "mesi:" + url
}
