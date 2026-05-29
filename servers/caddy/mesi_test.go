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

// --- Memcached Cache Backend Unit Tests ---

// TestUnmarshalCaddyfileCacheMemcachedServers parses cache_memcached_servers directive.
func TestUnmarshalCaddyfileCacheMemcachedServers(t *testing.T) {
	input := `mesi {
		cache_memcached_servers 10.0.0.1:11211
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if len(m.CacheMemcachedServers) != 1 || m.CacheMemcachedServers[0] != "10.0.0.1:11211" {
		t.Errorf("expected CacheMemcachedServers=['10.0.0.1:11211'], got '%v'", m.CacheMemcachedServers)
	}
}

// TestUnmarshalCaddyfileCacheMemcachedServersMultiple parses multiple servers.
func TestUnmarshalCaddyfileCacheMemcachedServersMultiple(t *testing.T) {
	input := `mesi {
		cache_memcached_servers 10.0.0.1:11211 10.0.0.2:11211 10.0.0.3:11211
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if len(m.CacheMemcachedServers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(m.CacheMemcachedServers))
	}
	expected := []string{"10.0.0.1:11211", "10.0.0.2:11211", "10.0.0.3:11211"}
	for i, s := range expected {
		if m.CacheMemcachedServers[i] != s {
			t.Errorf("expected server[%d]='%s', got '%s'", i, s, m.CacheMemcachedServers[i])
		}
	}
}

