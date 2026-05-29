//go:build redis

package roadrunner

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInitRedisCache(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"
	config.CacheRedisAddr = "localhost:6379"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestInitRedisCacheDefaultAddr(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestInitRedisCacheWithPassword(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"
	config.CacheRedisAddr = "localhost:6379"
	config.CacheRedisPassword = "secret"
	config.CacheRedisDB = 2

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestInitInvalidCacheBackend(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "invalid"

	p := &Plugin{config: config}
	if err := p.Init(); err == nil {
		t.Fatal("Expected error for invalid cache backend")
	}
}

func TestCloseRedis(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"
	config.CacheRedisAddr = "localhost:6379"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Unexpected error closing plugin: %v", err)
	}
}

func TestMiddlewareWithRedisCache(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><esi:include src=\"/fragment\" /></body></html>"))
	})

	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(handler)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestCacheTTL(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "120s"
	config.CacheRedisAddr = "localhost:6379"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if p.cacheTTL != 120*time.Second {
		t.Errorf("Expected cacheTTL 120s, got %v", p.cacheTTL)
	}
}
