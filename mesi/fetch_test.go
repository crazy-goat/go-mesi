package mesi

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

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
		Logger:          DiscardLogger{},
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

	t.Run("error message does not leak response body", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
			body       string
		}{
			{"500 internal error", http.StatusInternalServerError, "INTERNAL_ERROR_SECRET_DATA"},
			{"404 not found", http.StatusNotFound, "SECRET_NOT_FOUND_DETAILS"},
			{"403 forbidden", http.StatusForbidden, "SECRET_FORBIDDEN_REASON"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer server.Close()

				_, _, err := singleFetchUrl(server.URL+"/secret", config)
				if err == nil {
					t.Fatal("expected error")
				}
				if strings.Contains(err.Error(), tt.body) {
					t.Errorf("error message leaks response body: %q", err.Error())
				}
				if !strings.Contains(err.Error(), strconv.Itoa(tt.statusCode)) {
					t.Errorf("error message should contain status code: %q", err.Error())
				}
			})
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
	handlerReached := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerReached)
		<-r.Context().Done()
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

	result := MESIParse(html, config)

	select {
	case <-handlerReached:
		t.Error("handler was reached despite cancelled context")
	default:
	}

	if result == "" {
		t.Log("result is empty (expected with cancelled context)")
	}
}

func TestMESIParseContextCancellationStopsAllGoroutines(t *testing.T) {
	handlerReached := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerReached)
		<-r.Context().Done()
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

	result := MESIParse(html, config)

	select {
	case <-handlerReached:
		t.Error("handler was reached despite cancelled context")
	default:
	}

	_ = result
}

func TestMESIParseContextCancellationMidParse(t *testing.T) {
	handlerReached := make(chan struct{})

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case handlerReached <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer slowServer.Close()

	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("FAST"))
	}))
	defer fastServer.Close()

	html := `<html><body>` +
		`<esi:include src="` + slowServer.URL + `/slow"/>` +
		`<esi:include src="` + fastServer.URL + `/fast"/>` +
		`<esi:include src="` + slowServer.URL + `/slow2"/>` +
		`</body></html>`

	ctx, cancel := context.WithCancel(context.Background())

	config := EsiParserConfig{
		Context:         ctx,
		DefaultUrl:      "http://example.com/",
		MaxDepth:        5,
		Timeout:         10 * time.Second,
		BlockPrivateIPs: false,
	}

	done := make(chan string)
	go func() {
		result := MESIParse(html, config)
		done <- result
	}()

	<-handlerReached
	cancel()

	result := <-done
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

func TestFetchConcurrentPrimaryFailsImmediatelyAltSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("PRIMARY_ERROR"))
		} else if r.URL.Path == "/alt" {
			time.Sleep(50 * time.Millisecond)
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
		t.Errorf("expected ALT_RESPONSE in output (should wait for success), got %q", result)
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
	if strings.Contains(result, "500") || strings.Contains(result, "error") {
		t.Errorf("error details leaked into output: %q", result)
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
	handlerBlock := make(chan struct{})

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-handlerBlock:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		}
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

	html := `<html><esi:include src="` + slowServer.URL + `/slow1" alt="` + slowServer.URL + `/slow2" fetch-mode="concurrent" /></html>`
	result := MESIParse(html, config)

	if strings.Contains(result, "OK") {
		t.Errorf("expected empty result due to context cancellation, got %q", result)
	}
}

func TestAllowPrivateIPsForAllowedHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test response"))
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	serverHost := serverURL.Hostname()

	t.Run("blocked when flag is false", func(t *testing.T) {
		config := EsiParserConfig{
			BlockPrivateIPs:                true,
			AllowPrivateIPsForAllowedHosts: false,
			AllowedHosts:                   []string{serverHost},
			Timeout:                        5 * time.Second,
		}

		_, _, err := singleFetchUrlWithContext(server.URL, config, context.Background())
		if err == nil {
			t.Error("expected error for private IP when flag is false")
		}
	})

	t.Run("allowed when flag is true", func(t *testing.T) {
		config := EsiParserConfig{
			BlockPrivateIPs:                true,
			AllowPrivateIPsForAllowedHosts: true,
			AllowedHosts:                   []string{serverHost},
			Timeout:                        5 * time.Second,
		}

		result, _, err := singleFetchUrlWithContext(server.URL, config, context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "test response" {
			t.Errorf("expected 'test response', got: %q", result)
		}
	})

	t.Run("blocked when host not in AllowedHosts", func(t *testing.T) {
		config := EsiParserConfig{
			BlockPrivateIPs:                true,
			AllowPrivateIPsForAllowedHosts: true,
			AllowedHosts:                   []string{"other.example.com"},
			Timeout:                        5 * time.Second,
		}

		_, _, err := singleFetchUrlWithContext(server.URL, config, context.Background())
		if err == nil {
			t.Error("expected error for host not in AllowedHosts")
		}
	})
}