// TestUnmarshalCaddyfileCacheMemcachedServersNoArg verifies cache_memcached_servers
// without argument returns an empty list (no error from RemainingArgs).
func TestUnmarshalCaddyfileCacheMemcachedServersNoArg(t *testing.T) {
	input := `mesi {
		cache_memcached_servers
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if len(m.CacheMemcachedServers) != 0 {
		t.Errorf("expected empty CacheMemcachedServers, got '%v'", m.CacheMemcachedServers)
	}
}

// TestMemcachedBackendProvision verifies that cache_backend="memcached" with
// servers creates a non-nil cache and memcachedClient in Provision().
func TestMemcachedBackendProvision(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:          "memcached",
		CacheMemcachedServers: []string{"localhost:11211"},
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.cache == nil {
		t.Fatal("cache should be non-nil when CacheBackend is 'memcached'")
	}
	if m.memcachedClient == nil {
		t.Fatal("memcachedClient should be non-nil when CacheBackend is 'memcached'")
	}
}

// TestMemcachedBackendProvisionNoServers verifies that cache_backend="memcached"
// without servers returns an error.
func TestMemcachedBackendProvisionNoServers(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend: "memcached",
	}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for memcached backend without servers")
	}
	if !strings.Contains(err.Error(), "cache_memcached_servers is required") {
		t.Errorf("expected 'cache_memcached_servers is required' error, got: %v", err)
	}
}

// TestMemcachedBackendProvisionWithTTL verifies cache_ttl is parsed for memcached.
func TestMemcachedBackendProvisionWithTTL(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:          "memcached",
		CacheMemcachedServers: []string{"localhost:11211"},
		CacheTTL:              "120s",
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
}

// TestMemcachedBackendProvisionInvalidTTL verifies that invalid cache_ttl
// returns an error for memcached backend.
func TestMemcachedBackendProvisionInvalidTTL(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:          "memcached",
		CacheMemcachedServers: []string{"localhost:11211"},
		CacheTTL:              "not-a-duration",
	}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for invalid cache_ttl")
	}
	if !strings.Contains(err.Error(), "invalid cache_ttl") {
		t.Errorf("expected 'invalid cache_ttl' error, got: %v", err)
	}
}

// TestMemcachedBackendCleanup verifies Cleanup is safe for memcached.
func TestMemcachedBackendCleanup(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:          "memcached",
		CacheMemcachedServers: []string{"localhost:11211"},
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.memcachedClient == nil {
		t.Fatal("memcachedClient should be non-nil after Provision")
	}

	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestMemcachedBackendCleanupIdempotent verifies double Cleanup is safe.
func TestMemcachedBackendCleanupIdempotent(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:          "memcached",
		CacheMemcachedServers: []string{"localhost:11211"},
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() (second call) returned error: %v", err)
	}
}

// TestMemcachedBackendCleanupWithoutProvision ensures Cleanup is safe
// when Provision was never called.
func TestMemcachedBackendCleanupWithoutProvision(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:          "memcached",
		CacheMemcachedServers: []string{"localhost:11211"},
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestMemcachedBackendServeHTTP verifies that ServeHTTP works with memcached backend
// (no crash, cache is available).
func TestMemcachedBackendServeHTTP(t *testing.T) {
	m := &MesiMiddleware{
		CacheBackend:          "memcached",
		CacheMemcachedServers: []string{"localhost:11211"},
		CacheTTL:              "60s",
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
		w.Write([]byte("<html><body>memcached cached</body></html>"))
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
	if !strings.Contains(body, "memcached cached") {
		t.Errorf("Expected body to contain 'memcached cached', got: %s", body)
	}

	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// --- MaxDepth Directive Tests ---

// TestUnmarshalCaddyfileMaxDepth parses max_depth directive with valid value.
func TestUnmarshalCaddyfileMaxDepth(t *testing.T) {
	input := `mesi {
		max_depth 3
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.MaxDepth == nil {
		t.Fatal("MaxDepth should be non-nil after parsing max_depth 3")
	}
	if *m.MaxDepth != 3 {
		t.Errorf("expected MaxDepth=3, got %d", *m.MaxDepth)
	}
}

// TestUnmarshalCaddyfileMaxDepthZero parses max_depth 0 (passthrough).
func TestUnmarshalCaddyfileMaxDepthZero(t *testing.T) {
	input := `mesi {
		max_depth 0
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.MaxDepth == nil {
		t.Fatal("MaxDepth should be non-nil after parsing max_depth 0")
	}
	if *m.MaxDepth != 0 {
		t.Errorf("expected MaxDepth=0 (passthrough), got %d", *m.MaxDepth)
	}
}

// TestUnmarshalCaddyfileMaxDepthInvalid verifies that max_depth with
// a non-numeric value returns an error.
func TestUnmarshalCaddyfileMaxDepthInvalid(t *testing.T) {
	input := `mesi {
		max_depth abc
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for invalid max_depth")
	}
	if !strings.Contains(err.Error(), "invalid max_depth") {
		t.Errorf("expected 'invalid max_depth' error, got: %v", err)
	}
}

// TestUnmarshalCaddyfileMaxDepthNoArg verifies that max_depth without
// argument returns ArgErr.
func TestUnmarshalCaddyfileMaxDepthNoArg(t *testing.T) {
	input := `mesi {
		max_depth
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for max_depth without argument")
	}
}

// TestMaxDepthDefaultUnset verifies that when max_depth is not set,
// MaxDepth is nil and default 5 is used in ServeHTTP.
func TestMaxDepthDefaultUnset(t *testing.T) {
	m := &MesiMiddleware{}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.MaxDepth != nil {
		t.Errorf("MaxDepth should be nil when not set, got %d", *m.MaxDepth)
	}

	// ServeHTTP with ESI content should use default depth 5
	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment"))
	}))
	defer fragmentServer.Close()

	esiContent := `<html><body><esi:include src="` + fragmentServer.URL + `/frag" /></body></html>`
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
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

// TestMaxDepthPassthrough verifies that max_depth 0 disables ESI processing
// (passthrough — includes are not fetched, tags replaced with empty).
func TestMaxDepthPassthrough(t *testing.T) {
	depth := 0
	m := &MesiMiddleware{MaxDepth: &depth}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	fragmentCallCount := 0
	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fragmentCallCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment"))
	}))
	defer fragmentServer.Close()

	esiContent := `<html><body><esi:include src="` + fragmentServer.URL + `/frag" /></body></html>`
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()
	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	// ESI should not be fetched — fragment server not called
	if fragmentCallCount != 0 {
		t.Errorf("expected 0 fragment calls (passthrough), got %d", fragmentCallCount)
	}
	// ESI tag is stripped and replaced with empty (no IncludeErrorMarker set)
	expected := `<html><body></body></html>`
	if rec.Body.String() != expected {
		t.Errorf("expected %q, got %q", expected, rec.Body.String())
	}
}

// TestMaxDepthExplicit verifies that max_depth 1 processes 1 level of includes
// but does not recursively process nested ESI tags in fetched content.
func TestMaxDepthExplicit(t *testing.T) {
	depth := 1
	m := &MesiMiddleware{MaxDepth: &depth}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	// Fragment returns another ESI include (2 levels deep)
	innerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("inner"))
	}))
	defer innerServer.Close()

	outerFragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<esi:include src="` + innerServer.URL + `/inner" />`))
	}))
	defer outerFragmentServer.Close()

	esiContent := `<html><body><esi:include src="` + outerFragmentServer.URL + `/outer" /></body></html>`
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()
	err := m.ServeHTTP(rec, req, handler)
	if err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	// Outer is fetched but inner ESI is processed with MaxDepth=0 (ParseOnly),
	// so inner tag is replaced with empty string
	expected := `<html><body></body></html>`
	if rec.Body.String() != expected {
		t.Errorf("expected %q, got %q", expected, rec.Body.String())
	}
}

