package mesi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestMESIParseContextCancellationMidParse(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("SLOW"))
		}
	}))
	defer slowServer.Close()

	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("FAST"))
	}))
	defer fastServer.Close()

	// Mix of slow and fast includes
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

	// Start MESIParse in a goroutine
	done := make(chan string)
	go func() {
		result := MESIParse(html, config)
		done <- result
	}()

	// Cancel context after 100ms, while MESIParse is running
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Wait for MESIParse to return
	select {
	case result := <-done:
		_ = result
	case <-time.After(2 * time.Second):
		t.Fatal("MESIParse did not return within 2 seconds after context cancellation")
	}
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
			// Primary fails immediately to test that fetchConcurrent waits for alt's success
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("PRIMARY_ERROR"))
		} else if r.URL.Path == "/alt" {
			// Alt succeeds after a small delay
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
	// BUG: This currently fails because fetchConcurrent returns the first result (primary's error)
	// instead of waiting for the first SUCCESSFUL result
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

func TestIsURLSafe_BlocksPrivateIPs(t *testing.T) {
	// Note: isURLSafe no longer checks private IPs at validation time.
	// Private IP checking is now done at dial time via safeDialer.
	// See TestSSRFDialBlocksPrivateIP for dial-time tests.
	tests := []struct {
		name string
		url  string
	}{
		{"localhost", "http://localhost/test"},
		{"127.0.0.1", "http://127.0.0.1/test"},
		{"10.0.0.1", "http://10.0.0.1/test"},
		{"public IP", "http://8.8.8.8/test"},
	}

	config := EsiParserConfig{
		BlockPrivateIPs: true,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := isURLSafe(tt.url, config)
			// isURLSafe should NOT return error for private IPs anymore
			// (check is now at dial time)
			if err != nil {
				t.Errorf("isURLSafe should not check private IPs, got error: %v", err)
			}
		})
	}
}

