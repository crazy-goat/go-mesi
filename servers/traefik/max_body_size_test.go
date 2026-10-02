package traefik

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// bodyLimitUpstream serves an HTML page with one ESI include, in two writes,
// with or without Content-Length.
func bodyLimitUpstream(t *testing.T, withLength bool) (http.Handler, string) {
	t.Helper()
	frag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("FragmentText"))
	}))
	t.Cleanup(frag.Close)
	page := `<html><body><esi:include src="` + frag.URL + `/f" />` + strings.Repeat("x", 300) + `</body></html>`
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if withLength {
			w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		}
		half := len(page) / 2
		_, _ = w.Write([]byte(page[:half]))
		_, _ = w.Write([]byte(page[half:]))
	}), page
}

func newBodyLimitPlugin(t *testing.T, up http.Handler, limit int64, mode string) http.Handler {
	t.Helper()
	h, err := New(context.Background(), up, &Config{MaxBodySize: limit, OnOversize: mode}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestBodyLimitUnderLimitIsProcessed(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		logs := captureLog(t)
		up, page := bodyLimitUpstream(t, withLength)
		rec := httptest.NewRecorder()
		newBodyLimitPlugin(t, up, int64(len(page)), "error").ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
		// BlockPrivateIPs is false in a bare Config, so the local fragment is fetched.
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "FragmentText") {
			t.Errorf("length known=%v: want ESI processed, got %d", withLength, rec.Code)
		}
		if logs.Len() != 0 {
			t.Errorf("length known=%v: nothing may be logged, got %q", withLength, logs.String())
		}
	}
}

func TestBodyLimitPassSendsBodyUnchanged(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		logs := captureLog(t)
		up, page := bodyLimitUpstream(t, withLength)
		rec := httptest.NewRecorder()
		newBodyLimitPlugin(t, up, int64(len(page))-1, "").ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != page {
			t.Errorf("length known=%v: want the page unchanged, got %d %.60q", withLength, rec.Code, rec.Body.String())
		}
		if !strings.Contains(logs.String(), "onOversize pass") {
			t.Errorf("length known=%v: want a warning, got %q", withLength, logs.String())
		}
	}
}

func TestBodyLimitErrorAnswers502(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		logs := captureLog(t)
		up, page := bodyLimitUpstream(t, withLength)
		rec := httptest.NewRecorder()
		newBodyLimitPlugin(t, up, int64(len(page))-1, "error").ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
		if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "esi:include") {
			t.Errorf("length known=%v: want 502 without the page, got %d %.60q", withLength, rec.Code, rec.Body.String())
		}
		if !strings.Contains(logs.String(), "onOversize error") {
			t.Errorf("length known=%v: want an error log, got %q", withLength, logs.String())
		}
	}
}

func TestBodyLimitUnlimitedByDefaultAndHEAD(t *testing.T) {
	up, _ := bodyLimitUpstream(t, true)
	rec := httptest.NewRecorder()
	newBodyLimitPlugin(t, up, 0, "error").ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	if !strings.Contains(rec.Body.String(), "FragmentText") {
		t.Error("0 means unlimited")
	}

	rec = httptest.NewRecorder()
	newBodyLimitPlugin(t, up, 10, "error").ServeHTTP(rec, httptest.NewRequest("HEAD", "http://example.com/", nil))
	if rec.Code == http.StatusBadGateway {
		t.Error("HEAD must not be limited")
	}
}

func TestNewBodyLimitValidation(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if _, err := New(context.Background(), handler, &Config{MaxBodySize: -1}, "test"); err == nil || !strings.Contains(err.Error(), "maxBodySize") {
		t.Errorf("a negative maxBodySize must fail New and name the option, got %v", err)
	}
	if _, err := New(context.Background(), handler, &Config{OnOversize: "drop"}, "test"); err == nil || !strings.Contains(err.Error(), "onOversize") {
		t.Errorf("an unknown onOversize must fail New and name the option, got %v", err)
	}
	for _, mode := range []string{"", "pass", "error"} {
		if _, err := New(context.Background(), handler, &Config{MaxBodySize: 1, OnOversize: mode}, "test"); err != nil {
			t.Errorf("onOversize %q must be accepted: %v", mode, err)
		}
	}
}