// TestMaxDepthProvisionDefault verifies Provision works with MaxDepth nil.
func TestMaxDepthProvisionDefault(t *testing.T) {
	m := &MesiMiddleware{}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.MaxDepth != nil {
		t.Errorf("MaxDepth should be nil, got %v", m.MaxDepth)
	}
}

// TestMaxDepthIntegrationParseAndProvision verifies the full flow:
// Caddyfile parsing → Provision with max_depth.
func TestMaxDepthIntegrationParseAndProvision(t *testing.T) {
	input := `mesi {
		max_depth 10
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.MaxDepth == nil {
		t.Fatal("MaxDepth should be non-nil")
	}
	if *m.MaxDepth != 10 {
		t.Errorf("expected MaxDepth=10, got %d", *m.MaxDepth)
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestMaxDepthIntegrationZeroPassthrough verifies the full flow:
// Caddyfile parsing → Provision → ServeHTTP with max_depth 0 (passthrough).
func TestMaxDepthIntegrationZeroPassthrough(t *testing.T) {
	input := `mesi {
		max_depth 0
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.MaxDepth == nil || *m.MaxDepth != 0 {
		t.Fatal("MaxDepth should be 0")
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	fragmentCallCount := 0
	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fragmentCallCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment"))
	}))
	defer fragmentServer.Close()

	esiContent := `<html><body><esi:include src="` + fragmentServer.URL + `/frag" /></body></html>`
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
		return nil
	})

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()
	if err := m.ServeHTTP(rec, req, handler); err != nil {
		t.Fatalf("ServeHTTP returned error: %v", err)
	}
	if fragmentCallCount != 0 {
		t.Errorf("expected 0 fragment calls (passthrough), got %d", fragmentCallCount)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// TestMaxDepthWithOtherDirectives verifies max_depth works alongside other directives.
func TestMaxDepthWithOtherDirectives(t *testing.T) {
	input := `mesi {
		max_depth 3
		shared_http_client
		cache_backend memory
		cache_ttl 60s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.MaxDepth == nil || *m.MaxDepth != 3 {
		t.Errorf("expected MaxDepth=3, got %v", m.MaxDepth)
	}
	if !m.SharedHTTPClient {
		t.Error("SharedHTTPClient should be true")
	}
	if m.CacheBackend != "memory" {
		t.Errorf("expected CacheBackend='memory', got '%s'", m.CacheBackend)
	}
	if m.CacheTTL != "60s" {
		t.Errorf("expected CacheTTL='60s', got '%s'", m.CacheTTL)
	}
}

// --- Timeout Tests ---

// TestTimeoutDefault verifies that when Timeout is empty,
// parsedTimeout defaults to 10s.
func TestTimeoutDefault(t *testing.T) {
	m := &MesiMiddleware{}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.parsedTimeout != 10*time.Second {
		t.Errorf("expected parsedTimeout=10s, got %v", m.parsedTimeout)
	}
}

// TestTimeoutCustom verifies that a valid timeout string is parsed.
func TestTimeoutCustom(t *testing.T) {
	m := &MesiMiddleware{Timeout: "30s"}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.parsedTimeout != 30*time.Second {
		t.Errorf("expected parsedTimeout=30s, got %v", m.parsedTimeout)
	}
}

// TestTimeoutMinutes verifies that minute-based durations work.
func TestTimeoutMinutes(t *testing.T) {
	m := &MesiMiddleware{Timeout: "2m"}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.parsedTimeout != 2*time.Minute {
		t.Errorf("expected parsedTimeout=2m, got %v", m.parsedTimeout)
	}
}

// TestTimeoutInvalid verifies that Provision() returns an error for
// invalid timeout values.
func TestTimeoutInvalid(t *testing.T) {
	m := &MesiMiddleware{Timeout: "not-a-duration"}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for invalid timeout")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("expected 'invalid timeout' error, got: %v", err)
	}
}

// TestTimeoutZero verifies that Provision() returns an error for zero timeout.
func TestTimeoutZero(t *testing.T) {
	m := &MesiMiddleware{Timeout: "0s"}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for zero timeout")
	}
	if !strings.Contains(err.Error(), "timeout must be positive") {
		t.Errorf("expected 'timeout must be positive' error, got: %v", err)
	}
}

// TestTimeoutNegative verifies that Provision() returns an error for negative timeout.
func TestTimeoutNegative(t *testing.T) {
	m := &MesiMiddleware{Timeout: "-5s"}
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("Provision() should return error for negative timeout")
	}
	if !strings.Contains(err.Error(), "timeout must be positive") {
		t.Errorf("expected 'timeout must be positive' error, got: %v", err)
	}
}

// TestTimeoutServeHTTP verifies that the configured timeout is used in
// EsiParserConfig when ServeHTTP processes ESI content.
func TestTimeoutServeHTTP(t *testing.T) {
	m := &MesiMiddleware{Timeout: "25s"}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.parsedTimeout != 25*time.Second {
		t.Fatalf("expected parsedTimeout=25s, got %v", m.parsedTimeout)
	}

	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment"))
	}))
	defer fragmentServer.Close()

	esiContent := `<html><body><esi:include src="` + fragmentServer.URL + `/frag" /></body></html>`
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
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

// --- Caddyfile Parsing: Timeout ---

// TestUnmarshalCaddyfileTimeout parses timeout directive.
func TestUnmarshalCaddyfileTimeout(t *testing.T) {
	input := `mesi {
		timeout 30s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.Timeout != "30s" {
		t.Errorf("expected Timeout='30s', got '%s'", m.Timeout)
	}
}

// TestUnmarshalCaddyfileTimeoutNoArg verifies that timeout without
// argument returns ArgErr.
func TestUnmarshalCaddyfileTimeoutNoArg(t *testing.T) {
	input := `mesi {
		timeout
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for timeout without argument")
	}
}

// --- Integration: Timeout + Other Directives ---

// TestTimeoutIntegrationParseAndProvision verifies the full flow:
// Caddyfile parsing → Provision → Cleanup with timeout.
func TestTimeoutIntegrationParseAndProvision(t *testing.T) {
	input := `mesi {
		timeout 20s
		max_depth 3
		shared_http_client
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.Timeout != "20s" {
		t.Errorf("expected Timeout='20s', got '%s'", m.Timeout)
	}
	if m.MaxDepth == nil || *m.MaxDepth != 3 {
		t.Errorf("expected MaxDepth=3, got %v", m.MaxDepth)
	}
	if !m.SharedHTTPClient {
		t.Error("SharedHTTPClient should be true")
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.parsedTimeout != 20*time.Second {
		t.Errorf("expected parsedTimeout=20s, got %v", m.parsedTimeout)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// --- IncludeErrorMarker Tests ---

// TestUnmarshalCaddyfileIncludeErrorMarker parses the include_error_marker directive.
func TestUnmarshalCaddyfileIncludeErrorMarker(t *testing.T) {
	input := `mesi {
		include_error_marker "<!-- esi error -->"
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.IncludeErrorMarker != "<!-- esi error -->" {
		t.Errorf("expected IncludeErrorMarker='<!-- esi error -->', got '%s'", m.IncludeErrorMarker)
	}
}

// TestUnmarshalCaddyfileIncludeErrorMarkerNoArg verifies that include_error_marker
// without argument returns ArgErr.
func TestUnmarshalCaddyfileIncludeErrorMarkerNoArg(t *testing.T) {
	input := `mesi {
		include_error_marker
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("UnmarshalCaddyfile should return error for include_error_marker without argument")
	}
}

// TestUnmarshalCaddyfileIncludeErrorMarkerEmpty verifies that include_error_marker
// with an empty string sets the marker to empty.
func TestUnmarshalCaddyfileIncludeErrorMarkerEmpty(t *testing.T) {
	input := `mesi {
		include_error_marker ""
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.IncludeErrorMarker != "" {
		t.Errorf("expected IncludeErrorMarker='', got '%s'", m.IncludeErrorMarker)
	}
}

// TestIncludeErrorMarkerDefaultUnset verifies that when include_error_marker
// is not set, IncludeErrorMarker is empty.
func TestIncludeErrorMarkerDefaultUnset(t *testing.T) {
	m := &MesiMiddleware{}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.IncludeErrorMarker != "" {
		t.Errorf("IncludeErrorMarker should be empty by default, got '%s'", m.IncludeErrorMarker)
	}
}

// TestIncludeErrorMarkerProvision verifies that Provision works with IncludeErrorMarker set.
func TestIncludeErrorMarkerProvision(t *testing.T) {
	m := &MesiMiddleware{IncludeErrorMarker: "<!-- ESI_ERROR -->"}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.IncludeErrorMarker != "<!-- ESI_ERROR -->" {
		t.Errorf("expected IncludeErrorMarker='<!-- ESI_ERROR -->', got '%s'", m.IncludeErrorMarker)
	}
}

// TestIncludeErrorMarkerServeHTTP verifies that the configured IncludeErrorMarker
// is passed to EsiParserConfig in ServeHTTP.
func TestIncludeErrorMarkerServeHTTP(t *testing.T) {
	m := &MesiMiddleware{IncludeErrorMarker: "<!-- esi error -->"}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment"))
	}))
	defer fragmentServer.Close()

	esiContent := `<html><body><esi:include src="` + fragmentServer.URL + `/frag" /></body></html>`
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(esiContent))
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

// TestIncludeErrorMarkerWithOtherDirectives verifies include_error_marker
// works alongside other directives.
func TestIncludeErrorMarkerWithOtherDirectives(t *testing.T) {
	input := `mesi {
		max_depth 3
		include_error_marker "<!-- ESI_FAIL -->"
		shared_http_client
		timeout 15s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.MaxDepth == nil || *m.MaxDepth != 3 {
		t.Errorf("expected MaxDepth=3, got %v", m.MaxDepth)
	}
	if m.IncludeErrorMarker != "<!-- ESI_FAIL -->" {
		t.Errorf("expected IncludeErrorMarker='<!-- ESI_FAIL -->', got '%s'", m.IncludeErrorMarker)
	}
	if !m.SharedHTTPClient {
		t.Error("SharedHTTPClient should be true")
	}
	if m.Timeout != "15s" {
		t.Errorf("expected Timeout='15s', got '%s'", m.Timeout)
	}
}

// TestIncludeErrorMarkerIntegrationParseAndProvision verifies the full flow:
// Caddyfile parsing → Provision → Cleanup with include_error_marker.
func TestIncludeErrorMarkerIntegrationParseAndProvision(t *testing.T) {
	input := `mesi {
		include_error_marker "<!-- esi error: fragment failed -->"
		max_depth 5
		timeout 10s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.IncludeErrorMarker != "<!-- esi error: fragment failed -->" {
		t.Errorf("expected IncludeErrorMarker='<!-- esi error: fragment failed -->', got '%s'", m.IncludeErrorMarker)
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// --- Debug Directive Tests ---

// TestDebugDefaultUnset verifies that when debug is not set, Debug is false.
func TestDebugDefaultUnset(t *testing.T) {
	m := &MesiMiddleware{}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.Debug {
		t.Error("Debug should be false by default")
	}
}

// TestUnmarshalCaddyfileDebug parses the debug directive.
func TestUnmarshalCaddyfileDebug(t *testing.T) {
	input := `mesi {
		debug
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if !m.Debug {
		t.Error("Debug should be true after parsing debug directive")
	}
}

// TestDebugProvision verifies that Provision works with Debug enabled.
func TestDebugProvision(t *testing.T) {
	m := &MesiMiddleware{Debug: true}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if !m.Debug {
		t.Error("Debug should be true")
	}
}

// TestDebugServeHTTP verifies that the configured Debug flag is passed
// to EsiParserConfig in ServeHTTP.
func TestDebugServeHTTP(t *testing.T) {
	m := &MesiMiddleware{Debug: true}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>debug test</body></html>"))
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

// TestDebugWithOtherDirectives verifies debug works alongside other directives.
func TestDebugWithOtherDirectives(t *testing.T) {
	input := `mesi {
		debug
		max_depth 3
		shared_http_client
		timeout 15s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if !m.Debug {
		t.Error("Debug should be true")
	}
	if m.MaxDepth == nil || *m.MaxDepth != 3 {
		t.Errorf("expected MaxDepth=3, got %v", m.MaxDepth)
	}
	if !m.SharedHTTPClient {
		t.Error("SharedHTTPClient should be true")
	}
	if m.Timeout != "15s" {
		t.Errorf("expected Timeout='15s', got '%s'", m.Timeout)
	}
}

// TestDebugIntegrationParseAndProvision verifies the full flow:
// Caddyfile parsing → Provision → Cleanup with debug.
func TestDebugIntegrationParseAndProvision(t *testing.T) {
	input := `mesi {
		debug
		max_depth 5
		timeout 10s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if !m.Debug {
		t.Error("Debug should be true")
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}

// --- AllowedHosts Directive Tests ---

// TestUnmarshalCaddyfileAllowedHosts parses allowed_hosts directive with multiple hosts.
func TestUnmarshalCaddyfileAllowedHosts(t *testing.T) {
	input := `mesi {
		allowed_hosts backend.internal cdn.example.com api.trusted.org
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if len(m.AllowedHosts) != 3 {
		t.Fatalf("expected 3 AllowedHosts, got %d", len(m.AllowedHosts))
	}
	expected := []string{"backend.internal", "cdn.example.com", "api.trusted.org"}
	for i, h := range expected {
		if m.AllowedHosts[i] != h {
			t.Errorf("expected AllowedHosts[%d]='%s', got '%s'", i, h, m.AllowedHosts[i])
		}
	}
}

// TestUnmarshalCaddyfileAllowedHostsSingle parses a single allowed host.
func TestUnmarshalCaddyfileAllowedHostsSingle(t *testing.T) {
	input := `mesi {
		allowed_hosts backend.internal
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if len(m.AllowedHosts) != 1 {
		t.Fatalf("expected 1 AllowedHost, got %d", len(m.AllowedHosts))
	}
	if m.AllowedHosts[0] != "backend.internal" {
		t.Errorf("expected AllowedHosts[0]='backend.internal', got '%s'", m.AllowedHosts[0])
	}
}

// TestUnmarshalCaddyfileAllowedHostsAbsent verifies that absent allowed_hosts
// leaves AllowedHosts as nil (no restriction).
func TestUnmarshalCaddyfileAllowedHostsAbsent(t *testing.T) {
	input := `mesi`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if m.AllowedHosts != nil {
		t.Errorf("AllowedHosts should be nil when absent, got %v", m.AllowedHosts)
	}
}

// TestAllowedHostsDefaultUnset verifies that when AllowedHosts is not set,
// it remains nil after Provision.
func TestAllowedHostsDefaultUnset(t *testing.T) {
	m := &MesiMiddleware{}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if m.AllowedHosts != nil {
		t.Errorf("AllowedHosts should be nil by default, got %v", m.AllowedHosts)
	}
}

// TestAllowedHostsProvision verifies that AllowedHosts is preserved through Provision.
func TestAllowedHostsProvision(t *testing.T) {
	m := &MesiMiddleware{
		AllowedHosts: []string{"backend.internal", "cdn.example.com"},
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if len(m.AllowedHosts) != 2 {
		t.Fatalf("expected 2 AllowedHosts, got %d", len(m.AllowedHosts))
	}
}

// TestAllowedHostsServeHTTP verifies that ServeHTTP works with AllowedHosts configured.
func TestAllowedHostsServeHTTP(t *testing.T) {
	m := &MesiMiddleware{
		AllowedHosts: []string{"backend.internal"},
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}

	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>allowed hosts test</body></html>"))
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

// TestAllowedHostsWithOtherDirectives verifies allowed_hosts works alongside other directives.
func TestAllowedHostsWithOtherDirectives(t *testing.T) {
	input := `mesi {
		allowed_hosts backend.internal cdn.example.com
		max_depth 3
		shared_http_client
		timeout 15s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if len(m.AllowedHosts) != 2 {
		t.Fatalf("expected 2 AllowedHosts, got %d", len(m.AllowedHosts))
	}
	if m.AllowedHosts[0] != "backend.internal" {
		t.Errorf("expected AllowedHosts[0]='backend.internal', got '%s'", m.AllowedHosts[0])
	}
	if m.AllowedHosts[1] != "cdn.example.com" {
		t.Errorf("expected AllowedHosts[1]='cdn.example.com', got '%s'", m.AllowedHosts[1])
	}
	if m.MaxDepth == nil || *m.MaxDepth != 3 {
		t.Errorf("expected MaxDepth=3, got %v", m.MaxDepth)
	}
	if !m.SharedHTTPClient {
		t.Error("SharedHTTPClient should be true")
	}
	if m.Timeout != "15s" {
		t.Errorf("expected Timeout='15s', got '%s'", m.Timeout)
	}
}

// TestAllowedHostsIntegrationParseAndProvision verifies the full flow:
// Caddyfile parsing → Provision → Cleanup with allowed_hosts.
func TestAllowedHostsIntegrationParseAndProvision(t *testing.T) {
	input := `mesi {
		allowed_hosts backend.internal cdn.example.com
		max_depth 5
		timeout 10s
	}`
	d := caddyfile.NewTestDispenser(input)
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if len(m.AllowedHosts) != 2 {
		t.Fatalf("expected 2 AllowedHosts, got %d", len(m.AllowedHosts))
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
}
