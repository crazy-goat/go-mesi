package traefik

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/crazy-goat/go-mesi/mesi"
)

func TestCacheKeyTemplateConfigField(t *testing.T) {
	t.Run("default empty", func(t *testing.T) {
		cfg := CreateConfig()
		if cfg.CacheKeyTemplate != "" {
			t.Errorf("expected empty CacheKeyTemplate by default, got %q", cfg.CacheKeyTemplate)
		}
	})
	t.Run("set and propagated", func(t *testing.T) {
		cfg := CreateConfig()
		cfg.CacheKeyTemplate = "mesi:${url}:${header:Accept-Language}"
		cfg.CacheBackend = "memory"
		cfg.CacheTTL = "60s"
		p, err := New(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), cfg, "test")
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		plugin := p.(*ResponsePlugin)
		if plugin.config.CacheKeyTemplate != "mesi:${url}:${header:Accept-Language}" {
			t.Errorf("expected CacheKeyTemplate preserved, got %q", plugin.config.CacheKeyTemplate)
		}
		if plugin.cache == nil {
			t.Fatal("expected cache non-nil")
		}
	})
}

func TestCacheKeyTemplateBuildCacheKeyDirect(t *testing.T) {
	t.Run("url substitution", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://example.com/", nil)
		key := mesi.BuildCacheKey("http://backend/frag", "mesi:${url}", r)
		if key != "mesi:http://backend/frag" {
			t.Errorf("expected 'mesi:http://backend/frag', got %q", key)
		}
	})
	t.Run("header substitution different headers different keys", func(t *testing.T) {
		r1 := httptest.NewRequest("GET", "http://example.com/", nil)
		r1.Header.Set("Accept-Language", "en")
		r2 := httptest.NewRequest("GET", "http://example.com/", nil)
		r2.Header.Set("Accept-Language", "pl")
		tmpl := "k:${header:Accept-Language}"
		k1 := mesi.BuildCacheKey("/frag", tmpl, r1)
		k2 := mesi.BuildCacheKey("/frag", tmpl, r2)
		if k1 == k2 {
			t.Errorf("different header values must produce different keys, both %q", k1)
		}
		if k1 != "k:en" {
			t.Errorf("expected 'k:en', got %q", k1)
		}
		if k2 != "k:pl" {
			t.Errorf("expected 'k:pl', got %q", k2)
		}
	})
	t.Run("cookie substitution", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://example.com/", nil)
		r.AddCookie(&http.Cookie{Name: "session_id", Value: "abc123"})
		key := mesi.BuildCacheKey("/frag", "sess:${cookie:session_id}", r)
		if key != "sess:abc123" {
			t.Errorf("expected 'sess:abc123', got %q", key)
		}
	})
	t.Run("unknown placeholder literal passthrough", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://example.com/", nil)
		key := mesi.BuildCacheKey("/frag", "k:${unknown}:end", r)
		if key != "k:${unknown}:end" {
			t.Errorf("expected literal passthrough 'k:${unknown}:end', got %q", key)
		}
	})
	t.Run("empty template passthrough", func(t *testing.T) {
		r := httptest.NewRequest("GET", "http://example.com/", nil)
		key := mesi.BuildCacheKey("/frag", "", r)
		if key != "" {
			t.Errorf("expected empty string for empty template, got %q", key)
		}
	})
}

