package mesi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSingleFetchUrlSchemeValidation(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantErr   bool
		errSubstr string
	}{
		{"http scheme valid", "http://example.com", false, ""},
		{"https scheme valid", "https://example.com", false, ""},
		{"httpx scheme invalid", "httpx://example.com", true, "invalid url scheme"},
		{"httpfoo scheme invalid", "httpfoo://example.com", true, "invalid url scheme"},
		{"httpss scheme invalid", "httpss://example.com", true, "invalid url scheme"},
		{"ftp scheme invalid", "ftp://example.com", true, "invalid url scheme"},
		{"file scheme invalid", "file:///etc/passwd", true, "invalid url scheme"},
		{"javascript scheme invalid", "javascript:alert(1)", true, "invalid url scheme"},
	}

	config := EsiParserConfig{
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: false,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := singleFetchUrl(tt.url, config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errSubstr)
				} else if tt.errSubstr != "" && !contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestSingleFetchUrlRelativeUrl(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      server.URL + "/base/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: false,
	}

	_, _, err := singleFetchUrl("relative/path", config)
	if err != nil {
		t.Errorf("relative URL should resolve to %s/base/relative/path, got error: %v", server.URL, err)
	}
}

func TestSingleFetchUrlWithServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		} else if r.URL.Path == "/esi" {
			w.Header().Set("Edge-control", "dca=esi")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ESI_CONTENT"))
		} else {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("NOT_FOUND"))
		}
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        5,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: false,
	}

	t.Run("successful fetch", func(t *testing.T) {
		data, isEsi, err := singleFetchUrl(server.URL+"/ok", config)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if data != "OK" {
			t.Errorf("expected 'OK', got %q", data)
		}
		if isEsi {
			t.Errorf("expected isEsi=false for non-ESI response")
		}
	})

	t.Run("ESI response detection", func(t *testing.T) {
		data, isEsi, err := singleFetchUrl(server.URL+"/esi", config)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if data != "ESI_CONTENT" {
			t.Errorf("expected 'ESI_CONTENT', got %q", data)
		}
		if !isEsi {
			t.Errorf("expected isEsi=true for ESI response")
		}
	})

	t.Run("404 error", func(t *testing.T) {
		_, _, err := singleFetchUrl(server.URL+"/notexist", config)
		if err == nil {
			t.Errorf("expected error for 404")
		}
	})
}

func TestSingleFetchUrlEdgeCases(t *testing.T) {
	config := EsiParserConfig{
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: false,
	}

	t.Run("empty URL with no default", func(t *testing.T) {
		noDefaultConfig := EsiParserConfig{
			DefaultUrl:      "",
			MaxDepth:        1,
			Timeout:         1 * time.Second,
			BlockPrivateIPs: false,
		}
		_, _, err := singleFetchUrl("", noDefaultConfig)
		if err == nil {
			t.Errorf("expected error for empty URL")
		}
	})

	t.Run("URL with no host", func(t *testing.T) {
		_, _, err := singleFetchUrl("http://", config)
		if err == nil {
			t.Errorf("expected error for URL with no host")
		}
	})

	t.Run("backslash in relative URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(r.URL.Path))
		}))
		defer server.Close()

		testConfig := EsiParserConfig{
			DefaultUrl:      server.URL + "/",
			MaxDepth:        1,
			Timeout:         1 * time.Second,
			BlockPrivateIPs: false,
		}

		_, _, err := singleFetchUrl("..\\..\\etc\\passwd", testConfig)
		if err != nil {
			t.Errorf("unexpected error for backslash path: %v", err)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestSingleFetchUrlWithContextCancellation(t *testing.T) {
	requestReceived := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestReceived)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("SHOULD NOT SEE THIS"))
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	config := EsiParserConfig{
		Context:         ctx,
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        1,
		Timeout:         10 * time.Second,
		BlockPrivateIPs: false,
	}

	go func() {
		<-requestReceived
		cancel()
	}()

	_, _, err := singleFetchUrl(server.URL+"/slow", config)

	if err == nil {
		t.Error("expected context cancelled error, got nil")
	}

	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected error containing 'context', got: %v", err)
	}
}

func TestSingleFetchUrlWithContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("DONE"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	config := EsiParserConfig{
		Context:         ctx,
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        1,
		Timeout:         10 * time.Second,
		BlockPrivateIPs: false,
	}

	_, _, err := singleFetchUrl(server.URL+"/slow", config)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestSingleFetchUrlWithNilContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	config := EsiParserConfig{
		Context:         nil,
		DefaultUrl:      server.URL,
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: false,
	}

	data, _, err := singleFetchUrl(server.URL+"/ok", config)
	if err != nil {
		t.Errorf("unexpected error with nil context: %v", err)
	}
	if data != "OK" {
		t.Errorf("expected 'OK', got %q", data)
	}
}

