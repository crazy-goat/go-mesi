//go:build redis

package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()
	if config.MaxDepth != 5 {
		t.Errorf("Expected MaxDepth 5, got %d", config.MaxDepth)
	}
}

func TestNewNilConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	_, err := New(context.Background(), handler, nil, "test")
	if err == nil {
		t.Fatal("Expected error for nil config")
	}
}

func TestNewDefaultConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("Expected non-nil plugin")
	}
}

func TestNewMemoryCache(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheSize = 100

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestNewRedisCache(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"
	config.CacheRedisAddr = "localhost:6379"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestNewRedisCacheDefaultAddr(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestNewRedisCacheWithPassword(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"
	config.CacheRedisAddr = "localhost:6379"
	config.CacheRedisPassword = "secret"
	config.CacheRedisDB = 2

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestNewInvalidCacheBackend(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "invalid"

	_, err := New(context.Background(), handler, config, "test")
	if err == nil {
		t.Fatal("Expected error for invalid cache backend")
	}
}

func TestNewInvalidCacheTTL(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "invalid"

	_, err := New(context.Background(), handler, config, "test")
	if err == nil {
		t.Fatal("Expected error for invalid cache TTL")
	}
}

func TestName(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.Name() != "mesi" {
		t.Errorf("Expected name 'mesi', got %s", plugin.Name())
	}
}

func TestClose(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"
	config.CacheRedisAddr = "localhost:6379"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	err = plugin.Close()
	if err != nil {
		t.Fatalf("Unexpected error closing plugin: %v", err)
	}
}

func TestServeHTTPWithCache(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><esi:include src=\"/fragment\" /></body></html>"))
	})

	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestServeHTTPWithoutCache(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><esi:include src=\"/fragment\" /></body></html>"))
	})

	config := CreateConfig()

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestServeHTTPNonHTML(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/api", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("Expected body {'status':'ok'}, got %s", rec.Body.String())
	}
}

func TestCacheIntegration(t *testing.T) {
	t.Skip("Skipping integration test: requires Redis")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
	})

	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "60s"
	config.CacheRedisAddr = "localhost:6379"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req1 := httptest.NewRequest("GET", "http://example.com/", nil)
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "http://example.com/", nil)
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)

	if rec1.Body.String() != rec2.Body.String() {
		t.Errorf("Expected same body for cached response")
	}
}

func TestRedisCacheWithConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
	})

	config := CreateConfig()
	config.CacheBackend = "redis"
	config.CacheTTL = "120s"
	config.CacheRedisAddr = "10.0.0.5:6379"
	config.CacheRedisPassword = "password"
	config.CacheRedisDB = 2

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cacheTTL != 120*time.Second {
		t.Errorf("Expected cacheTTL 120s, got %v", plugin.cacheTTL)
	}
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}
