package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crazy-goat/go-mesi/mesi"
)

func TestNewExplicitZeroKeepsPassthrough(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cfg := &Config{MaxDepth: intPtr(0)}
	p, err := New(context.Background(), handler, cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plugin := p.(*ResponsePlugin)
	if plugin.config.MaxDepth == nil || *plugin.config.MaxDepth != 0 {
		t.Errorf("expected explicit 0 to stay 0, got %v", plugin.config.MaxDepth)
	}
	if plugin.maxDepth() != 0 {
		t.Errorf("expected maxDepth()=0, got %d", plugin.maxDepth())
	}
}

func TestNewNilMaxDepthDefaultsToFive(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	p, err := New(context.Background(), handler, &Config{}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plugin := p.(*ResponsePlugin)
	if plugin.config.MaxDepth == nil || *plugin.config.MaxDepth != 5 {
		t.Errorf("expected unset MaxDepth to become 5, got %v", plugin.config.MaxDepth)
	}
}

func TestNewRejectsNegativeMaxDepth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	_, err := New(context.Background(), handler, &Config{MaxDepth: intPtr(-1)}, "test")
	if err == nil {
		t.Fatal("expected error for negative maxDepth")
	}
	if !strings.Contains(err.Error(), "maxDepth") {
		t.Errorf("expected maxDepth in error, got %v", err)
	}
}

func TestNewRejectsMaxDepthAboveCap(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	_, err := New(context.Background(), handler, &Config{MaxDepth: intPtr(int(mesi.MaxMaxDepth) + 1)}, "test")
	if err == nil {
		t.Fatal("expected error for maxDepth above MaxMaxDepth")
	}
	if !strings.Contains(err.Error(), "maxDepth") {
		t.Errorf("expected maxDepth in error, got %v", err)
	}
}

func TestServeHTTPMaxDepthZeroPassthrough(t *testing.T) {
	fragmentCalls := 0
	frag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fragmentCalls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment"))
	}))
	defer frag.Close()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + frag.URL + `/frag" /></body></html>`))
	})

	p, err := New(context.Background(), next, &Config{MaxDepth: intPtr(0)}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))

	if fragmentCalls != 0 {
		t.Errorf("expected 0 fragment fetches at maxDepth 0, got %d", fragmentCalls)
	}
	if rec.Body.String() != "<html><body></body></html>" {
		t.Errorf("expected passthrough empty include, got %q", rec.Body.String())
	}
}
