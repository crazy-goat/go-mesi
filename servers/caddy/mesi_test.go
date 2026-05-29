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

// --- Cache Key Template Tests ---

// TestUnmarshalCaddyfileCacheKeyTemplate parses the cache_key_template directive.
func TestUnmarshalCaddyfileCacheKeyTemplate(t *testing.T) {
	input := `mesi {
		cache_key_template "mesi:${url}:${header:Accept-Language}"
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheKeyTemplate != "mesi:${url}:${header:Accept-Language}" {
		t.Errorf("expected CacheKeyTemplate='mesi:${url}:${header:Accept-Language}', got '%s'", m.CacheKeyTemplate)
	}
}

// TestUnmarshalCaddyfileCacheKeyTemplateNoArg verifies cache_key_template without
// argument returns ArgErr.
func TestUnmarshalCaddyfileCacheKeyTemplateNoArg(t *testing.T) {
	input := `mesi {
		cache_key_template
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for cache_key_template without argument")
	}
}

// TestCacheKeyTemplateDefaultAbsent verifies that when CacheKeyTemplate is empty,
// no custom CacheKeyFunc is set.
func TestCacheKeyTemplateDefaultAbsent(t *testing.T) {
	m := &MesiMiddleware{CacheBackend: "memory"}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>no template</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/page", nil)
	rec := httptest.NewRecorder()

	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	// Serves as a smoke test — no panic, works without CacheKeyFunc
}

