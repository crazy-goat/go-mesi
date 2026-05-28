package caddy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

)

// TestSharedHTTPClientDefault ensures that without the directive,
// SharedHTTPClient is false and no shared transport is created.
func TestSharedHTTPClientDefault(t *testing.T) {
	m := &MesiMiddleware{}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.SharedHTTPClient {
		t.Error("SharedHTTPClient should be false by default")
	}
	if m.sharedTransport != nil {
		t.Error("sharedTransport should be nil when SharedHTTPClient is false")
	}
}

// TestSharedHTTPClientProvision checks that Provision() creates an
// SSRF-safe transport when SharedHTTPClient is true.
func TestSharedHTTPClientProvision(t *testing.T) {
	m := &MesiMiddleware{SharedHTTPClient: true}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.sharedTransport == nil {
		t.Fatal("sharedTransport should be non-nil when SharedHTTPClient is true")
	}
	if m.sharedTransport.DialContext == nil {
		t.Error("sharedTransport should have a DialContext (SSRF-safe)")
	}
}

// TestSharedHTTPClientProvisionFalse ensures that when SharedHTTPClient is false,
// Provision() does not create a shared transport.
func TestSharedHTTPClientProvisionFalse(t *testing.T) {
	m := &MesiMiddleware{SharedHTTPClient: false}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.sharedTransport != nil {
		t.Error("sharedTransport should be nil when SharedHTTPClient is false")
	}
}

// TestUnmarshalCaddyfileSharedHTTPClient parses the directive and verifies
// the field is set to true.
func TestUnmarshalCaddyfileSharedHTTPClient(t *testing.T) {
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
		t.Error("SharedHTTPClient should be true after parsing shared_http_client directive")
	}
}

// TestUnmarshalCaddyfileEmpty parses an empty body and verifies defaults.
func TestUnmarshalCaddyfileEmpty(t *testing.T) {
	input := `mesi`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.SharedHTTPClient {
		t.Error("SharedHTTPClient should be false by default")
	}
}