func TestIsURLSafe_AllowedHosts(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		allowedHosts []string
		wantErr      bool
	}{
		{"allowed exact", "http://example.com/test", []string{"example.com"}, false},
		{"allowed subdomain", "http://api.example.com/test", []string{"example.com"}, false},
		{"not allowed", "http://other.com/test", []string{"example.com"}, true},
		{"multiple allowed", "http://foo.com/test", []string{"example.com", "foo.com"}, false},
		{"empty allowed list", "http://example.com/test", []string{}, false},
		// Port handling tests
		{"allowed host with port", "http://example.com:8080/test", []string{"example.com"}, false},
		{"allowed subdomain with port", "http://api.example.com:443/test", []string{"example.com"}, false},
		{"not allowed with port", "http://other.com:8080/test", []string{"example.com"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{
				BlockPrivateIPs: true,
				AllowedHosts:    tt.allowedHosts,
			}
			err := isURLSafe(tt.url, config)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestIsURLSafe_Disabled(t *testing.T) {
	config := EsiParserConfig{
		BlockPrivateIPs: false,
	}

	err := isURLSafe("http://127.0.0.1/test", config)
	if err != nil {
		t.Errorf("expected no error when BlockPrivateIPs=false, got: %v", err)
	}
}

func TestIsURLSafe_InvalidURL(t *testing.T) {
	config := EsiParserConfig{
		BlockPrivateIPs: true,
	}

	tests := []struct {
		name string
		url  string
	}{
		{"invalid url", "://invalid"},
		{"no host", "http:///path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := isURLSafe(tt.url, config)
			if err == nil {
				t.Error("expected error for invalid URL")
			}
		})
	}
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

func TestSingleFetchUrlSSRFValidation(t *testing.T) {
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: true,
		Logger:          DiscardLogger{},
	}

	_, _, err := singleFetchUrl("http://127.0.0.1/test", config)
	if err == nil {
		t.Error("expected SSRF error for private IP")
	}
	// Error now comes from dialer, not from isURLSafe validation
	if !strings.Contains(err.Error(), "blocked dial to private/reserved ip") {
		t.Errorf("expected dial-time SSRF error, got: %v", err)
	}
}

func TestParseWithConfigAllowedHostsAndBlockPrivateIPs(t *testing.T) {
	if out := MESIParse(`<esi:include src="http://evil.com/test" />`, EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        5,
		Timeout:         30 * time.Second,
		AllowedHosts:    []string{"allowed.com"},
		BlockPrivateIPs: true,
	}); !strings.Contains(out, "host not in allowed list") {
		t.Fatalf("expected allowed-host SSRF block, got %q", out)
	}

	if out := MESIParse(`<esi:include src="http://127.0.0.1/test" />`, EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        5,
		Timeout:         30 * time.Second,
		BlockPrivateIPs: true,
	}); !strings.Contains(out, "blocked") {
		t.Fatalf("expected private IP SSRF block, got %q", out)
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

func TestIsPrivateOrReservedIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"loopback", "127.0.0.1", true},
		{"10.0.0.0/8", "10.0.0.1", true},
		{"10.255.255.255", "10.255.255.255", true},
		{"172.16.0.0/12", "172.16.0.1", true},
		{"172.31.255.255", "172.31.255.255", true},
		{"192.168.0.0/16", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"unspecified", "0.0.0.0", true},
		{"multicast", "224.0.0.1", true},
		{"reserved", "240.0.0.1", true},
		{"public", "8.8.8.8", false},
		{"public 2", "1.1.1.1", false},
		{"ipv6 loopback", "::1", true},
		{"ipv6 ULA fd00", "fd00::1", true},
		{"ipv6 ULA fc00", "fc00::1", true},
		{"ipv6 link-local", "fe80::1", true},
		{"ipv6 unspecified", "::", true},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"ipv4-mapped private", "::ffff:10.0.0.1", true},
		{"ipv6 documentation", "2001:db8::1", true},
		{"ipv6 multicast", "ff02::1", true},
		{"nat64", "64:ff9b::8.8.8.8", true},
		{"cgnat", "100.64.0.1", true},
		{"benchmark", "198.18.0.1", true},
		{"public ipv6", "2606:4700:4700::1111", false},
		{"public ipv6 google", "2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			result := isPrivateOrReservedIP(ip)
			if result != tt.expected {
				t.Errorf("isPrivateOrReservedIP(%s) = %v, expected %v", tt.ip, result, tt.expected)
			}
		})
	}
}

func TestSingleFetchUrlWithContext_ResponseUnderLimit(t *testing.T) {
	// Create test server that returns 1KB response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 1024)) // 1KB
	}))
	defer ts.Close()

	config := CreateDefaultConfig()
	config.MaxResponseSize = 10 * 1024 // 10KB limit
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
	// Create test server that returns 20KB response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 20*1024)) // 20KB
	}))
	defer ts.Close()

	config := CreateDefaultConfig()
	config.MaxResponseSize = 10 * 1024 // 10KB limit
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
	// Create test server that returns 100KB response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 100*1024)) // 100KB
	}))
	defer ts.Close()

	config := CreateDefaultConfig()
	config.MaxResponseSize = 0 // No limit
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
	// Create test server that returns exactly at limit
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, limit)) // Exactly at limit
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

func TestSSRFDialBlocksPrivateIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	// Create config with BlockPrivateIPs=true
	config := EsiParserConfig{
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        1,
		Timeout:         2 * time.Second,
		BlockPrivateIPs: true,
		Logger:          DiscardLogger{},
	}

	// Try to fetch from the test server
	// The test server binds to 127.0.0.1 which is a private IP
	// With BlockPrivateIPs=true, this should fail at dial time
	_, _, err := singleFetchUrlWithContext(server.URL, config, context.Background())
	if err == nil {
		t.Fatal("expected error when fetching from private IP with BlockPrivateIPs=true, got nil")
	}
	if !contains(err.Error(), "blocked dial to private/reserved ip") {
		t.Errorf("expected 'blocked dial' error, got: %v", err)
	}
}

