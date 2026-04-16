package mesi

import (
	"net/url"
	"strings"
	"testing"
)

func TestUrlSchemeValidation(t *testing.T) {
	validSchemes := []string{"http", "https"}
	invalidSchemes := []string{"httpx", "httpfoo", "httpss", "ftp", "file", "javascript", "data", "gopher"}

	baseHost := "://example.com/path"

	for _, scheme := range validSchemes {
		u := scheme + baseHost
		parsed, err := url.Parse(u)
		if err != nil {
			t.Errorf("valid scheme %q failed to parse: %v", scheme, err)
			continue
		}
		if parsed.Scheme != scheme {
			t.Errorf("expected scheme %q, got %q", scheme, parsed.Scheme)
		}
	}

	for _, scheme := range invalidSchemes {
		u := scheme + baseHost
		parsed, err := url.Parse(u)
		if err != nil {
			t.Errorf("invalid scheme %q failed to parse: %v", scheme, err)
			continue
		}
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			t.Errorf("scheme %q should be rejected but got scheme %q", scheme, parsed.Scheme)
		}
	}
}

func TestRelativeUrlHandling(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		defUrl   string
		expected string
	}{
		{"simple relative", "foo/bar", "http://base.com/", "http://base.com/foo/bar"},
		{"absolute path", "/foo/bar", "http://base.com/", "http://base.com/foo/bar"},
		{"no leading slash", "foo/bar", "http://base.com", "http://base.com/foo/bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, _ := url.Parse(tt.input)
			if parsed.Scheme != "" {
				t.Errorf("relative URL %q should have empty scheme, got %q", tt.input, parsed.Scheme)
			}

			result := strings.TrimRight(tt.defUrl, "/") + "/" + strings.TrimLeft(tt.input, "/")
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestUrlSchemeCheckLogic(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
		errMsg  string
	}{
		{"http://example.com", false, ""},
		{"https://example.com", false, ""},
		{"http://example.com/path?query=1", false, ""},
		{"https://example.com/path#anchor", false, ""},
		{"httpx://example.com", true, "invalid url scheme"},
		{"httpfoo://example.com", true, "invalid url scheme"},
		{"httpss://example.com", true, "invalid url scheme"},
		{"ftp://example.com", true, "invalid url scheme"},
		{"file:///etc/passwd", true, "invalid url scheme"},
		{"javascript:alert(1)", true, "invalid url scheme"},
	}

	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			parsed, err := url.Parse(c.url)
			if err != nil {
				if !c.wantErr {
					t.Errorf("unexpected parse error for %q: %v", c.url, err)
				}
				return
			}

			isInvalid := parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https"
			if c.wantErr && !isInvalid {
				t.Errorf("expected invalid scheme for %q, but it was accepted", c.url)
			}
			if !c.wantErr && isInvalid {
				t.Errorf("expected valid scheme for %q, but was rejected", c.url)
			}
		})
	}
}