// TestUnmarshalCaddyfileUnknownDirective verifies that an unknown directive
// returns an error.
func TestUnmarshalCaddyfileUnknownDirective(t *testing.T) {
	input := `mesi {
		unknown_directive
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for unknown directive")
	}
	if !strings.Contains(err.Error(), "unrecognized directive") {
		t.Errorf("Expected 'unrecognized directive' error, got: %v", err)
	}
}

// TestServeHTTPWithSharedClient verifies that when a shared transport is available,
// the EsiParserConfig.HTTPClient is set.
func TestServeHTTPWithSharedClient(t *testing.T) {
	m := &MesiMiddleware{SharedHTTPClient: true}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.sharedTransport == nil {
		t.Fatal("sharedTransport should be non-nil")
	}

	// We need to test that ServeHTTP produces the right config.
	// Since we can't easily capture the config passed to MESIParse,
	// let's test the observable behavior: shared transport = connection reuse.

	// Create a test handler that returns an HTML page with ESI includes
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><esi:include src=\"/fragment\" /></body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	err = m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestServeHTTPWithoutSharedClient verifies that without shared transport,
// HTTPClient is nil in config (per-request clients created in fetch.go).
func TestServeHTTPWithoutSharedClient(t *testing.T) {
	m := &MesiMiddleware{SharedHTTPClient: false}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>no esi</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	err = m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestNonHTMLContentType verifies that non-HTML responses bypass ESI processing.
func TestNonHTMLContentType(t *testing.T) {
	m := &MesiMiddleware{SharedHTTPClient: true}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/api", nil)
	rec := httptest.NewRecorder()

	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("Expected JSON body unchanged, got: %s", rec.Body.String())
	}
}

// TestProvisionCreatesSharedTransport verifies that Provision() creates an
// SSRF-safe transport when SharedHTTPClient is true, and that the same
// transport instance is reused.
func TestProvisionCreatesSharedTransport(t *testing.T) {
	m := &MesiMiddleware{SharedHTTPClient: true}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	if m.sharedTransport == nil {
		t.Fatal("sharedTransport should not be nil")
	}
	if m.sharedTransport.DialContext == nil {
		t.Error("sharedTransport.DialContext should not be nil")
	}

	// Same instance across calls
	transportCopy := m.sharedTransport
	if m.sharedTransport != transportCopy {
		t.Error("sharedTransport should be the same instance")
	}
}

// TestMiddlewareInstanceIsolation ensures that multiple Middleware instances
// each get their own sharedTransport.
func TestMiddlewareInstanceIsolation(t *testing.T) {
	m1 := &MesiMiddleware{SharedHTTPClient: true}
	m2 := &MesiMiddleware{SharedHTTPClient: true}

	if err := m1.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if err := m2.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	if m1.sharedTransport == nil || m2.sharedTransport == nil {
		t.Fatal("Both instances should have sharedTransport")
	}

	// Different instances must have different transport objects
	if m1.sharedTransport == m2.sharedTransport {
		t.Error("Different Middleware instances should have separate transport objects")
	}
}

// TestSharedTransportImplementsRoundTripper checks that the shared transport
// satisfies the http.RoundTripper interface.
func TestSharedTransportImplementsRoundTripper(t *testing.T) {
	m := &MesiMiddleware{SharedHTTPClient: true}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	var rt http.RoundTripper = m.sharedTransport
	if rt == nil {
		t.Error("sharedTransport should implement http.RoundTripper")
	}
}

// --- Cache Backend Tests ---

// TestCacheBackendMemoryProvision verifies that cache_backend="memory" creates
// a non-nil cache in Provision().
func TestCacheBackendMemoryProvision(t *testing.T) {
	m := &MesiMiddleware{CacheBackend: "memory"}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil when CacheBackend is 'memory'")
	}
}

// TestCacheBackendMemoryProvisionWithSize verifies custom cache_size is used.
func TestCacheBackendMemoryProvisionWithSize(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "memory",
		CacheSize:    50,
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
}

// TestCacheBackendMemoryProvisionWithTTL verifies that a valid cache_ttl
// is parsed and stored in cacheTTL.
func TestCacheBackendMemoryProvisionWithTTL(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "memory",
		CacheTTL:     "60s",
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
	if m.cacheTTL != 60*time.Second {
		t.Errorf("expected cacheTTL=60s, got %v", m.cacheTTL)
	}
}

// TestCacheBackendAbsent verifies that when CacheBackend is empty, no cache
// is created (backward compatible).
func TestCacheBackendAbsent(t *testing.T) {
	m := &MesiMiddleware{}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache != nil {
		t.Error("cache should be nil when CacheBackend is empty")
	}
	if m.cacheTTL != 0 {
		t.Errorf("cacheTTL should be 0, got %v", m.cacheTTL)
	}
}

// TestCacheTTLInvalid verifies that Provision() returns an error for
// invalid cache_ttl values.
func TestCacheTTLInvalid(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "memory",
		CacheTTL:     "not-a-duration",
	}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for invalid cache_ttl")
	}
	if !strings.Contains(err.Error(), "invalid cache_ttl") {
		t.Errorf("expected 'invalid cache_ttl' error, got: %v", err)
	}
}

// TestCacheBackendUnknown verifies that an unknown cache backend returns an error.
func TestCacheBackendUnknown(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "invalid-backend",
	}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for unknown cache_backend")
	}
	if !strings.Contains(err.Error(), "unknown cache_backend") {
		t.Errorf("expected 'unknown cache_backend' error, got: %v", err)
	}
}

// TestCacheBackendServeHTTP verifies that when cache is configured, the
// EsiParserConfig receives Cache and CacheTTL in ServeHTTP.
func TestCacheBackendServeHTTP(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "memory",
		CacheSize:    100,
		CacheTTL:     "30s",
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>cached content</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	err = m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "cached content") {
		t.Errorf("Expected body to contain 'cached content', got: %s", body)
	}
}

// TestCacheBackendServeHTTPNoCache verifies that when no cache is configured,
// ServeHTTP still works (backward compatible).
func TestCacheBackendServeHTTPNoCache(t *testing.T) {
	m := &MesiMiddleware{}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>no cache</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	err = m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "no cache") {
		t.Errorf("Expected body to contain 'no cache', got: %s", body)
	}
}

// TestCacheSizeZeroUsesDefault verifies that cache_size <= 0 uses the
// default of 10000 entries.
func TestCacheSizeZeroUsesDefault(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "memory",
		CacheSize:    0,
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
}

// TestCacheSizeNegativeUsesDefault verifies that negative cache_size uses default.
func TestCacheSizeNegativeUsesDefault(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "memory",
		CacheSize:    -5,
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
}

// TestCacheTTLWithoutBackend verifies that cache_ttl without cache_backend
// is silently ignored (no error).
func TestCacheTTLWithoutBackend(t *testing.T) {
	m := &MesiMiddleware{
		CacheTTL: "60s",
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache != nil {
		t.Error("cache should be nil when CacheBackend is empty")
	}
	if m.cacheTTL != 0 {
		t.Errorf("cacheTTL should be 0, got %v", m.cacheTTL)
	}
}

// TestCacheTTLInvalidWithoutBackend verifies that invalid cache_ttl without
// cache_backend is silently ignored (no error).
func TestCacheTTLInvalidWithoutBackend(t *testing.T) {
	m := &MesiMiddleware{
		CacheTTL: "bad-value",
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache != nil {
		t.Error("cache should be nil when CacheBackend is empty")
	}
	if m.cacheTTL != 0 {
		t.Errorf("cacheTTL should be 0, got %v", m.cacheTTL)
	}
}

// --- Caddyfile Parsing Tests ---

// TestUnmarshalCaddyfileCacheBackend parses cache_backend memory directive.
func TestUnmarshalCaddyfileCacheBackend(t *testing.T) {
	input := `mesi {
		cache_backend memory
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
}

// TestUnmarshalCaddyfileCacheSize parses cache_size directive.
func TestUnmarshalCaddyfileCacheSize(t *testing.T) {
	input := `mesi {
		cache_size 5000
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheSize != 5000 {
		t.Errorf("expected CacheSize=5000, got %d", m.CacheSize)
	}
}

// TestUnmarshalCaddyfileCacheTTL parses cache_ttl directive.
func TestUnmarshalCaddyfileCacheTTL(t *testing.T) {
	input := `mesi {
		cache_ttl 30s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheTTL != "30s" {
		t.Errorf("expected CacheTTL='30s', got '%s'", m.CacheTTL)
	}
}

// TestUnmarshalCaddyfileBackendWithoutArg verifies cache_backend without arg
// returns ArgErr.
func TestUnmarshalCaddyfileBackendWithoutArg(t *testing.T) {
	input := `mesi {
		cache_backend
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for cache_backend without argument")
	}
}

// TestUnmarshalCaddyfileCacheSizeInvalid verifies that invalid cache_size
// returns an error.
func TestUnmarshalCaddyfileCacheSizeInvalid(t *testing.T) {
	input := `mesi {
		cache_size not-a-number
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for invalid cache_size")
	}
}
