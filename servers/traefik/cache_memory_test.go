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

func TestNewMemoryCacheDefaultSize(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "30s"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestNewMemoryCacheCustomSize(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "120s"
	config.CacheSize = 500

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
	if plugin.cacheTTL != 120*time.Second {
		t.Errorf("Expected cacheTTL 120s, got %v", plugin.cacheTTL)
	}
}

func TestNewNoCacheBackend(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache != nil {
		t.Fatal("Expected nil cache when no backend specified")
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

func TestNewCacheBackendWithoutTTL(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memory"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache even without TTL")
	}
	if plugin.cacheTTL != 0 {
		t.Errorf("Expected cacheTTL 0 when not specified, got %v", plugin.cacheTTL)
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
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

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

func TestMemoryCachePassedToConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body>static</body></html>`))
	})

	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
	if plugin.cacheTTL != 60*time.Second {
		t.Errorf("Expected cacheTTL 60s, got %v", plugin.cacheTTL)
	}
}

func TestMemoryCacheDifferentSizes(t *testing.T) {
	sizes := []int{1, 100, 5000, 10000}
	for _, size := range sizes {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		config := CreateConfig()
		config.CacheBackend = "memory"
		config.CacheTTL = "60s"
		config.CacheSize = size

		p, err := New(context.Background(), handler, config, "test")
		if err != nil {
			t.Fatalf("Unexpected error for size %d: %v", size, err)
		}

		plugin := p.(*ResponsePlugin)
		if plugin.cache == nil {
			t.Fatalf("Expected non-nil cache for size %d", size)
		}
	}
}

func TestMemoryCacheTTLValues(t *testing.T) {
	tests := []struct {
		ttl      string
		expected time.Duration
	}{
		{"1s", 1 * time.Second},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h", 1 * time.Hour},
	}

	for _, tt := range tests {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		config := CreateConfig()
		config.CacheBackend = "memory"
		config.CacheTTL = tt.ttl

		p, err := New(context.Background(), handler, config, "test")
		if err != nil {
			t.Fatalf("Unexpected error for TTL %q: %v", tt.ttl, err)
		}

		plugin := p.(*ResponsePlugin)
		if plugin.cacheTTL != tt.expected {
			t.Errorf("For TTL %q: expected %v, got %v", tt.ttl, tt.expected, plugin.cacheTTL)
		}
	}
}
