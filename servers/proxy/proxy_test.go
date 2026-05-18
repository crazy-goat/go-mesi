package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
)

func TestNewProxy_ValidBackend(t *testing.T) {
	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy("http://example.com", config)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if proxy == nil {
		t.Fatal("expected non-nil proxy")
	}
}

func TestNewProxy_InvalidBackendURL(t *testing.T) {
	config := mesi.CreateDefaultConfig()
	_, err := NewProxy("://invalid", config)
	if err == nil {
		t.Fatal("expected error for invalid backend URL")
	}
}

func TestProxy_ContentTypeGating_HTMLProcessed(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("Hello <!--esi World-->"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	config.Timeout = 5 * time.Second
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "World") {
		t.Errorf("expected ESI processed content, got: %s", body)
	}
	if strings.Contains(body, "<!--esi") {
		t.Errorf("expected <!--esi wrapper removed, got: %s", body)
	}
}

func TestProxy_ContentTypeGating_NonHTMLPassthrough(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("<esi:include src=\"http://example.com/test\"/>"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	config.Timeout = 5 * time.Second
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "<esi:include") {
		t.Errorf("expected raw ESI tags preserved for non-HTML, got: %s", body)
	}
}

func TestProxy_ContentTypeGating_HTMLPassthroughOnNonEsi(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>no esi here</body></html>"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	config.Timeout = 5 * time.Second
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	body := rec.Body.String()
	if expected := "<html><body>no esi here</body></html>"; body != expected {
		t.Errorf("expected unchanged HTML body %q, got %q", expected, body)
	}
}

func TestProxy_ParseOnHeader_WithEdgeControl(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Edge-control", "dca=esi")
		w.Write([]byte("Hello <!--esi World-->"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	config.ParseOnHeader = true
	config.Timeout = 5 * time.Second
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "World") {
		t.Errorf("expected ESI processed (Edge-control present), got: %s", body)
	}
	if strings.Contains(body, "<!--esi") {
		t.Errorf("expected <!--esi wrapper removed, got: %s", body)
	}
}

func TestProxy_ParseOnHeader_WithoutEdgeControl(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("Hello <!--esi World-->"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	config.ParseOnHeader = true
	config.Timeout = 5 * time.Second
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "<!--esi") {
		t.Errorf("expected raw ESI (no Edge-control header), got: %s", body)
	}
}

func TestProxy_SurrogateCapabilityHeader_Injected(t *testing.T) {
	var capturedHeaders http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if capturedHeaders.Get("Surrogate-Capability") != "ESI/1.0" {
		t.Errorf("expected Surrogate-Capability request header, got: %q", capturedHeaders.Get("Surrogate-Capability"))
	}
}

func TestProxy_SurrogateCapabilityHeader_NotOverwritten(t *testing.T) {
	var capturedHeaders http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Surrogate-Capability", "ESI/1.0")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if capturedHeaders.Get("Surrogate-Capability") != "ESI/1.0" {
		t.Errorf("expected Surrogate-Capability header preserved, got: %q", capturedHeaders.Get("Surrogate-Capability"))
	}
}

func TestProxy_SurrogateControlHeader_InResponse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if got := rec.Header().Get("Surrogate-Control"); got != "ESI/1.0" {
		t.Errorf("expected Surrogate-Control header in response, got: %q", got)
	}
}

func TestProxy_ContentLength_Recalculated(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		original := "Hello <!--esi World-->"
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(original)))
		w.Write([]byte(original))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	config.Timeout = 5 * time.Second
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "World") {
		t.Errorf("expected ESI processed content, got: %q", body)
	}
	if strings.Contains(body, "<!--esi") {
		t.Errorf("expected <!--esi wrapper removed, got: %q", body)
	}
	cl := rec.Header().Get("Content-Length")
	if cl == "" {
		t.Fatal("expected Content-Length to be set")
	}
	if cl != fmt.Sprintf("%d", len(body)) {
		t.Errorf("Content-Length %s does not match body length %d", cl, len(body))
	}
}

