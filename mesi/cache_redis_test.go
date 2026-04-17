package mesi

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisCache_SetAndGet(t *testing.T) {
	client := newRedisClientForTest(t)
	if client == nil {
		t.Skip("Redis not available")
	}
	defer client.Close()

	cache := NewRedisCache(client, time.Hour)
	ctx := context.Background()

	err := cache.Set(ctx, "redis_key1", "redis_value1", time.Hour)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, err := cache.Get(ctx, "redis_key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok || v != "redis_value1" {
		t.Fatalf("expected redis_value1, got %s, ok=%v", v, ok)
	}

	cache.Delete(ctx, "redis_key1")
	_, ok, _ = cache.Get(ctx, "redis_key1")
	if ok {
		t.Fatal("key should be deleted")
	}
}

func TestRedisCache_TTL(t *testing.T) {
	client := newRedisClientForTest(t)
	if client == nil {
		t.Skip("Redis not available")
	}
	defer client.Close()

	cache := NewRedisCache(client, time.Hour)
	ctx := context.Background()

	err := cache.Set(ctx, "redis_ttl_key", "ttl_value", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, _ := cache.Get(ctx, "redis_ttl_key")
	if !ok || v != "ttl_value" {
		t.Fatalf("expected ttl_value, got %s, ok=%v", v, ok)
	}

	time.Sleep(200 * time.Millisecond)

	_, ok, _ = cache.Get(ctx, "redis_ttl_key")
	if ok {
		t.Fatal("key should have expired")
	}
}

func newRedisClientForTest(t *testing.T) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil
	}
	return client
}