func TestMESIParseContextPropagation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("SLOW_RESPONSE"))
		}
	}))
	defer server.Close()

	html := `<html><body><esi:include src="` + server.URL + `/slow"/></body></html>`

	ctx, cancel := context.WithCancel(context.Background())

	config := EsiParserConfig{
		Context:         ctx,
		DefaultUrl:      "http://example.com/",
		MaxDepth:        5,
		Timeout:         10 * time.Second,
		BlockPrivateIPs: false,
	}

	cancel()

	start := time.Now()
	result := MESIParse(html, config)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("MESIParse took too long (%v) - context not propagated properly", elapsed)
	}

	if result == "" {
		t.Log("result is empty (expected with cancelled context)")
	}
}

func TestMESIParseContextCancellationStopsAllGoroutines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("SLOW"))
		}
	}))
	defer server.Close()

	html := `<html><body><esi:include src="` + server.URL + `/slow"/><esi:include src="` + server.URL + `/slow"/></body></html>`

	ctx, cancel := context.WithCancel(context.Background())

	config := EsiParserConfig{
		Context:         ctx,
		DefaultUrl:      "http://example.com/",
		MaxDepth:        5,
		Timeout:         10 * time.Second,
		BlockPrivateIPs: false,
	}

	cancel()

	start := time.Now()
	result := MESIParse(html, config)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("MESIParse took too long (%v) with cancelled context", elapsed)
	}

	_ = result
}

func TestFetchConcurrentBothSucceed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("PRIMARY"))
		} else if r.URL.Path == "/alt" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ALT"))
		}
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      server.URL + "/",
		MaxDepth:        1,
		Timeout:         2 * time.Second,
		BlockPrivateIPs: false,
	}

	html := `<html><esi:include src="` + server.URL + `/primary" alt="` + server.URL + `/alt" fetch-mode="concurrent" /></html>`

	result := MESIParse(html, config)
	if !strings.Contains(result, "PRIMARY") && !strings.Contains(result, "ALT") {
		t.Errorf("expected PRIMARY or ALT in output, got %q", result)
	}
}

func TestFetchConcurrentPrimaryFailsAltSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("NOT_FOUND"))
		} else if r.URL.Path == "/alt" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ALT_RESPONSE"))
		}
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      server.URL + "/",
		MaxDepth:        1,
		Timeout:         2 * time.Second,
		BlockPrivateIPs: false,
	}

	html := `<html><esi:include src="` + server.URL + `/primary" alt="` + server.URL + `/alt" fetch-mode="concurrent" /></html>`

	result := MESIParse(html, config)
	if !strings.Contains(result, "ALT_RESPONSE") {
		t.Errorf("expected ALT_RESPONSE in output (alt finishes first), got %q", result)
	}
}

func TestFetchConcurrentBothFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      server.URL + "/",
		MaxDepth:        1,
		Timeout:         2 * time.Second,
		BlockPrivateIPs: false,
	}

	html := `<html><esi:include src="` + server.URL + `/fail1" alt="` + server.URL + `/fail2" fetch-mode="concurrent" /></html>`

	result := MESIParse(html, config)
	if !strings.Contains(result, "500") && !strings.Contains(result, "error") {
		t.Errorf("expected error in output when both URLs fail, got %q", result)
	}
}

func TestFetchConcurrentNoAltShortCircuit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/single" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("SINGLE"))
		}
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      server.URL + "/",
		MaxDepth:        1,
		Timeout:         2 * time.Second,
		BlockPrivateIPs: false,
	}

	html := `<html><esi:include src="` + server.URL + `/single" fetch-mode="concurrent" /></html>`

	result := MESIParse(html, config)
	if !strings.Contains(result, "SINGLE") {
		t.Errorf("expected SINGLE in output, got %q", result)
	}
}

func TestFetchConcurrentContextCancellation(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	config := EsiParserConfig{
		Context:         ctx,
		DefaultUrl:      slowServer.URL + "/",
		MaxDepth:        1,
		Timeout:         2 * time.Second,
		BlockPrivateIPs: false,
	}

	start := time.Now()
	html := `<html><esi:include src="` + slowServer.URL + `/slow1" alt="` + slowServer.URL + `/slow2" fetch-mode="concurrent" /></html>`
	_ = MESIParse(html, config)
	elapsed := time.Since(start)

	if elapsed > 1*time.Second {
		t.Errorf("fetchConcurrent took too long (%v) with cancelled context", elapsed)
	}
}
