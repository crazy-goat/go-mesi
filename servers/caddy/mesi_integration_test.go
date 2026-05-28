package caddy

import (
	"testing"

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