func TestSSRFDialAllowsPublicIP(t *testing.T) {
	// Test that the safeDialer Control callback allows public IPs
	config := EsiParserConfig{
		BlockPrivateIPs: true,
	}

	dialer := safeDialer(config)

	// Test with a public IP (8.8.8.8 - Google DNS)
	// The Control callback should allow this (return nil)
	err := dialer.Control("tcp", "8.8.8.8:80", nil)
	if err != nil {
		t.Errorf("expected public IP 8.8.8.8 to be allowed, got error: %v", err)
	}

	// Test with another public IP (1.1.1.1 - Cloudflare DNS)
	err = dialer.Control("tcp", "1.1.1.1:443", nil)
	if err != nil {
		t.Errorf("expected public IP 1.1.1.1 to be allowed, got error: %v", err)
	}

	// Verify that private IPs are still blocked
	err = dialer.Control("tcp", "127.0.0.1:80", nil)
	if err == nil {
		t.Error("expected private IP 127.0.0.1 to be blocked")
	}

	err = dialer.Control("tcp", "10.0.0.1:80", nil)
	if err == nil {
		t.Error("expected private IP 10.0.0.1 to be blocked")
	}
}

func TestSSRFDialerWithBlockPrivateIPsDisabled(t *testing.T) {
	// When BlockPrivateIPs=false, all IPs should be allowed
	config := EsiParserConfig{
		BlockPrivateIPs: false,
	}

	dialer := safeDialer(config)

	// Private IPs should be allowed when BlockPrivateIPs=false
	err := dialer.Control("tcp", "127.0.0.1:80", nil)
	if err != nil {
		t.Errorf("expected private IP to be allowed when BlockPrivateIPs=false, got: %v", err)
	}

	err = dialer.Control("tcp", "10.0.0.1:80", nil)
	if err != nil {
		t.Errorf("expected private IP to be allowed when BlockPrivateIPs=false, got: %v", err)
	}

	// Public IPs should also be allowed
	err = dialer.Control("tcp", "8.8.8.8:80", nil)
	if err != nil {
		t.Errorf("expected public IP to be allowed when BlockPrivateIPs=false, got: %v", err)
	}
}

func TestNewSSRFSafeTransport(t *testing.T) {
	config := EsiParserConfig{
		BlockPrivateIPs: true,
	}

	transport := NewSSRFSafeTransport(config)
	if transport == nil {
		t.Fatal("NewSSRFSafeTransport returned nil")
	}

	// Verify the transport has the correct DialContext
	if transport.DialContext == nil {
		t.Fatal("transport.DialContext is nil")
	}

	// Test with BlockPrivateIPs=false
	config2 := EsiParserConfig{
		BlockPrivateIPs: false,
	}
	transport2 := NewSSRFSafeTransport(config2)
	if transport2 == nil {
		t.Fatal("NewSSRFSafeTransport returned nil for BlockPrivateIPs=false")
	}
}

func TestSSRFBlocksIPv6Loopback(t *testing.T) {
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: true,
		Logger:          DiscardLogger{},
	}

	html := `<html><body><esi:include src="http://[::1]/test"/></body></html>`
	result := MESIParse(html, config)

	if !strings.Contains(result, "blocked dial to private/reserved ip") {
		t.Errorf("expected SSRF block for IPv6 loopback, got: %q", result)
	}
}

func TestSSRFBlocksIPv6ULA(t *testing.T) {
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: true,
		Logger:          DiscardLogger{},
	}

	html := `<html><body><esi:include src="http://[fd00::1]/test"/></body></html>`
	result := MESIParse(html, config)

	if !strings.Contains(result, "blocked dial to private/reserved ip") {
		t.Errorf("expected SSRF block for IPv6 ULA, got: %q", result)
	}
}

func TestSSRFBlocksIPv4MappedIPv6(t *testing.T) {
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: true,
		Logger:          DiscardLogger{},
	}

	html := `<html><body><esi:include src="http://[::ffff:127.0.0.1]/test"/></body></html>`
	result := MESIParse(html, config)

	if !strings.Contains(result, "blocked dial to private/reserved ip") {
		t.Errorf("expected SSRF block for IPv4-mapped IPv6, got: %q", result)
	}
}