func TestProxy_ContentLength_NonHTMLPreserved(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		body := "<esi:include src=\"http://example.com/test\"/>"
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.Write([]byte(body))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	expectedBody := "<esi:include src=\"http://example.com/test\"/>"
	expectedLen := fmt.Sprintf("%d", len(expectedBody))
	if rec.Header().Get("Content-Length") != expectedLen {
		t.Errorf("expected Content-Length %s, got %s", expectedLen, rec.Header().Get("Content-Length"))
	}
}

func TestProxy_ErrorStatus_Passthrough(t *testing.T) {
	tests := []struct {
		code int
		name string
	}{
		{400, "Bad Request"},
		{404, "Not Found"},
		{500, "Internal Server Error"},
		{502, "Bad Gateway"},
		{503, "Service Unavailable"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.code), func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(tt.code)
				w.Write([]byte(tt.name))
			}))
			defer backend.Close()

			config := mesi.CreateDefaultConfig()
			config.Timeout = 5 * time.Second
			proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)

			if rec.Code != tt.code {
				t.Errorf("expected status %d, got %d", tt.code, rec.Code)
			}
		})
	}
}

func TestProxy_OriginalHeaders_Preserved(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("expected X-Custom-Header preserved, got: %q", got)
	}
}

func TestProxy_DefaultUrl_SetFromHost(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	config.Timeout = 5 * time.Second
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Host = "myhost.local:8080"
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	result := rec.Body.String()
	if result != "ok" {
		t.Errorf("expected body 'ok', got %q", result)
	}
}

func TestProxy_ConfigPreserved(t *testing.T) {
	config := mesi.CreateDefaultConfig()
	config.MaxDepth = 3
	config.Timeout = 30 * time.Second
	config.ParseOnHeader = true
	config.BlockPrivateIPs = false
	config.Debug = true

	proxy, err := NewProxy("http://example.com", config)
	if err != nil {
		t.Fatal(err)
	}

	if proxy.config.MaxDepth != 3 {
		t.Errorf("expected MaxDepth 3, got %d", proxy.config.MaxDepth)
	}
	if proxy.config.Timeout != 30*time.Second {
		t.Errorf("expected Timeout 30s, got %v", proxy.config.Timeout)
	}
	if proxy.config.ParseOnHeader != true {
		t.Errorf("expected ParseOnHeader true, got %v", proxy.config.ParseOnHeader)
	}
	if proxy.config.BlockPrivateIPs != false {
		t.Errorf("expected BlockPrivateIPs false, got %v", proxy.config.BlockPrivateIPs)
	}
	if proxy.config.Debug != true {
		t.Errorf("expected Debug true, got %v", proxy.config.Debug)
	}
}

func TestProxy_NoSurrogateControlOnNonHTML(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("plain text"))
	}))
	defer backend.Close()

	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if got := rec.Header().Get("Surrogate-Control"); got != "" {
		t.Errorf("expected no Surrogate-Control for non-HTML, got: %q", got)
	}
}

func TestProxy_BackendURLStored(t *testing.T) {
	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy("http://backend.test:9090", config)
	if err != nil {
		t.Fatal(err)
	}

	if proxy.backend != "http://backend.test:9090" {
		t.Errorf("expected backend 'http://backend.test:9090', got %q", proxy.backend)
	}
	if proxy.backendURL.String() != "http://backend.test:9090" {
		t.Errorf("expected backendURL 'http://backend.test:9090', got %q", proxy.backendURL.String())
	}
}

func TestProxy_TransportConfigured(t *testing.T) {
	config := mesi.CreateDefaultConfig()
	proxy, err := NewProxy("http://example.com", config)
	if err != nil {
		t.Fatal(err)
	}

	if proxy.transport == nil {
		t.Fatal("expected transport to be non-nil")
	}
}