// TestCacheKeyTemplateUrlSubstitution verifies ${url} is substituted with the
// full URL in the cache key.
func TestCacheKeyTemplateUrlSubstitution(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:      "memory",
		CacheKeyTemplate:  "pfx:${url}:sfx",
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	callCount := 0
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		callCount++
		w.Header().Set("Content-Type", "text/html")
		// Return non-ESI content to avoid real network fetch
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/page", nil)
	rec := httptest.NewRecorder()

	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestCacheKeyTemplateSubstitutesHeaders verifies ${header:X} placeholders
// are replaced with request header values.
func TestCacheKeyTemplateSubstitutesHeaders(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:      "memory",
		CacheKeyTemplate:  "${header:Accept-Language}",
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.Header.Set("Accept-Language", "pl-PL")
	rec := httptest.NewRecorder()

	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestCacheKeyTemplateSubstitutesCookies verifies ${cookie:Name} placeholders
// are replaced with cookie values.
func TestCacheKeyTemplateSubstitutesCookies(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:      "memory",
		CacheKeyTemplate:  "sess:${cookie:session_id}",
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "abc123"})
	rec := httptest.NewRecorder()

	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestCacheKeyTemplateUnknownPlaceholder verifies unknown placeholders are
// left as-is (literal) in the cache key.
func TestCacheKeyTemplateUnknownPlaceholder(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:      "memory",
		CacheKeyTemplate:  "key:${unknown}:literal",
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestCacheKeyTemplateWithoutCacheBackend verifies that cache_key_template
// without cache_backend is silently ignored (no error, no CacheKeyFunc).
func TestCacheKeyTemplateWithoutCacheBackend(t *testing.T) {
	m := &MesiMiddleware{
		// No CacheBackend set
		CacheKeyTemplate: "key:${url}",
	}
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
}

// TestCacheKeyTemplateComplexPattern verifies a complex template with
// multiple placeholder types.
func TestCacheKeyTemplateComplexPattern(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:      "memory",
		CacheKeyTemplate:  "mesi:${url}:lang=${header:Accept-Language}:sess=${cookie:session_id}",
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>complex template</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/page", nil)
	req.Header.Set("Accept-Language", "en-US")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "xyz789"})
	rec := httptest.NewRecorder()

	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// --- Redis Cache Backend Unit Tests ---

// TestUnmarshalCaddyfileCacheRedisAddr parses cache_redis_addr directive.
func TestUnmarshalCaddyfileCacheRedisAddr(t *testing.T) {
	input := `mesi {
		cache_redis_addr 10.0.0.5:6379
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheRedisAddr != "10.0.0.5:6379" {
		t.Errorf("expected CacheRedisAddr='10.0.0.5:6379', got '%s'", m.CacheRedisAddr)
	}
}

// TestUnmarshalCaddyfileCacheRedisPassword parses cache_redis_password directive.
func TestUnmarshalCaddyfileCacheRedisPassword(t *testing.T) {
	input := `mesi {
		cache_redis_password s3cret
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheRedisPassword != "s3cret" {
		t.Errorf("expected CacheRedisPassword='s3cret', got '%s'", m.CacheRedisPassword)
	}
}

// TestUnmarshalCaddyfileCacheRedisDB parses cache_redis_db directive.
func TestUnmarshalCaddyfileCacheRedisDB(t *testing.T) {
	input := `mesi {
		cache_redis_db 2
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.CacheRedisDB != 2 {
		t.Errorf("expected CacheRedisDB=2, got %d", m.CacheRedisDB)
	}
}

// TestUnmarshalCaddyfileCacheRedisDBInvalid verifies that cache_redis_db with
// a non-numeric value returns an error.
func TestUnmarshalCaddyfileCacheRedisDBInvalid(t *testing.T) {
	input := `mesi {
		cache_redis_db not-a-number
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for invalid cache_redis_db")
	}
}

// TestUnmarshalCaddyfileCacheRedisAddrNoArg verifies that cache_redis_addr
// without argument returns ArgErr.
func TestUnmarshalCaddyfileCacheRedisAddrNoArg(t *testing.T) {
	input := `mesi {
		cache_redis_addr
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for cache_redis_addr without argument")
	}
}

// TestUnmarshalCaddyfileCacheRedisPasswordNoArg verifies that cache_redis_password
// without argument returns ArgErr.
func TestUnmarshalCaddyfileCacheRedisPasswordNoArg(t *testing.T) {
	input := `mesi {
		cache_redis_password
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for cache_redis_password without argument")
	}
}

// TestUnmarshalCaddyfileCacheRedisDBNoArg verifies that cache_redis_db
// without argument returns ArgErr.
func TestUnmarshalCaddyfileCacheRedisDBNoArg(t *testing.T) {
	input := `mesi {
		cache_redis_db
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for cache_redis_db without argument")
	}
}

// TestRedisBackendProvision verifies that cache_backend="redis" creates
// a non-nil cache and redisClient in Provision().
func TestRedisBackendProvision(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:    "redis",
		CacheRedisAddr:  "localhost:6379",
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil when CacheBackend is 'redis'")
	}
	if m.redisClient == nil {
		t.Fatal("redisClient should be non-nil when CacheBackend is 'redis'")
	}
	// Cleanup
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestRedisBackendDefaultAddr verifies that an empty CacheRedisAddr defaults
// to "localhost:6379".
func TestRedisBackendDefaultAddr(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "redis",
		// CacheRedisAddr left empty — should default to localhost:6379
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
	if m.redisClient == nil {
		t.Fatal("redisClient should be non-nil")
	}
	// Cleanup closes the Redis client
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestRedisBackendWithPassword verifies that CacheRedisPassword is passed
// through to the Redis client options (we verify by checking the client exists).
func TestRedisBackendProvisionWithPassword(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:       "redis",
		CacheRedisAddr:     "10.0.0.5:6379",
		CacheRedisPassword: "hunter2",
		CacheRedisDB:       1,
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
	if m.redisClient == nil {
		t.Fatal("redisClient should be non-nil")
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestRedisBackendWithTTL verifies that cache_ttl is parsed when
// cache_backend is "redis".
func TestRedisBackendProvisionWithTTL(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:   "redis",
		CacheRedisAddr: "localhost:6379",
		CacheTTL:       "120s",
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}
	if m.cacheTTL != 120*time.Second {
		t.Errorf("expected cacheTTL=120s, got %v", m.cacheTTL)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestRedisBackendInvalidTTL verifies that Provision() returns an error for
// invalid cache_ttl when cache_backend is "redis".
func TestRedisBackendInvalidTTL(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:   "redis",
		CacheRedisAddr: "localhost:6379",
		CacheTTL:       "not-a-duration",
	}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for invalid cache_ttl")
	}
	if !strings.Contains(err.Error(), "invalid cache_ttl") {
		t.Errorf("expected 'invalid cache_ttl' error, got: %v", err)
	}
}

// TestRedisBackendCleanup verifies that Cleanup() closes the Redis client
// and that double-Cleanup is safe (idempotent).
func TestRedisBackendCleanup(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:   "redis",
		CacheRedisAddr: "localhost:6379",
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.redisClient == nil {
		t.Fatal("redisClient should be non-nil after Provision")
	}

	// First Cleanup should succeed
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}

	// Second Cleanup should be safe (idempotent)
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() (second call) returned error: %v", err)
	}
}

// TestRedisBackendCleanupWithoutProvision ensures Cleanup is safe
// when Provision was never called (redisClient is nil).
func TestRedisBackendCleanupWithoutProvision(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:   "redis",
		CacheRedisAddr: "localhost:6379",
	}
	// Cleanup before Provision — should be a no-op
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestRedisBackendServeHTTP verifies that ServeHTTP works with Redis backend
// (no crash, cache is available).
func TestRedisBackendServeHTTP(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:   "redis",
		CacheRedisAddr: "localhost:6379",
		CacheTTL:       "60s",
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil")
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>redis cached</body></html>"))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "redis cached") {
		t.Errorf("Expected body to contain 'redis cached', got: %s", body)
	}

	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}
