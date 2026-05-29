package roadrunner

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInitSharedHTTPClient(t *testing.T) {
	config := CreateConfig()
	config.SharedHTTPClient = true

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if p.sharedTransport == nil {
		t.Fatal("Expected non-nil sharedTransport when SharedHTTPClient is true")
	}
}

func TestInitWithoutSharedHTTPClient(t *testing.T) {
	config := CreateConfig()

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if p.sharedTransport != nil {
		t.Fatal("Expected nil sharedTransport when SharedHTTPClient is false")
	}
}

func TestSharedHTTPClientTransportIsSSRFSafe(t *testing.T) {
	config := CreateConfig()
	config.SharedHTTPClient = true

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if p.sharedTransport == nil {
		t.Fatal("Expected non-nil sharedTransport")
	}

	if p.sharedTransport.DialContext == nil {
		t.Fatal("Expected DialContext to be set (SSRF-safe transport)")
	}
}

func TestCloseWithSharedHTTPClient(t *testing.T) {
	config := CreateConfig()
	config.SharedHTTPClient = true

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Unexpected error closing plugin: %v", err)
	}
}

func TestCloseWithoutSharedHTTPClient(t *testing.T) {
	p := &Plugin{}
	if err := p.Close(); err != nil {
		t.Fatalf("Unexpected error closing plugin: %v", err)
	}
}

func TestMiddlewareWithSharedHTTPClient(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
	})

	config := CreateConfig()
	config.SharedHTTPClient = true

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(handler)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}
