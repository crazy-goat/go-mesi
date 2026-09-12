package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlockPrivateIPsDefaultTrue(t *testing.T) {
	config := CreateConfig()
	if !config.BlockPrivateIPs {
		t.Errorf("Expected BlockPrivateIPs true by default, got %v", config.BlockPrivateIPs)
	}
}

func TestBlockPrivateIPsCanBeDisabled(t *testing.T) {
	config := CreateConfig()
	config.BlockPrivateIPs = false
	if config.BlockPrivateIPs {
		t.Errorf("Expected BlockPrivateIPs false after explicit disable, got %v", config.BlockPrivateIPs)
	}
}

func TestBlockPrivateIPsPropagatedToPlugin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.BlockPrivateIPs = false

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if plugin.config.BlockPrivateIPs {
		t.Errorf("Expected plugin BlockPrivateIPs false, got %v", plugin.config.BlockPrivateIPs)
	}
}

func TestBlockPrivateIPsEnabledPropagatedToPlugin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.BlockPrivateIPs = true

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if !plugin.config.BlockPrivateIPs {
		t.Errorf("Expected plugin BlockPrivateIPs true, got %v", plugin.config.BlockPrivateIPs)
	}
}

func TestBlockPrivateIPsDisabledFetchesPrivateInclude(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PRIVATE-OK"))
	}))
	defer backend.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><esi:include src="` + backend.URL + `" /></body></html>`))
	})

	config := CreateConfig()
	config.BlockPrivateIPs = false

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "PRIVATE-OK") {
		t.Errorf("Expected private include to be fetched with BlockPrivateIPs=false, got %q", body)
	}
}
