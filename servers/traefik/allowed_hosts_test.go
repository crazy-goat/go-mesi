package traefik

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
)

func TestAllowedHostsDefaultEmpty(t *testing.T) {
	config := CreateConfig()
	if len(config.AllowedHosts) != 0 {
		t.Errorf("Expected empty AllowedHosts by default, got %v", config.AllowedHosts)
	}
}

func TestAllowedHostsPropagatedToPlugin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.AllowedHosts = []string{"backend.internal", "cdn.trusted.com"}

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if len(plugin.config.AllowedHosts) != 2 {
		t.Fatalf("Expected 2 AllowedHosts, got %d", len(plugin.config.AllowedHosts))
	}
	if plugin.config.AllowedHosts[0] != "backend.internal" {
		t.Errorf("Expected AllowedHosts[0]='backend.internal', got %q", plugin.config.AllowedHosts[0])
	}
	if plugin.config.AllowedHosts[1] != "cdn.trusted.com" {
		t.Errorf("Expected AllowedHosts[1]='cdn.trusted.com', got %q", plugin.config.AllowedHosts[1])
	}
}

func TestAllowedHostsServeHTTP(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>allowed hosts test</body></html>"))
	})

	config := CreateConfig()
	config.AllowedHosts = []string{"backend.internal"}

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestAllowedHostsAllowsConfiguredHost(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("FRAGMENT-BACKEND"))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("Failed to parse backend URL: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + backend.URL + `/fragment" /></body></html>`))
	})

	config := CreateConfig()
	config.AllowedHosts = []string{backendURL.Hostname()}
	config.BlockPrivateIPs = false

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "FRAGMENT-BACKEND") {
		t.Errorf("Expected allowed host include to resolve, got %q", body)
	}
}

func TestAllowedHostsBlocksUnconfiguredHost(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("FRAGMENT-BACKEND"))
	}))
	defer backend.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + backend.URL + `/fragment" /></body></html>`))
	})

	config := CreateConfig()
	config.AllowedHosts = []string{"other.trusted.com"}
	config.BlockPrivateIPs = false

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "FRAGMENT-BACKEND") {
		t.Errorf("Expected unconfigured host include to be blocked, got %q", body)
	}
}

// TestAllowedHostsSuffixInjectionGuardServeHTTP verifies through the real
// plugin path that a host merely ending with the allowed host (but not a real
// subdomain) is rejected. The host check fails in isURLSafe before any fetch,
// so no DNS resolution is required.
func TestAllowedHostsSuffixInjectionGuardServeHTTP(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="http://evil.com/fragment" /></body></html>`))
	})

	config := CreateConfig()
	config.AllowedHosts = []string{"example.com"}
	config.BlockPrivateIPs = false

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "evil.com") {
		t.Errorf("Host evil.com must not match allowed host example.com (suffix-injection guard), got %q", body)
	}
}

// TestAllowedHostsMultipleHostsBlockUnlistedServeHTTP verifies through the
// real plugin path that with a multi-entry allowlist, an unlisted host is
// blocked while the allowlist is honoured. The unlisted host is rejected by
// the host check before any fetch, so no DNS resolution is required.
func TestAllowedHostsMultipleHostsBlockUnlistedServeHTTP(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="http://c.internal/fragment" /></body></html>`))
	})

	config := CreateConfig()
	config.AllowedHosts = []string{"a.internal", "b.internal"}
	config.BlockPrivateIPs = false

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "c.internal") {
		t.Errorf("Unlisted host c.internal must be blocked by multi-entry allowlist, got %q", body)
	}
}

func TestAllowedHostsEmptyBackwardCompatible(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("FRAGMENT-BACKEND"))
	}))
	defer backend.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + backend.URL + `/fragment" /></body></html>`))
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
	if !strings.Contains(body, "FRAGMENT-BACKEND") {
		t.Errorf("Expected empty AllowedHosts to allow all hosts (backward compatible), got %q", body)
	}
}

// TestAllowedHostsSubdomainMatching verifies that a host which is a subdomain
// of an allowed host passes the URL-level SSRF allowlist (the effective SSRF
// control for the Traefik plugin, since dial-time IP blocking is stubbed under
// Yaegi). A custom client resolves the subdomain to the backend so the fetch
// succeeds when the allowlist permits it.
func TestAllowedHostsSubdomainMatching(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("FRAGMENT-BACKEND"))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("Failed to parse backend URL: %v", err)
	}
	backendHost, backendPort, err := net.SplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatalf("Failed to split backend host:port: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				if host == "sub.example.com" {
					address = net.JoinHostPort(backendHost, backendPort)
				}
				d := net.Dialer{}
				return d.DialContext(ctx, network, address)
			},
		},
	}

	input := `<html><body><esi:include src="http://sub.example.com/fragment" /></body></html>`
	config := mesi.EsiParserConfig{
		MaxDepth:           5,
		Timeout:            10 * time.Second,
		BlockPrivateIPs:    false,
		AllowedHosts:       []string{"example.com"},
		HTTPClient:         client,
		DefaultUrl:         "http://example.com/",
	}

	out := mesi.MESIParse(input, config)
	if !strings.Contains(out, "FRAGMENT-BACKEND") {
		t.Errorf("Expected subdomain of allowed host to resolve, got %q", out)
	}
}

// TestAllowedHostsSuffixInjectionGuard verifies that a host merely ending with
// the allowed host (but not a real subdomain, e.g. evil.com vs example.com, or
// notexample.com vs example.com) is rejected by the allowlist.
func TestAllowedHostsSuffixInjectionGuard(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("FRAGMENT-BACKEND"))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("Failed to parse backend URL: %v", err)
	}
	backendHost, backendPort, err := net.SplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatalf("Failed to split backend host:port: %v", err)
	}

	resolveToBackend := func(host string) (string, bool) {
		if host == "evil.com" || host == "notexample.com" {
			return net.JoinHostPort(backendHost, backendPort), true
		}
		return "", false
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				if mapped, ok := resolveToBackend(host); ok {
					address = mapped
				}
				d := net.Dialer{}
				return d.DialContext(ctx, network, address)
			},
		},
	}

	cases := []string{"evil.com", "notexample.com"}
	for _, host := range cases {
		input := `<html><body><esi:include src="http://` + host + `/fragment" /></body></html>`
		config := mesi.EsiParserConfig{
			MaxDepth:        5,
			Timeout:         10 * time.Second,
			BlockPrivateIPs: false,
			AllowedHosts:    []string{"example.com"},
			HTTPClient:      client,
			DefaultUrl:      "http://example.com/",
		}

		out := mesi.MESIParse(input, config)
		if strings.Contains(out, "FRAGMENT-BACKEND") {
			t.Errorf("Host %q must not match allowed host example.com (suffix-injection guard), got %q", host, out)
		}
	}
}
