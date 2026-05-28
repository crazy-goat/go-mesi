package caddy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// TestIntegrationParseEmptyDirective ensures the basic mesi directive
// without any subdirectives loads and provisions correctly.
func TestIntegrationParseEmptyDirective(t *testing.T) {
	input := `mesi`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile(empty) returned error: %v", err)
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	// Cleanup should be safe on an unprovisioned middleware
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}

	if m.SharedHTTPClient {
		t.Error("SharedHTTPClient should be false for empty directive")
	}
}

// TestIntegrationParseSharedHTTPClientFull verifies the full flow:
// Caddyfile parsing → Provision → Cleanup with shared_http_client.
func TestIntegrationParseSharedHTTPClientFull(t *testing.T) {
	input := `mesi {
		shared_http_client
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if !m.SharedHTTPClient {
		t.Fatal("SharedHTTPClient should be true")
	}

	// Provision creates the shared transport
	err = m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.sharedTransport == nil {
		t.Fatal("sharedTransport should be non-nil after Provision with SharedHTTPClient=true")
	}

	// Cleanup closes idle connections — safe to call after Provision
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
	if m.sharedTransport == nil {
		t.Fatal("sharedTransport should still be non-nil after Cleanup")
	}
}

// TestIntegrationCleanupWithoutProvision ensures Cleanup is safe
// when Provision was never called.
func TestIntegrationCleanupWithoutProvision(t *testing.T) {
	m := &MesiMiddleware{SharedHTTPClient: true}
	// Cleanup before Provision — should be a no-op
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestIntegrationCleanupDoubleCall verifies Cleanup is idempotent.
func TestIntegrationCleanupDoubleCall(t *testing.T) {
	input := `mesi {
		shared_http_client
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	// First cleanup
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
	// Second cleanup — should be idempotent
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() (second call) returned error: %v", err)
	}
}

// --- Cache Backend Integration Tests ---

// TestIntegrationCacheBackendMemoryFull verifies the full flow:
// Caddyfile parsing → Provision → cache instantiation → Cleanup
// with cache_backend memory.
func TestIntegrationCacheBackendMemoryFull(t *testing.T) {
	input := `mesi {
		cache_backend memory
		cache_size 5000
		cache_ttl 60s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheBackend != "memory" {
		t.Errorf("expected CacheBackend='memory', got '%s'", m.CacheBackend)
	}
	if m.CacheSize != 5000 {
		t.Errorf("expected CacheSize=5000, got %d", m.CacheSize)
	}
	if m.CacheTTL != "60s" {
		t.Errorf("expected CacheTTL='60s', got '%s'", m.CacheTTL)
	}

	// Provision creates the cache
	err = m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil after Provision with cache_backend memory")
	}
	if m.cacheTTL != 60*time.Second {
		t.Errorf("expected cacheTTL=60s, got %v", m.cacheTTL)
	}

	// Cleanup — should still have cache (no close needed for memory cache)
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should still be non-nil after Cleanup")
	}
}

// TestIntegrationCacheBackendMemoryNoTTL verifies cache_backend memory
// works without cache_ttl.
func TestIntegrationCacheBackendMemoryNoTTL(t *testing.T) {
	input := `mesi {
		cache_backend memory
		cache_size 100
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}

	err = m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
	// TTL should be 0 (no expiry) when not specified
	if m.cacheTTL != 0 {
		t.Errorf("expected cacheTTL=0 when no cache_ttl specified, got %v", m.cacheTTL)
	}
}

// TestIntegrationCacheBackendMemoryOnlySize verifies cache_backend memory
// with only cache_size (no TTL).
func TestIntegrationCacheBackendMemoryOnlySize(t *testing.T) {
	input := `mesi {
		cache_backend memory
		cache_size 250
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheSize != 250 {
		t.Errorf("expected CacheSize=250, got %d", m.CacheSize)
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestIntegrationCacheBackendInvalidTTL verifies that an invalid cache_ttl
// causes Provision to return an error.
func TestIntegrationCacheBackendInvalidTTL(t *testing.T) {
	input := `mesi {
		cache_backend memory
		cache_ttl bad-value
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for invalid cache_ttl")
	}
}

