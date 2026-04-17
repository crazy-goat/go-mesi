package mesi

import (
	"context"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

func TestMemcachedCache_SetAndGet(t *testing.T) {
	mc := newMemcachedClientForTest(t)
	if mc == nil {
		t.Skip("Memcached not available")
	}
	defer func() { _ = mc.Close() }()

	cache := NewMemcachedCache(mc, time.Hour)
	ctx := context.Background()

	err := cache.Set(ctx, "mc_key1", "mc_value1", time.Hour)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, err := cache.Get(ctx, "mc_key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok || v != "mc_value1" {
		t.Fatalf("expected mc_value1, got %s, ok=%v", v, ok)
	}

	_ = cache.Delete(ctx, "mc_key1")
	_, ok, _ = cache.Get(ctx, "mc_key1")
	if ok {
		t.Fatal("key should be deleted")
	}
}

func TestMemcachedCache_TTL(t *testing.T) {
	mc := newMemcachedClientForTest(t)
	if mc == nil {
		t.Skip("Memcached not available")
	}
	defer func() { _ = mc.Close() }()

	cache := NewMemcachedCache(mc, time.Hour)
	ctx := context.Background()

	err := cache.Set(ctx, "mc_ttl_key", "ttl_value", 2*time.Second)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, _ := cache.Get(ctx, "mc_ttl_key")
	if !ok || v != "ttl_value" {
		t.Fatalf("expected ttl_value, got %s, ok=%v", v, ok)
	}

	time.Sleep(2100 * time.Millisecond)

	_, ok, _ = cache.Get(ctx, "mc_ttl_key")
	if ok {
		t.Fatal("key should have expired")
	}
}

func newMemcachedClientForTest(t *testing.T) *memcache.Client {
	mc := memcache.New("localhost:11211")
	err := mc.Ping()
	if err != nil {
		return nil
	}
	return mc
}