func TestIsEsiResponse(t *testing.T) {
	tests := []struct {
		name           string
		edgeControl    string
		expectedResult bool
	}{
		{"dca=esi header", "dca=esi", true},
		{"dca=esi with other directives", "no-store, dca=esi, max-age=3600", true},
		{"dca=esi case insensitive", "DCA=ESI", true},
		{"no dca=esi", "no-store, max-age=3600", false},
		{"empty header", "", false},
		{"partial match dca", "dca=esionly", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{},
			}
			if tt.edgeControl != "" {
				resp.Header.Set("Edge-control", tt.edgeControl)
			}
			result := IsEsiResponse(resp)
			if result != tt.expectedResult {
				t.Errorf("IsEsiResponse() = %v, expected %v", result, tt.expectedResult)
			}
		})
	}
}

func TestSingleFetchUrlExceedsTimeBudget(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"zero timeout", 0},
		{"negative timeout", -1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{
				DefaultUrl:      "http://example.com/",
				MaxDepth:        1,
				Timeout:         tt.timeout,
				BlockPrivateIPs: false,
				Logger:          DiscardLogger{},
			}

			_, _, err := singleFetchUrl("http://example.com/test", config)
			if err == nil {
				t.Error("expected error for timeout <= 0")
			}
			if !strings.Contains(err.Error(), "exceeded time budget") {
				t.Errorf("expected 'exceeded time budget' error, got: %v", err)
			}
		})
	}
}

func TestParseWithConfigAllowedHostsAndBlockPrivateIPs(t *testing.T) {
	log := &recordingLogger{}

	out := MESIParse(`<esi:include src="http://evil.com/test" />`, EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        5,
		Timeout:         30 * time.Second,
		AllowedHosts:    []string{"allowed.com"},
		BlockPrivateIPs: true,
		Logger:          log,
	})
	if out != "" {
		t.Fatalf("expected empty output for blocked include, got %q", out)
	}
	if !log.containsMsg("include_failed") {
		t.Fatal("expected include_failed log for allowed-host block")
	}

	log.entries = nil

	out = MESIParse(`<esi:include src="http://127.0.0.1/test" />`, EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        5,
		Timeout:         30 * time.Second,
		BlockPrivateIPs: true,
		Logger:          log,
	})
	if out != "" {
		t.Fatalf("expected empty output for blocked include, got %q", out)
	}
	if !log.containsMsg("include_failed") {
		t.Fatal("expected include_failed log for private IP block")
	}
}

func TestSingleFetchUrlInvalidRequest(t *testing.T) {
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: false,
		Logger:          DiscardLogger{},
	}

	_, _, err := singleFetchUrl("http://\x00invalid/test", config)
	if err == nil {
		t.Fatal("expected error for invalid URL in request creation")
	}
	if !strings.Contains(err.Error(), "invalid url") {
		t.Errorf("expected 'invalid url' error, got: %v", err)
	}
}

func TestSingleFetchUrlWithContext_ResponseUnderLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 1024))
	}))
	defer ts.Close()

	config := CreateDefaultConfig()
	config.MaxResponseSize = 10 * 1024
	config.Timeout = 5 * time.Second
	config.BlockPrivateIPs = false

	data, _, err := singleFetchUrlWithContext(ts.URL, config, context.Background())
	if err != nil {
		t.Errorf("Expected no error for response under limit, got: %v", err)
	}
	if len(data) != 1024 {
		t.Errorf("Expected 1024 bytes, got %d", len(data))
	}
}

