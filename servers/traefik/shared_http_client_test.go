package traefik

import (
	"context"
	"net/http"
	"testing"
)

func TestNewSharedHTTPClient(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.SharedHTTPClient = true

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.sharedTransport == nil {
		t.Fatal("Expected non-nil sharedTransport when SharedHTTPClient is true")
	}
}

func TestNewWithoutSharedHTTPClient(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.sharedTransport != nil {
		t.Fatal("Expected nil sharedTransport when SharedHTTPClient is false")
	}
}

func TestSharedHTTPClientTransportIsSSRFSafe(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.SharedHTTPClient = true

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.sharedTransport == nil {
		t.Fatal("Expected non-nil sharedTransport")
	}

	if plugin.sharedTransport.DialContext == nil {
		t.Fatal("Expected DialContext to be set (SSRF-safe transport)")
	}
}

func TestCloseWithSharedHTTPClient(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.SharedHTTPClient = true

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

func TestCloseWithoutSharedHTTPClient(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()

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
