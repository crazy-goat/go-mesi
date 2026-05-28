package caddy

import (
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
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