// TestIntegrationCacheBackendUnknown verifies that an unknown cache_backend
// causes Provision to return an error.
func TestIntegrationCacheBackendUnknown(t *testing.T) {
	input := `mesi {
		cache_backend bogus
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for unknown cache_backend")
	}
}

// TestIntegrationCacheBackendIncomplete verifies partial configs are valid.
// cache_backend without cache_size/cache_ttl should use defaults.
func TestIntegrationCacheBackendIncomplete(t *testing.T) {
	input := `mesi {
		cache_backend memory
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheBackend != "memory" {
		t.Errorf("expected CacheBackend='memory', got '%s'", m.CacheBackend)
	}
	// cache_size should be 0 (default will be applied in Provision)
	if m.CacheSize != 0 {
		t.Errorf("expected CacheSize=0 (default), got %d", m.CacheSize)
	}
	// cache_ttl should be empty
	if m.CacheTTL != "" {
		t.Errorf("expected CacheTTL='' (default), got '%s'", m.CacheTTL)
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
}

// --- Cache Key Template Integration Tests ---

// TestIntegrationCacheKeyTemplateParseAndProvision verifies the full flow:
// Caddyfile parsing → Provision → template stored correctly.
func TestIntegrationCacheKeyTemplateParseAndProvision(t *testing.T) {
	input := `mesi {
		cache_backend memory
		cache_key_template "mesi:${url}:${header:Accept-Language}"
		cache_ttl 60s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheBackend != "memory" {
		t.Errorf("expected CacheBackend='memory', got '%s'", m.CacheBackend)
	}
	if m.CacheKeyTemplate != "mesi:${url}:${header:Accept-Language}" {
		t.Errorf("expected CacheKeyTemplate='mesi:${url}:${header:Accept-Language}', got '%s'", m.CacheKeyTemplate)
	}
	if m.CacheTTL != "60s" {
		t.Errorf("expected CacheTTL='60s', got '%s'", m.CacheTTL)
	}

	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil after Provision with cache_backend memory")
	}
	if m.cacheTTL != 60*time.Second {
		t.Errorf("expected cacheTTL=60s, got %v", m.cacheTTL)
	}

	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestIntegrationCacheKeyTemplateHeaderVariant verifies that different
// header values produce different cache entries via the template.
// Uses SharedHTTPClient with a non-SSRF-safe transport to allow test-server
// access, and includes real ESI fetches to verify cache key differentiation.
func TestIntegrationCacheKeyTemplateHeaderVariant(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:      "memory",
		CacheKeyTemplate:  "lang:${header:X-Cache-Variant}",
		CacheTTL:          "60s",
		SharedHTTPClient:  true,
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	// Replace SSRF-safe transport with a plain one for test-server access
	m.sharedTransport = &http.Transport{}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}

	// Test backend that counts calls per fragment
	fragmentCallCount := 0
	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fragmentCallCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment content"))
	}))
	defer fragmentServer.Close()

	url := fragmentServer.URL + "/frag"
	esiContent := `<html><body><esi:include src="` + url + `" /></body></html>`

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
		return nil
	})

	// Request 1: X-Cache-Variant = A
	req1 := httptest.NewRequest("GET", "http://example.com/page", nil)
	req1.Header.Set("X-Cache-Variant", "A")
	rec1 := httptest.NewRecorder()
	if err := m.ServeHTTP(rec1, req1, handler); err != nil {
		t.Fatalf("ServeHTTP(A) returned error: %v", err)
	}

	// Request 2: X-Cache-Variant = B (different cache key)
	req2 := httptest.NewRequest("GET", "http://example.com/page", nil)
	req2.Header.Set("X-Cache-Variant", "B")
	rec2 := httptest.NewRecorder()
	if err := m.ServeHTTP(rec2, req2, handler); err != nil {
		t.Fatalf("ServeHTTP(B) returned error: %v", err)
	}

	// Request 3: X-Cache-Variant = A again (should hit cache)
	req3 := httptest.NewRequest("GET", "http://example.com/page", nil)
	req3.Header.Set("X-Cache-Variant", "A")
	rec3 := httptest.NewRecorder()
	if err := m.ServeHTTP(rec3, req3, handler); err != nil {
		t.Fatalf("ServeHTTP(A #2) returned error: %v", err)
	}

	// Should have called fragment server exactly 2 times (A + B); 3rd was cached
	if fragmentCallCount != 2 {
		t.Errorf("expected 2 fragment server calls (A + B), got %d", fragmentCallCount)
	}
}

// TestIntegrationCacheKeyTemplateNoTemplate verifies that without
// cache_key_template, different headers share the same cache entry (URL-only key).
func TestIntegrationCacheKeyTemplateNoTemplate(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:     "memory",
		CacheTTL:         "60s",
		SharedHTTPClient: true,
		// No CacheKeyTemplate — uses DefaultCacheKey (URL-only)
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	// Replace SSRF-safe transport with a plain one for test-server access
	m.sharedTransport = &http.Transport{}

	fragmentCallCount := 0
	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fragmentCallCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment content"))
	}))
	defer fragmentServer.Close()

	url := fragmentServer.URL + "/frag"
	esiContent := `<html><body><esi:include src="` + url + `" /></body></html>`

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
		return nil
	})

	// Same URL, different headers — should hit cache after first call (URL-only key)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "http://example.com/page", nil)
		if i == 1 {
			req.Header.Set("X-Variant", "B") // Different header, same URL
		}
		rec := httptest.NewRecorder()
		if err := m.ServeHTTP(rec, req, handler); err != nil {
			t.Fatalf("ServeHTTP attempt %d returned error: %v", i, err)
		}
	}

	// Without template, cache key is URL-only → only 1 fragment server call
	if fragmentCallCount != 1 {
		t.Errorf("expected 1 fragment server call (URL-only default key), got %d", fragmentCallCount)
	}
}

// TestIntegrationCacheKeyTemplateCookieVariant verifies different cookies
// produce different cache entries via the template.
func TestIntegrationCacheKeyTemplateCookieVariant(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:      "memory",
		CacheKeyTemplate:  "abtest:${cookie:ab_test_group}",
		CacheTTL:          "60s",
		SharedHTTPClient:  true,
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	// Replace SSRF-safe transport with a plain one for test-server access
	m.sharedTransport = &http.Transport{}

	fragmentCallCount := 0
	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fragmentCallCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment content"))
	}))
	defer fragmentServer.Close()

	url := fragmentServer.URL + "/frag"
	esiContent := `<html><body><esi:include src="` + url + `" /></body></html>`

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
		return nil
	})

	// Request A: cookie ab_test_group=A
	reqA := httptest.NewRequest("GET", "http://example.com/page", nil)
	reqA.AddCookie(&http.Cookie{Name: "ab_test_group", Value: "A"})
	recA := httptest.NewRecorder()
	if err := m.ServeHTTP(recA, reqA, handler); err != nil {
		t.Fatalf("ServeHTTP(A) returned error: %v", err)
	}

	// Request B: cookie ab_test_group=B
	reqB := httptest.NewRequest("GET", "http://example.com/page", nil)
	reqB.AddCookie(&http.Cookie{Name: "ab_test_group", Value: "B"})
	recB := httptest.NewRecorder()
	if err := m.ServeHTTP(recB, reqB, handler); err != nil {
		t.Fatalf("ServeHTTP(B) returned error: %v", err)
	}

	// Third request: same as A — should hit cache
	reqA2 := httptest.NewRequest("GET", "http://example.com/page", nil)
	reqA2.AddCookie(&http.Cookie{Name: "ab_test_group", Value: "A"})
	recA2 := httptest.NewRecorder()
	if err := m.ServeHTTP(recA2, reqA2, handler); err != nil {
		t.Fatalf("ServeHTTP(A #2) returned error: %v", err)
	}

	if fragmentCallCount != 2 {
		t.Errorf("expected 2 fragment server calls (A + B), got %d", fragmentCallCount)
	}
}