func TestSingleFetchUrlWithContext_ResponseExceedsLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 20*1024))
	}))
	defer ts.Close()

	config := CreateDefaultConfig()
	config.MaxResponseSize = 10 * 1024
	config.Timeout = 5 * time.Second
	config.BlockPrivateIPs = false

	_, _, err := singleFetchUrlWithContext(ts.URL, config, context.Background())
	if err == nil {
		t.Error("Expected error for response exceeding limit, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Errorf("Expected error message about size limit, got: %v", err)
	}
}

func TestSingleFetchUrlWithContext_ZeroLimitNoRestriction(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 100*1024))
	}))
	defer ts.Close()

	config := CreateDefaultConfig()
	config.MaxResponseSize = 0
	config.Timeout = 5 * time.Second
	config.BlockPrivateIPs = false

	data, _, err := singleFetchUrlWithContext(ts.URL, config, context.Background())
	if err != nil {
		t.Errorf("Expected no error with zero limit, got: %v", err)
	}
	if len(data) != 100*1024 {
		t.Errorf("Expected 100KB data, got %d bytes", len(data))
	}
}

func TestSingleFetchUrlWithContext_ExactLimit(t *testing.T) {
	limit := int64(1024)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, limit))
	}))
	defer ts.Close()

	config := CreateDefaultConfig()
	config.MaxResponseSize = limit
	config.Timeout = 5 * time.Second
	config.BlockPrivateIPs = false

	data, _, err := singleFetchUrlWithContext(ts.URL, config, context.Background())
	if err != nil {
		t.Errorf("Expected no error for response at exact limit, got: %v", err)
	}
	if int64(len(data)) != limit {
		t.Errorf("Expected %d bytes, got %d", limit, len(data))
	}
}

func TestFetchWithCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cached content"))
	}))
	defer server.Close()

	cache := NewMemoryCache(100, time.Hour)
	config := CreateDefaultConfig()
	config.Cache = cache
	config.CacheKeyFunc = func(url string) string { return "test:" + url }
	config.BlockPrivateIPs = false

	url := server.URL + "/test"
	_, _, _ = singleFetchUrlWithContext(url, config, context.Background())
	if callCount != 1 {
		t.Fatalf("first call: expected 1 HTTP call, got %d", callCount)
	}

	_, _, _ = singleFetchUrlWithContext(url, config, context.Background())
	if callCount != 1 {
		t.Fatalf("second call: expected 0 HTTP calls (cached), got %d", callCount)
	}

	ctx := context.Background()
	val, ok, _ := cache.Get(ctx, "test:"+url)
	if !ok || val != "cached content" {
		t.Fatalf("cache miss or wrong value: ok=%v, val=%s", ok, val)
	}
}

func TestFetchWithoutCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("direct content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.Cache = nil
	config.BlockPrivateIPs = false

	url := server.URL + "/test"
	content, _, err := singleFetchUrlWithContext(url, config, context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "direct content" {
		t.Fatalf("expected 'direct content', got '%s'", content)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", callCount)
	}
}

func TestFetchWithCache_NilCacheKeyFunc(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("direct content"))
	}))
	defer server.Close()

	cache := NewMemoryCache(100, time.Hour)
	config := CreateDefaultConfig()
	config.Cache = cache
	config.CacheKeyFunc = nil
	config.BlockPrivateIPs = false

	url := server.URL + "/test"
	content, _, err := singleFetchUrlWithContext(url, config, context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "direct content" {
		t.Fatalf("expected 'direct content', got '%s'", content)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 HTTP call (no cache key func), got %d", callCount)
	}
}

func TestFetchWithCache_EsiResponse(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Edge-control", "dca=esi")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("esi content"))
	}))
	defer server.Close()

	cache := NewMemoryCache(100, time.Hour)
	config := CreateDefaultConfig()
	config.Cache = cache
	config.CacheKeyFunc = func(url string) string { return "test:" + url }
	config.BlockPrivateIPs = false

	url := server.URL + "/esi"
	content, isEsi, err := singleFetchUrlWithContext(url, config, context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "esi content" {
		t.Fatalf("expected 'esi content', got '%s'", content)
	}
	if !isEsi {
		t.Fatalf("expected isEsi=true for ESI response")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", callCount)
	}

	content2, isEsi2, err := singleFetchUrlWithContext(url, config, context.Background())
	if err != nil {
		t.Fatalf("unexpected error on cached call: %v", err)
	}
	if content2 != "esi content" {
		t.Fatalf("expected cached 'esi content', got '%s'", content2)
	}
	if isEsi2 {
		t.Fatalf("expected isEsi=false for cached response")
	}
	if callCount != 1 {
		t.Fatalf("expected 0 HTTP calls (cached), got %d", callCount)
	}
}

