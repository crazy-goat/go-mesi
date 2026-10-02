package caddy

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// bodyLimitHandler serves an HTML page that contains one ESI include of a
// fragment, with or without Content-Length, so a test can see whether the
// tag was processed (fragment text appears) or passed through (tag stays).
func bodyLimitHandler(page string, withLength bool) caddyhttp.Handler {
	return caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		if withLength {
			w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		}
		w.WriteHeader(http.StatusOK)
		// Two writes, so an unknown-length body reaches the limit in the
		// second one, with the first part already buffered.
		half := len(page) / 2
		_, _ = w.Write([]byte(page[:half]))
		_, _ = w.Write([]byte(page[half:]))
		return nil
	})
}

func newBodyLimitModule(t *testing.T, maxBody int64, mode string) (*MesiMiddleware, *observer.ObservedLogs) {
	t.Helper()
	blockPrivateIPs := false
	m := &MesiMiddleware{BlockPrivateIPs: &blockPrivateIPs, MaxBodySize: maxBody, OnOversize: mode}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	core, logs := observer.New(zap.WarnLevel)
	m.logger = zap.New(core)
	return m, logs
}

func bodyLimitPage(t *testing.T) (page string, fragment string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("FragmentText"))
	}))
	t.Cleanup(srv.Close)
	return `<html><body><esi:include src="` + srv.URL + `/f" />` + strings.Repeat("x", 300) + `</body></html>`, "FragmentText"
}

func TestBodyLimitUnderLimitIsProcessed(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		page, fragment := bodyLimitPage(t)
		m, logs := newBodyLimitModule(t, int64(len(page)), "error") // exactly at the limit
		rec := httptest.NewRecorder()
		if err := m.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil), bodyLimitHandler(page, withLength)); err != nil {
			t.Fatalf("ServeHTTP: %v", err)
		}
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), fragment) {
			t.Errorf("length known=%v: want ESI processed, got %d %.80q", withLength, rec.Code, rec.Body.String())
		}
		if logs.Len() != 0 {
			t.Errorf("length known=%v: nothing may be logged under the limit", withLength)
		}
	}
}

func TestBodyLimitPassSendsBodyUnchanged(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		page, fragment := bodyLimitPage(t)
		m, logs := newBodyLimitModule(t, int64(len(page))-1, "") // default mode is pass
		rec := httptest.NewRecorder()
		if err := m.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil), bodyLimitHandler(page, withLength)); err != nil {
			t.Fatalf("ServeHTTP: %v", err)
		}
		if rec.Code != http.StatusOK || rec.Body.String() != page {
			t.Errorf("length known=%v: want the page unchanged, got %d %.80q", withLength, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), fragment) {
			t.Errorf("length known=%v: the include must not be processed", withLength)
		}
		if logs.FilterLevelExact(zap.WarnLevel).Len() != 1 {
			t.Errorf("length known=%v: want one warning, got %d log entries", withLength, logs.Len())
		}
	}
}

func TestBodyLimitErrorAnswers502(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		page, _ := bodyLimitPage(t)
		m, logs := newBodyLimitModule(t, int64(len(page))-1, "error")
		rec := httptest.NewRecorder()
		if err := m.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil), bodyLimitHandler(page, withLength)); err != nil {
			t.Fatalf("ServeHTTP: %v", err)
		}
		if rec.Code != http.StatusBadGateway {
			t.Errorf("length known=%v: want 502, got %d", withLength, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "esi:include") {
			t.Errorf("length known=%v: the page must not be sent", withLength)
		}
		if logs.FilterLevelExact(zap.ErrorLevel).Len() != 1 {
			t.Errorf("length known=%v: want one error log, got %d entries", withLength, logs.Len())
		}
	}
}

func TestBodyLimitDefaultIsUnlimited(t *testing.T) {
	page, fragment := bodyLimitPage(t)
	m, _ := newBodyLimitModule(t, 0, "error")
	rec := httptest.NewRecorder()
	if err := m.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil), bodyLimitHandler(page, false)); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	if !strings.Contains(rec.Body.String(), fragment) {
		t.Error("max_body_size 0 must keep the unlimited behaviour")
	}
}

func TestBodyLimitProvisionValidation(t *testing.T) {
	if err := (&MesiMiddleware{MaxBodySize: -1}).Provision(caddy.Context{}); err == nil {
		t.Error("a negative max_body_size must fail Provision")
	}
	if err := (&MesiMiddleware{OnOversize: "drop"}).Provision(caddy.Context{}); err == nil {
		t.Error("an unknown on_oversize must fail Provision")
	}
}

func TestBodyLimitCaddyfile(t *testing.T) {
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(caddyfile.NewTestDispenser("mesi {\n max_body_size 1048576\n on_oversize error\n}")); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if m.MaxBodySize != 1048576 || m.OnOversize != "error" {
		t.Errorf("got max_body_size=%d on_oversize=%q", m.MaxBodySize, m.OnOversize)
	}

	for _, bad := range []string{
		"max_body_size", "max_body_size -1", "max_body_size 10m", "max_body_size abc",
		"max_body_size 99999999999999999999", "on_oversize", "on_oversize drop",
	} {
		if err := (&MesiMiddleware{}).UnmarshalCaddyfile(caddyfile.NewTestDispenser("mesi {\n " + bad + "\n}")); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
