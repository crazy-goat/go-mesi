package mesi

import (
	"net/http"
	"net/http/httptest"
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