var _ Cache = errorCache{}

type errorCache struct{}

func (errorCache) Get(_ context.Context, key string) (string, bool, error) {
	return "", false, errors.New("cache get failed: " + key)
}

func (errorCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	return errors.New("cache set failed: " + key)
}

func (errorCache) Delete(_ context.Context, key string) error {
	return nil
}

func TestCacheGetErrorGoesToLogger(t *testing.T) {
	logger := &recordingLogger{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("origin content"))
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      server.URL + "/",
		MaxDepth:        1,
		Timeout:         5 * time.Second,
		BlockPrivateIPs: false,
		Logger:          logger,
		Cache:           errorCache{},
		CacheKeyFunc:    DefaultCacheKey,
	}

	content, _, err := singleFetchUrlWithContext(server.URL+"/test", config, context.Background())
	if err != nil {
		t.Fatalf("expected fallback to origin on cache get error, got: %v", err)
	}
	if content != "origin content" {
		t.Fatalf("expected 'origin content', got %q", content)
	}
	if !logger.containsMsg("cache_get_error") {
		t.Fatal("expected cache_get_error log entry")
	}
}

func TestCacheSetErrorGoesToLogger(t *testing.T) {
	logger := &recordingLogger{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("origin content"))
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      server.URL + "/",
		MaxDepth:        1,
		Timeout:         5 * time.Second,
		BlockPrivateIPs: false,
		Logger:          logger,
		Cache:           errorCache{},
		CacheKeyFunc:    DefaultCacheKey,
	}

	content, _, err := singleFetchUrlWithContext(server.URL+"/test", config, context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "origin content" {
		t.Fatalf("expected 'origin content', got %q", content)
	}
	if !logger.containsMsg("cache_set_error") {
		t.Fatal("expected cache_set_error log entry")
	}
}

func TestCacheErrorDoesNotReachStderr(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	logger := &recordingLogger{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("origin content"))
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      server.URL + "/",
		MaxDepth:        1,
		Timeout:         5 * time.Second,
		BlockPrivateIPs: false,
		Logger:          logger,
		Cache:           errorCache{},
		CacheKeyFunc:    DefaultCacheKey,
	}

	_, _, err := singleFetchUrlWithContext(server.URL+"/test", config, context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf.Len() > 0 {
		t.Fatalf("stdlib log received unexpected output: %s", buf.String())
	}
}

func TestSentinelErrorsIs(t *testing.T) {
	_, _, schemeErr := singleFetchUrlWithContext("httpx://example.com/test", EsiParserConfig{Timeout: time.Second, BlockPrivateIPs: false, Logger: DiscardLogger{}}, context.Background())
	_, _, timeoutErr := singleFetchUrlWithContext("http://example.com/test", EsiParserConfig{Timeout: 0, BlockPrivateIPs: false, Logger: DiscardLogger{}}, context.Background())
	_, _, dialErr := singleFetchUrlWithContext("http://127.0.0.1/test", EsiParserConfig{Timeout: time.Second, BlockPrivateIPs: true, Logger: DiscardLogger{}}, context.Background())
	hostErr := isURLSafe("http://evil.com/test", EsiParserConfig{AllowedHosts: []string{"allowed.com"}})

	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "invalid scheme wraps ErrInvalidURL",
			err:    schemeErr,
			target: ErrInvalidURL,
			want:   true,
		},
		{
			name:   "timeout wraps ErrTimeBudgetExceeded",
			err:    timeoutErr,
			target: ErrTimeBudgetExceeded,
			want:   true,
		},
		{
			name:   "dial SSRF block wraps ErrSSRFBlocked",
			err:    dialErr,
			target: ErrSSRFBlocked,
			want:   true,
		},
		{
			name:   "allowed-host SSRF block wraps ErrSSRFBlocked",
			err:    hostErr,
			target: ErrSSRFBlocked,
			want:   true,
		},
		{
			name:   "host-not-allowed not ErrInvalidURL",
			err:    hostErr,
			target: ErrInvalidURL,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatal("expected non-nil error")
			}
			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v\nerr.Error() = %q", tt.err, tt.target, got, tt.want, tt.err)
			}
		})
	}
}
