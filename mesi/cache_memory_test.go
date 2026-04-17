package mesi

import (
	"context"
	"testing"
	"time"
)

func TestMemoryCache_SetAndGet(t *testing.T) {
	cache := NewMemoryCache(100, time.Hour)
	ctx := context.Background()

	err := cache.Set(ctx, "key1", "value1", time.Hour)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, err := cache.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok || v != "value1" {
		t.Fatalf("expected value1, got %s, ok=%v", v, ok)
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	cache := NewMemoryCache(100, time.Hour)
	ctx := context.Background()

	_ = cache.Set(ctx, "key1", "value1", 0)
	_ = cache.Delete(ctx, "key1")

	_, ok, _ := cache.Get(ctx, "key1")
	if ok {
		t.Fatal("expected key1 to be deleted")
	}
}

func TestMemoryCache_LRUEviction(t *testing.T) {
	cache := NewMemoryCache(3, time.Hour)
	ctx := context.Background()

	_ = cache.Set(ctx, "k1", "v1", 0)
	_ = cache.Set(ctx, "k2", "v2", 0)
	_ = cache.Set(ctx, "k3", "v3", 0)
	_ = cache.Set(ctx, "k4", "v4", 0)

	_, ok, _ := cache.Get(ctx, "k1")
	if ok {
		t.Fatal("k1 should have been evicted")
	}

	_, ok, _ = cache.Get(ctx, "k4")
	if !ok {
		t.Fatal("k4 should exist")
	}
}

func TestMemoryCache_TTL(t *testing.T) {
	cache := NewMemoryCache(100, time.Hour)
	ctx := context.Background()

	_ = cache.Set(ctx, "key1", "value1", 50*time.Millisecond)

	_, ok, _ := cache.Get(ctx, "key1")
	if !ok {
		t.Fatal("key1 should exist immediately")
	}

	time.Sleep(100 * time.Millisecond)

	_, ok, _ = cache.Get(ctx, "key1")
	if ok {
		t.Fatal("key1 should have expired")
	}
}
