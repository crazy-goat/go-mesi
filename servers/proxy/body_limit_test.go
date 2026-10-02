package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
)

const bodyLimitFragment = "FragmentText"

// bodyLimitProxy starts a backend that serves an HTML page with one ESI
// include, with or without Content-Length, and a proxy in front of it.
func bodyLimitProxy(t *testing.T, withLength bool, maxBody int64, mode string, mutate func(*mesi.EsiParserConfig)) (*Proxy, string) {
	t.Helper()
	frag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyLimitFragment))
	}))
	t.Cleanup(frag.Close)
	page := `<html><body><esi:include src="` + frag.URL + `/f" />` + strings.Repeat("x", 300) + `</body></html>`

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Edge-control", "dca=esi")
		if withLength {
			w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		}
		w.WriteHeader(http.StatusOK)
		half := len(page) / 2
		_, _ = w.Write([]byte(page[:half]))
		w.(http.Flusher).Flush() // two reads at the proxy
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(page[half:]))
	}))
	t.Cleanup(backend.Close)

	config := mesi.CreateDefaultConfig()
	config.Timeout = 5 * time.Second
	config.BlockPrivateIPs = false
	if mutate != nil {
		mutate(&config)
	}
	p, err := NewProxy(backend.URL, config)
	if err != nil {
		t.Fatal(err)
	}
	if maxBody == -1 {
		maxBody = int64(len(page)) - 1 // one byte over
	}
	if maxBody == -2 {
		maxBody = int64(len(page)) // exactly at the limit
	}
	if err := p.SetBodyLimit(maxBody, mode); err != nil {
		t.Fatal(err)
	}
	return p, page
}

func serveBodyLimit(p *Proxy, method string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/page", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestBodyLimit_UnderLimitIsProcessed(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		logs := captureLog(t)
		p, _ := bodyLimitProxy(t, withLength, -2, "error", nil)
		rec := serveBodyLimit(p, "GET")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), bodyLimitFragment) {
			t.Errorf("length known=%v: want ESI processed, got %d", withLength, rec.Code)
		}
		if logs.Len() != 0 {
			t.Errorf("length known=%v: nothing may be logged, got %q", withLength, logs.String())
		}
	}
}

func TestBodyLimit_PassSendsBodyUnchanged(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		logs := captureLog(t)
		p, page := bodyLimitProxy(t, withLength, -1, "pass", nil)
		rec := serveBodyLimit(p, "GET")
		if rec.Code != http.StatusOK || rec.Body.String() != page {
			t.Errorf("length known=%v: want the page unchanged, got %d %.60q", withLength, rec.Code, rec.Body.String())
		}
		if !strings.Contains(logs.String(), "on-oversize pass") {
			t.Errorf("length known=%v: want a warning, got %q", withLength, logs.String())
		}
	}
}

func TestBodyLimit_ErrorAnswers502(t *testing.T) {
	for _, withLength := range []bool{true, false} {
		logs := captureLog(t)
		p, _ := bodyLimitProxy(t, withLength, -1, "error", nil)
		rec := serveBodyLimit(p, "GET")
		if rec.Code != http.StatusBadGateway {
			t.Errorf("length known=%v: want 502, got %d", withLength, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "esi:include") || rec.Header().Get("Edge-control") != "" {
			t.Errorf("length known=%v: the upstream page and headers must not be sent", withLength)
		}
		if !strings.Contains(logs.String(), "on-oversize error") {
			t.Errorf("length known=%v: want an error log, got %q", withLength, logs.String())
		}
	}
}

func TestBodyLimit_DefaultUnlimitedAndHEAD(t *testing.T) {
	p, _ := bodyLimitProxy(t, true, 0, "error", nil)
	if rec := serveBodyLimit(p, "GET"); !strings.Contains(rec.Body.String(), bodyLimitFragment) {
		t.Error("0 means unlimited")
	}

	p, _ = bodyLimitProxy(t, true, 10, "error", nil)
	if rec := serveBodyLimit(p, "HEAD"); rec.Code == http.StatusBadGateway {
		t.Error("HEAD must not be limited")
	}
}

func TestBodyLimit_ParseOnHeaderWithoutHeaderIsNotLimited(t *testing.T) {
	// ParseOnHeader needs Edge-control: dca=esi; the backend sets it, so the
	// page is limited, and a backend without it is passed through as is.
	p, _ := bodyLimitProxy(t, true, 10, "error", func(c *mesi.EsiParserConfig) { c.ParseOnHeader = true })
	if rec := serveBodyLimit(p, "GET"); rec.Code != http.StatusBadGateway {
		t.Errorf("a page that would be parsed must be limited, got %d", rec.Code)
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer plain.Close()
	config := mesi.CreateDefaultConfig()
	config.ParseOnHeader = true
	p2, err := NewProxy(plain.URL, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := p2.SetBodyLimit(10, "error"); err != nil {
		t.Fatal(err)
	}
	if rec := serveBodyLimit(p2, "GET"); rec.Code != http.StatusOK || rec.Body.Len() != 100 {
		t.Errorf("a page that is not parsed must not be limited, got %d (%d bytes)", rec.Code, rec.Body.Len())
	}
}

func TestSetBodyLimit_Validation(t *testing.T) {
	p, err := NewProxy("http://example.com", mesi.CreateDefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetBodyLimit(-1, "pass"); err == nil {
		t.Error("a negative size must be rejected")
	}
	if err := p.SetBodyLimit(10, "drop"); err == nil {
		t.Error("an unknown mode must be rejected")
	}
	if err := p.SetBodyLimit(10, ""); err != nil {
		t.Errorf("an empty mode is the default: %v", err)
	}
}
