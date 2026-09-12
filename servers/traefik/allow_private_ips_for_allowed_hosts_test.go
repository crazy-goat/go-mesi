package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAllowPrivateIPsForAllowedHostsDefaultFalse(t *testing.T) {
	config := CreateConfig()
	if config.AllowPrivateIPsForAllowedHosts {
		t.Errorf("Expected AllowPrivateIPsForAllowedHosts false by default, got %v", config.AllowPrivateIPsForAllowedHosts)
	}
}

func TestAllowPrivateIPsForAllowedHostsCanBeEnabled(t *testing.T) {
	config := CreateConfig()
	config.AllowPrivateIPsForAllowedHosts = true
	if !config.AllowPrivateIPsForAllowedHosts {
		t.Errorf("Expected AllowPrivateIPsForAllowedHosts true after explicit enable, got %v", config.AllowPrivateIPsForAllowedHosts)
	}
}

func TestAllowPrivateIPsForAllowedHostsPropagatedToPlugin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	config := CreateConfig()
	config.AllowPrivateIPsForAllowedHosts = true

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	plugin := p.(*ResponsePlugin)
	if !plugin.config.AllowPrivateIPsForAllowedHosts {
		t.Errorf("Expected plugin AllowPrivateIPsForAllowedHosts true, got %v", plugin.config.AllowPrivateIPsForAllowedHosts)
	}
}

// newBypassTestPlugin builds a plugin wired to an upstream page containing an
// <esi:include> to the given backend URL, with per-call tunable SSRF options.
// BlockPrivateIPs stays at its default true unless disabled explicitly.
func newBypassTestPlugin(t *testing.T, backendURL string, allowedHosts []string, bypass, sharedClient bool) http.Handler {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><esi:include src="` + backendURL + `" /></body></html>`))
	})

	config := CreateConfig()
	config.AllowedHosts = allowedHosts
	config.AllowPrivateIPsForAllowedHosts = bypass
	config.SharedHTTPClient = sharedClient

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	return p
}

// newLoopbackBackend returns a plain fragment server on 127.0.0.1 (a
// private/reserved address) that answers with PRIVATE-OK.
func newLoopbackBackend(t *testing.T) *httptest.Server {
	t.Helper()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PRIVATE-OK"))
	}))
	t.Cleanup(backend.Close)
	return backend
}

func serveByPlugin(t *testing.T, p http.Handler) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	return rec
}

// assertBlocked verifies the include did NOT resolve: no fragment content,
// and the raw <esi:include> tag was still processed away (vacuous-pass guard:
// an unprocessed page would contain the raw tag and pass a naive absence
// check).
func assertBlocked(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, "PRIVATE-OK") {
		t.Errorf("Expected include to be blocked, got body: %s", body)
	}
	if strings.Contains(body, "esi:include") {
		t.Errorf("Expected the <esi:include> tag to be processed away, got body: %s", body)
	}
}

func TestAllowPrivateIPsForAllowedHostsBypassAllowsListedPrivateHost(t *testing.T) {
	// Real bypass at the Go level: BlockPrivateIPs stays true (default), the
	// loopback host is listed in AllowedHosts, and the per-host bypass grants
	// a plain client for this fetch (mesi/fetch.go fetchClientForURL).
	backend := newLoopbackBackend(t)

	p := newBypassTestPlugin(t, backend.URL, []string{"127.0.0.1"}, true, false)
	rec := serveByPlugin(t, p)

	if !strings.Contains(rec.Body.String(), "PRIVATE-OK") {
		t.Errorf("Expected private include to be fetched with the bypass enabled, got body: %s", rec.Body.String())
	}
}

func TestAllowPrivateIPsForAllowedHostsDisabledBlocks(t *testing.T) {
	backend := newLoopbackBackend(t)

	p := newBypassTestPlugin(t, backend.URL, []string{"127.0.0.1"}, false, false)
	rec := serveByPlugin(t, p)

	assertBlocked(t, rec.Body.String())
}

func TestAllowPrivateIPsForAllowedHostsUnlistedHostStillBlocked(t *testing.T) {
	// The bypass only covers hosts present in AllowedHosts: with the flag on
	// but the private host outside the whitelist, the pre-dial whitelist
	// check rejects the include regardless.
	backend := newLoopbackBackend(t)

	p := newBypassTestPlugin(t, backend.URL, []string{"example.com"}, true, false)
	rec := serveByPlugin(t, p)

	assertBlocked(t, rec.Body.String())
}

func TestAllowPrivateIPsForAllowedHostsEmptyAllowlistNoBypass(t *testing.T) {
	// Fail-closed: with an empty allowlist no hostname can match, so the
	// bypass branch is never taken even with the flag on.
	backend := newLoopbackBackend(t)

	p := newBypassTestPlugin(t, backend.URL, []string{}, true, false)
	rec := serveByPlugin(t, p)

	assertBlocked(t, rec.Body.String())
}

func TestAllowPrivateIPsForAllowedHostsSharedClientStillBlocks(t *testing.T) {
	// Documented limitation (mirrors RoadRunner #209): with sharedHTTPClient
	// the shared transport bakes BlockPrivateIPs at startup, so the bypass is
	// never consulted for shared-client fetches (shared branch wins first in
	// fetchClientForURL).
	backend := newLoopbackBackend(t)

	p := newBypassTestPlugin(t, backend.URL, []string{"127.0.0.1"}, true, true)
	rec := serveByPlugin(t, p)

	assertBlocked(t, rec.Body.String())
}
