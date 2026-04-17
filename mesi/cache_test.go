package mesi

import (
	"context"
	"testing"
	"time"
)

type mockCache struct {
	data map[string]string
}

func (m *mockCache) Get(ctx context.Context, key string) (string, bool, error) {
	v, ok := m.data[key]
	return v, ok, nil
}

func (m *mockCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	m.data[key] = value
	return nil
}

func (m *mockCache) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func TestCacheInterface(t *testing.T) {
	var c Cache = &mockCache{data: make(map[string]string)}

	ctx := context.Background()
	err := c.Set(ctx, "key1", "value1", 0)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	v, ok, err := c.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok || v != "value1" {
		t.Fatalf("expected value1, got %s, ok=%v", v, ok)
	}

	err = c.Delete(ctx, "key1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, ok, _ = c.Get(ctx, "key1")
	if ok {
		t.Fatal("expected key1 to be deleted")
	}
}

func TestDefaultCacheKey(t *testing.T) {
	key := DefaultCacheKey("http://example.com/test")
	expected := "mesi:http://example.com/test"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}
}
