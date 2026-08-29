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
		_, _ = fmt.Fprintf(w, "FRAG-%d", n)
	}))
	t.Cleanup(fragment.Close)
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<html><body><esi:include src="%s/frag" /></body></html>`, fragment.URL)
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

	t.Run("same header hits cache", func(t *testing.T) {
		hits.Store(0)
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
		req1 := httptest.NewRequest("GET", "http://example.com/", nil)
		req1.Header.Set("X-Lang", "en")
		rec1 := httptest.NewRecorder()
		p2.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("first fetch status %d", rec1.Code)
		}
		if hits.Load() != 1 {
			t.Fatalf("first fetch hits=%d, want 1", hits.Load())
		}
		req2 := httptest.NewRequest("GET", "http://example.com/", nil)
		req2.Header.Set("X-Lang", "pl")
		rec2 := httptest.NewRecorder()
		p2.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("second fetch status %d", rec2.Code)
		}
		if hits.Load() != 2 {
			t.Errorf("different header should miss cache, hits=%d want 2", hits.Load())
		}
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
		if rec1.Code != http.StatusOK {
			t.Fatalf("first fetch status %d", rec1.Code)
		}
		if hits.Load() != 1 {
			t.Fatalf("first hits=%d want 1", hits.Load())
		}
		req2 := httptest.NewRequest("GET", "http://example.com/", nil)
		req2.AddCookie(&http.Cookie{Name: "sess", Value: "b"})
		rec2 := httptest.NewRecorder()
		p.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("second fetch status %d", rec2.Code)
		}
		if hits.Load() != 2 {
			t.Errorf("different cookie should miss cache, hits=%d want 2", hits.Load())
		}
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
	if rec1.Body.String() != rec2.Body.String() {
		t.Errorf("expected same cached body, got %q vs %q", rec1.Body.String(), rec2.Body.String())
	}
}

func TestCacheKeyTemplatePluginEmptyFallsBackToDefault(t *testing.T) {
	cfg := CreateConfig()
	cfg.CacheBackend = "memory"
	cfg.CacheTTL = "60s"
	cfg.CacheKeyTemplate = ""
	cfg.BlockPrivateIPs = false

	r := httptest.NewRequest("GET", "http://example.com/", nil)
	if got := mesi.DefaultCacheKey("http://example.com/frag"); got != "mesi:http://example.com/frag" {
		t.Fatalf("DefaultCacheKey contract broken: %q", got)
	}
	_ = r

	t.Run("same URL different headers share cache", func(t *testing.T) {
		var hits atomic.Int32
		frag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := hits.Add(1)
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, "FRAG-%d", n)
		}))
		defer frag.Close()
		upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `<html><body><esi:include src="%s/frag" /></body></html>`, frag.URL)
		})
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
			t.Errorf("empty template must be URL-only, so different headers with same URL should share cache; hits=%d want 1", hits.Load())
		}
		body1 := rec1.Body.String()
		body2 := rec2.Body.String()
		if !strings.Contains(body1, "FRAG-1") || !strings.Contains(body2, "FRAG-1") {
			t.Errorf("expected both responses to contain cached FRAG-1, got %q and %q", body1, body2)
		}
	})

	t.Run("different URLs must not share cache", func(t *testing.T) {
		var hits atomic.Int32
		fragA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := hits.Add(1)
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, "A-%d", n)
		}))
		defer fragA.Close()
		fragB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := hits.Add(1)
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, "B-%d", n)
		}))
		defer fragB.Close()

		comboHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			if r.Header.Get("X-Target") == "B" {
				_, _ = fmt.Fprintf(w, `<html><body><esi:include src="%s/frag" /></body></html>`, fragB.URL)
			} else {
				_, _ = fmt.Fprintf(w, `<html><body><esi:include src="%s/frag" /></body></html>`, fragA.URL)
			}
		})

		p2, err := New(context.Background(), comboHandler, cfg, "test")
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}
		reqA := httptest.NewRequest("GET", "http://example.com/a", nil)
		reqA.Header.Set("X-Target", "A")
		reqA.Header.Set("X-Lang", "en")
		recA := httptest.NewRecorder()
		p2.ServeHTTP(recA, reqA)
		if hits.Load() != 1 {
			t.Fatalf("url1 hits=%d want 1", hits.Load())
		}
		if !strings.Contains(recA.Body.String(), "A-1") {
			t.Fatalf("url1 expected A-1, got %q", recA.Body.String())
		}
		reqB := httptest.NewRequest("GET", "http://example.com/b", nil)
		reqB.Header.Set("X-Target", "B")
		reqB.Header.Set("X-Lang", "pl")
		recB := httptest.NewRecorder()
		p2.ServeHTTP(recB, reqB)
		if hits.Load() != 2 {
			t.Errorf("empty template with different URLs must hit backend again; hits=%d want 2 (cross-URL cache poisoning if guard deleted)", hits.Load())
		}
		if strings.Contains(recB.Body.String(), "A-1") {
			t.Errorf("url2 must not return url1 body (poisoned cache), got %q", recB.Body.String())
		}
		if !strings.Contains(recB.Body.String(), "B-2") {
			t.Errorf("url2 expected B-2, got %q", recB.Body.String())
		}
	})
}

func TestCacheKeyTemplatePluginUrlDistinctKeys(t *testing.T) {
	var hits atomic.Int32
	fragA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, "A-%d", n)
	}))
	defer fragA.Close()
	fragB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, "B-%d", n)
	}))
	defer fragB.Close()

	cfg := CreateConfig()
	cfg.CacheBackend = "memory"
	cfg.CacheTTL = "60s"
	cfg.CacheKeyTemplate = "pfx:${url}:sfx"
	cfg.BlockPrivateIPs = false

	comboHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		if r.Header.Get("X-Target") == "B" {
			_, _ = fmt.Fprintf(w, `<html><body><esi:include src="%s/frag" /></body></html>`, fragB.URL)
		} else {
			_, _ = fmt.Fprintf(w, `<html><body><esi:include src="%s/frag" /></body></html>`, fragA.URL)
		}
	})

	p, err := New(context.Background(), comboHandler, cfg, "test")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	reqA := httptest.NewRequest("GET", "http://example.com/a", nil)
	reqA.Header.Set("X-Target", "A")
	recA := httptest.NewRecorder()
	p.ServeHTTP(recA, reqA)
	if hits.Load() != 1 {
		t.Fatalf("url1 hits=%d want 1", hits.Load())
	}

	reqB := httptest.NewRequest("GET", "http://example.com/b", nil)
	reqB.Header.Set("X-Target", "B")
	recB := httptest.NewRecorder()
	p.ServeHTTP(recB, reqB)
	if hits.Load() != 2 {
		t.Errorf("${url} template: distinct URLs must be distinct keys; hits=%d want 2", hits.Load())
	}
	if strings.Contains(recB.Body.String(), "A-1") {
		t.Errorf("url2 must not return url1 body, got %q", recB.Body.String())
	}
	reqA2 := httptest.NewRequest("GET", "http://example.com/a", nil)
	reqA2.Header.Set("X-Target", "A")
	recA2 := httptest.NewRecorder()
	p.ServeHTTP(recA2, reqA2)
	if hits.Load() != 2 {
		t.Errorf("same url should hit cache; hits=%d want 2", hits.Load())
	}
	if !strings.Contains(recA2.Body.String(), "A-1") {
		t.Errorf("cached url1 expected A-1, got %q", recA2.Body.String())
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
	p, err := New(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>ok</html>"))
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