func newCacheKeyTemplateTestServers(t *testing.T, hits *atomic.Int32) (string, http.Handler) {
	t.Helper()
	fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "FRAG-%d", n)
	}))
	t.Cleanup(fragment.Close)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<html><body><esi:include src="%s/frag" /></body></html>`, fragment.URL)
	})
	return fragment.URL, upstream
}

func TestCacheKeyTemplatePluginHeaderIsolation(t *testing.T) {
	var hits atomic.Int32
	_, upstream := newCacheKeyTemplateTestServers(t, &hits)

	cfg := CreateConfig()
	cfg.CacheBackend = "memory"
	cfg.CacheTTL = "60s"
	cfg.CacheKeyTemplate = "k:${header:X-Lang}"
	cfg.BlockPrivateIPs = false

	p, err := New(context.Background(), upstream, cfg, "test")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	t.Run("same header hits cache", func(t *testing.T) {
		hits.Store(0)
		// Use a fresh plugin instance so cache is empty at start of subtest
		p2, err := New(context.Background(), upstream, cfg, "test")
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		req1 := httptest.NewRequest("GET", "http://example.com/", nil)
		req1.Header.Set("X-Lang", "en")
		rec1 := httptest.NewRecorder()
		p2.ServeHTTP(rec1, req1)
		if hits.Load() != 1 {
			t.Fatalf("first fetch should hit backend once, got %d", hits.Load())
		}
		req2 := httptest.NewRequest("GET", "http://example.com/", nil)
		req2.Header.Set("X-Lang", "en")
		rec2 := httptest.NewRecorder()
		p2.ServeHTTP(rec2, req2)
		if hits.Load() != 1 {
			t.Errorf("second fetch with same header should be cached, hits=%d", hits.Load())
		}
		if rec1.Body.String() != rec2.Body.String() {
			t.Errorf("cached response mismatch: %q vs %q", rec1.Body.String(), rec2.Body.String())
		}
	})

	t.Run("different headers miss cache", func(t *testing.T) {
		hits.Store(0)
		p2, err := New(context.Background(), upstream, cfg, "test")
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		// silence unused p (we use p2 for isolation)
		_ = p
		req1 := httptest.NewRequest("GET", "http://example.com/", nil)
		req1.Header.Set("X-Lang", "en")
		rec1 := httptest.NewRecorder()
		p2.ServeHTTP(rec1, req1)
		if hits.Load() != 1 {
			t.Fatalf("first fetch hits=%d, want 1", hits.Load())
		}
		req2 := httptest.NewRequest("GET", "http://example.com/", nil)
		req2.Header.Set("X-Lang", "pl")
		rec2 := httptest.NewRecorder()
		p2.ServeHTTP(rec2, req2)
		if hits.Load() != 2 {
			t.Errorf("different header should miss cache, hits=%d want 2", hits.Load())
		}
		_ = rec1
		_ = rec2
	})
}

func TestCacheKeyTemplatePluginCookieIsolation(t *testing.T) {
	var hits atomic.Int32
	_, upstream := newCacheKeyTemplateTestServers(t, &hits)

	cfg := CreateConfig()
	cfg.CacheBackend = "memory"
	cfg.CacheTTL = "60s"
	cfg.CacheKeyTemplate = "k:${cookie:sess}"
	cfg.BlockPrivateIPs = false

	t.Run("different cookies different keys", func(t *testing.T) {
		hits.Store(0)
		p, err := New(context.Background(), upstream, cfg, "test")
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		req1 := httptest.NewRequest("GET", "http://example.com/", nil)
		req1.AddCookie(&http.Cookie{Name: "sess", Value: "a"})
		rec1 := httptest.NewRecorder()
		p.ServeHTTP(rec1, req1)
		if hits.Load() != 1 {
			t.Fatalf("first hits=%d want 1", hits.Load())
		}
		req2 := httptest.NewRequest("GET", "http://example.com/", nil)
		req2.AddCookie(&http.Cookie{Name: "sess", Value: "b"})
		rec2 := httptest.NewRecorder()
		p.ServeHTTP(rec2, req2)
		if hits.Load() != 2 {
			t.Errorf("different cookie should miss cache, hits=%d want 2", hits.Load())
		}
		_ = rec1
		_ = rec2
	})

	t.Run("same cookie hits cache", func(t *testing.T) {
		hits.Store(0)
		p, err := New(context.Background(), upstream, cfg, "test")
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		req1 := httptest.NewRequest("GET", "http://example.com/", nil)
		req1.AddCookie(&http.Cookie{Name: "sess", Value: "same"})
		rec1 := httptest.NewRecorder()
		p.ServeHTTP(rec1, req1)
		req2 := httptest.NewRequest("GET", "http://example.com/", nil)
		req2.AddCookie(&http.Cookie{Name: "sess", Value: "same"})
		rec2 := httptest.NewRecorder()
		p.ServeHTTP(rec2, req2)
		if hits.Load() != 1 {
			t.Errorf("same cookie should hit cache, hits=%d want 1", hits.Load())
		}
		if rec1.Body.String() != rec2.Body.String() {
			t.Errorf("cached mismatch %q vs %q", rec1.Body.String(), rec2.Body.String())
		}
	})
}

func TestCacheKeyTemplatePluginUnknownPassthrough(t *testing.T) {
	var hits atomic.Int32
	_, upstream := newCacheKeyTemplateTestServers(t, &hits)

	cfg := CreateConfig()
	cfg.CacheBackend = "memory"
	cfg.CacheTTL = "60s"
	cfg.CacheKeyTemplate = "k:${unknown}:literal"
	cfg.BlockPrivateIPs = false

	p, err := New(context.Background(), upstream, cfg, "test")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	req1 := httptest.NewRequest("GET", "http://example.com/", nil)
	req1.Header.Set("X-Lang", "en")
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "http://example.com/", nil)
	req2.Header.Set("X-Lang", "pl")
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)

	if hits.Load() != 1 {
		t.Errorf("unknown placeholder must be literal, so same URL with different headers should share cache; hits=%d want 1", hits.Load())
	}
	_ = rec1
	_ = rec2
}

func TestCacheKeyTemplatePluginEmptyFallsBackToDefault(t *testing.T) {
	var hits atomic.Int32
	_, upstream := newCacheKeyTemplateTestServers(t, &hits)

	cfg := CreateConfig()
	cfg.CacheBackend = "memory"
	cfg.CacheTTL = "60s"
	cfg.CacheKeyTemplate = ""
	cfg.BlockPrivateIPs = false

	p, err := New(context.Background(), upstream, cfg, "test")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Direct check: empty template must mean BuildCacheKey("", r) not used;
	// plugin must fall back to DefaultCacheKey (url-only). Verify via
	// mesi.DefaultCacheKey equivalence.
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	if got := mesi.DefaultCacheKey("http://example.com/frag"); got != "mesi:http://example.com/frag" {
		t.Fatalf("DefaultCacheKey contract broken: %q", got)
	}
	_ = r

	req1 := httptest.NewRequest("GET", "http://example.com/", nil)
	req1.Header.Set("X-Lang", "en")
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "http://example.com/", nil)
	req2.Header.Set("X-Lang", "pl")
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)

	if hits.Load() != 1 {
		t.Errorf("empty template must be URL-only, so different headers with same URL should share cache; hits=%d want 1", hits.Load())
	}
	body1 := rec1.Body.String()
	body2 := rec2.Body.String()
	if !strings.Contains(body1, "FRAG-1") || !strings.Contains(body2, "FRAG-1") {
		t.Errorf("expected both responses to contain cached FRAG-1, got %q and %q", body1, body2)
	}
}

func TestCacheKeyTemplatePluginUrlSubstitution(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("X-Lang", "en")
	key := mesi.BuildCacheKey("http://backend/frag?x=1", "pfx:${url}:sfx", r)
	if key != "pfx:http://backend/frag?x=1:sfx" {
		t.Errorf("expected 'pfx:http://backend/frag?x=1:sfx', got %q", key)
	}
}

func TestCacheKeyTemplateWithoutCacheBackendNoPanic(t *testing.T) {
	cfg := CreateConfig()
	cfg.CacheKeyTemplate = "k:${url}"
	// No cache backend — plugin must still start and ServeHTTP must not panic
	p, err := New(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>ok</html>"))
	}), cfg, "test")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
