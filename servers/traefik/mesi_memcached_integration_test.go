//go:build memcached

package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewMemcachedCache(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memcached"
	config.CacheTTL = "60s"
	config.CacheMemcachedServers = []string{"localhost:11211"}

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestNewMemcachedCacheMultipleServers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memcached"
	config.CacheTTL = "120s"
	config.CacheMemcachedServers = []string{"10.0.0.1:11211", "10.0.0.2:11211"}

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

func TestNewMemcachedCacheNoServers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memcached"
	config.CacheTTL = "60s"
	config.CacheMemcachedServers = []string{}

	_, err := New(context.Background(), handler, config, "test")
	if err == nil {
		t.Fatal("Expected error for memcached without servers")
	}
}

func TestNewMemcachedCacheNilServers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memcached"
	config.CacheTTL = "60s"

	_, err := New(context.Background(), handler, config, "test")
	if err == nil {
		t.Fatal("Expected error for memcached without servers")
	}
}

func TestMemcachedCacheWithConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memcached"
	config.CacheTTL = "120s"
	config.CacheMemcachedServers = []string{"10.0.0.5:11211", "10.0.0.6:11211"}

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

func TestCloseMemcached(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.CacheBackend = "memcached"
	config.CacheTTL = "60s"
	config.CacheMemcachedServers = []string{"localhost:11211"}

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

func TestServeHTTPMemcachedCache(t *testing.T) {
	// Smoke test: verifies plugin handles requests with memcached config.
	// The ESI include will fail (no real backend), but the plugin should
	// not crash and should return 200.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><esi:include src=\"/fragment\" /></body></html>"))
	})

	config := CreateConfig()
	config.CacheBackend = "memcached"
	config.CacheTTL = "60s"
	config.CacheMemcachedServers = []string{"localhost:11211"}

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
