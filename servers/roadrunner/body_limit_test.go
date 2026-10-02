package roadrunner

import (
	"bytes"
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

func bodyLimitPlugin(t *testing.T, limit int64, mode string) *Plugin {
	t.Helper()
	block := false
	p := &Plugin{config: &Config{MaxBodySize: limit, OnOversize: mode, BlockPrivateIPs: &block}}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return p
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
		p := bodyLimitPlugin(t, int64(len(page)), "error") // exactly at the limit
		rec := httptest.NewRecorder()
		p.Middleware(up).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
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
		p := bodyLimitPlugin(t, int64(len(page))-1, "") // default mode is pass
		rec := httptest.NewRecorder()
		p.Middleware(up).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != page {
			t.Errorf("length known=%v: want the page unchanged, got %d %.60q", withLength, rec.Code, rec.Body.String())
		}
		if !strings.Contains(logs.String(), "on_oversize pass") {
			t.Errorf("length known=%v: want a warning, got %q", withLength, logs.String())
		}
	}
}

func TestBodyLimitErrorAnswers502(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		logs := captureLog(t)
		up, page := bodyLimitUpstream(t, withLength)
		p := bodyLimitPlugin(t, int64(len(page))-1, "error")
		rec := httptest.NewRecorder()
		p.Middleware(up).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
		if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "esi:include") {
			t.Errorf("length known=%v: want 502 without the page, got %d %.60q", withLength, rec.Code, rec.Body.String())
		}
		if !strings.Contains(logs.String(), "on_oversize error") {
			t.Errorf("length known=%v: want an error log, got %q", withLength, logs.String())
		}
	}
}

func TestBodyLimitUnlimitedByDefaultAndHEAD(t *testing.T) {
	up, _ := bodyLimitUpstream(t, true)
	rec := httptest.NewRecorder()
	bodyLimitPlugin(t, 0, "error").Middleware(up).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	if !strings.Contains(rec.Body.String(), "FragmentText") {
		t.Error("0 means unlimited")
	}

	rec = httptest.NewRecorder()
	bodyLimitPlugin(t, 10, "error").Middleware(up).ServeHTTP(rec, httptest.NewRequest("HEAD", "http://example.com/", nil))
	if rec.Code == http.StatusBadGateway {
		t.Error("HEAD must not be limited")
	}
}

func TestInitBodyLimitValidation(t *testing.T) {
	if err := (&Plugin{config: &Config{MaxBodySize: -1}}).Init(); err == nil {
		t.Error("a negative max_body_size must fail Init")
	}
	if err := (&Plugin{config: &Config{OnOversize: "drop"}}).Init(); err == nil {
		t.Error("an unknown on_oversize must fail Init")
	}
	for _, mode := range []string{"", "pass", "error"} {
		if err := (&Plugin{config: &Config{MaxBodySize: 1, OnOversize: mode}}).Init(); err != nil {
			t.Errorf("on_oversize %q must be accepted: %v", mode, err)
		}
	}
}
