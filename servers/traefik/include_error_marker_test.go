package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIncludeErrorMarkerDefaultEmpty(t *testing.T) {
	config := CreateConfig()
	if config.IncludeErrorMarker != "" {
		t.Errorf("Expected empty IncludeErrorMarker by default, got %q", config.IncludeErrorMarker)
	}
}

func TestIncludeErrorMarkerAcceptedInConfig(t *testing.T) {
	config := CreateConfig()
	config.IncludeErrorMarker = "<!-- esi error -->"
	if config.IncludeErrorMarker != "<!-- esi error -->" {
		t.Errorf("Expected IncludeErrorMarker '<!-- esi error -->', got %q", config.IncludeErrorMarker)
	}
}

func TestIncludeErrorMarkerPropagatedToPlugin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.IncludeErrorMarker = "<!-- ESI_FAIL -->"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.config.IncludeErrorMarker != "<!-- ESI_FAIL -->" {
		t.Errorf("Expected IncludeErrorMarker '<!-- ESI_FAIL -->', got %q", plugin.config.IncludeErrorMarker)
	}
}

func TestIncludeErrorMarkerServeHTTP(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + backend.URL + `/missing" /></body></html>`))
	})

	config := CreateConfig()
	config.IncludeErrorMarker = "<!-- esi error -->"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "<!-- esi error -->") {
		t.Errorf("Expected IncludeErrorMarker in response body, got %q", body)
	}
}

func TestIncludeErrorMarkerNotRenderedOnSuccess(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body>no esi tags</body></html>`))
	})

	config := CreateConfig()
	config.IncludeErrorMarker = "<!-- esi error -->"

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<!-- esi error -->") {
		t.Errorf("IncludeErrorMarker should not appear in successful response, got %q", body)
	}
}

func TestIncludeErrorMarkerEmptyString(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + backend.URL + `/missing" /></body></html>`))
	})

	config := CreateConfig()
	config.IncludeErrorMarker = ""

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<!--") {
		t.Errorf("Empty IncludeErrorMarker should not inject HTML comment, got %q", body)
	}
}
