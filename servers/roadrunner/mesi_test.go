package roadrunner

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()
	if config.MaxDepth != 5 {
		t.Errorf("Expected MaxDepth 5, got %d", config.MaxDepth)
	}
}

func TestInitDefaults(t *testing.T) {
	p := &Plugin{}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.config.MaxDepth != 5 {
		t.Errorf("Expected MaxDepth 5, got %d", p.config.MaxDepth)
	}
	if p.cache != nil {
		t.Error("Expected nil cache with default config")
	}
}

func TestInitMemoryCache(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheSize = 100

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestInitMemoryCacheDefaultSize(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestInitInvalidCacheTTL(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "invalid"

	p := &Plugin{config: config}
	if err := p.Init(); err == nil {
		t.Fatal("Expected error for invalid cache TTL")
	}
}

func TestInitCacheBackendWithoutTTL(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Error("Expected non-nil cache for memory backend even without TTL")
	}
	if p.cacheTTL != 0 {
		t.Errorf("Expected zero cacheTTL when CacheTTL is empty, got %v", p.cacheTTL)
	}
}

func TestName(t *testing.T) {
	p := &Plugin{}
	if p.Name() != "mesi" {
		t.Errorf("Expected name 'mesi', got %s", p.Name())
	}
}

func TestClose(t *testing.T) {
	p := &Plugin{}
	if err := p.Close(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestMiddlewareNonHTML(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	config := CreateConfig()
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(handler)
	req := httptest.NewRequest("GET", "http://example.com/api", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("Expected body {'status':'ok'}, got %s", rec.Body.String())
	}
}

func TestMiddlewareHTML(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
	})

	config := CreateConfig()
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

func TestMiddlewareWithCache(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><esi:include src=\"/fragment\" /></body></html>"))
	})

	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

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

func TestMiddlewareSurrogateCapability(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
	})

	config := CreateConfig()
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(handler)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if req.Header.Get("Surrogate-Capability") != "ESI/1.0" {
		t.Errorf("Expected Surrogate-Capability header, got %s", req.Header.Get("Surrogate-Capability"))
	}
}

func TestMiddlewareIncludeErrorMarker(t *testing.T) {
	config := CreateConfig()
	config.IncludeErrorMarker = "<!-- esi error -->"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if p.config.IncludeErrorMarker != "<!-- esi error -->" {
		t.Errorf("Expected include_error_marker '<!-- esi error -->', got %s", p.config.IncludeErrorMarker)
	}
}

func TestInitCacheKeyTemplate(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheKeyTemplate = "mesi:${url}:${header:Accept-Language}"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.config.CacheKeyTemplate != "mesi:${url}:${header:Accept-Language}" {
		t.Errorf("Expected cache_key_template 'mesi:${url}:${header:Accept-Language}', got %s", p.config.CacheKeyTemplate)
	}
}

func TestInitCacheKeyTemplateDefaultEmpty(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.config.CacheKeyTemplate != "" {
		t.Errorf("Expected empty cache_key_template, got %s", p.config.CacheKeyTemplate)
	}
}

// newBlockPrivateIPsTestServers spins up a private-IP (127.0.0.1) fragment
// server and an upstream server that emits an <esi:include> pointing at it.
// 127.0.0.1 is a loopback/reserved address, so it is blocked by the SSRF
// dial-time check whenever BlockPrivateIPs is enabled.
func newBlockPrivateIPsTestServers(t *testing.T) (fragmentURL string, upstream http.Handler) {
	t.Helper()

	fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("FRAGMENT_OK"))
	}))
	t.Cleanup(fragment.Close)

	upstream = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf("<html><body><esi:include src=\"%s/fragment\" /></body></html>", fragment.URL)))
	})

	return fragment.URL, upstream
}

func TestBlockPrivateIPsDefaultBlocks(t *testing.T) {
	_, upstream := newBlockPrivateIPsTestServers(t)

	config := CreateConfig()
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(upstream)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected private-IP include to be blocked by default, got body: %s", rec.Body.String())
	}
}

func TestBlockPrivateIPsTrueBlocks(t *testing.T) {
	_, upstream := newBlockPrivateIPsTestServers(t)

	block := true
	config := CreateConfig()
	config.BlockPrivateIPs = &block
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(upstream)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected private-IP include to be blocked when block_private_ips=true, got body: %s", rec.Body.String())
	}
}

func TestBlockPrivateIPsFalseAllows(t *testing.T) {
	_, upstream := newBlockPrivateIPsTestServers(t)

	block := false
	config := CreateConfig()
	config.BlockPrivateIPs = &block
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(upstream)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected private-IP include to be allowed when block_private_ips=false, got body: %s", rec.Body.String())
	}
}
