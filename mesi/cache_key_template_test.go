package mesi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildCacheKey_URL(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	key := BuildCacheKey("http://backend/frag", "mesi:${url}", r)
	if key != "mesi:http://backend/frag" {
		t.Errorf("expected 'mesi:http://backend/frag', got '%s'", key)
	}
}

func TestBuildCacheKey_Header(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("Accept-Language", "pl")

	// canonical form
	key := BuildCacheKey("/frag", "mesi:${url}:${header:Accept-Language}", r)
	if key != "mesi:/frag:pl" {
		t.Errorf("expected 'mesi:/frag:pl', got '%s'", key)
	}

	// lowercase form
	key = BuildCacheKey("/frag", "mesi:${url}:${header:accept-language}", r)
	if key != "mesi:/frag:pl" {
		t.Errorf("expected 'mesi:/frag:pl', got '%s'", key)
	}

	// uppercase form
	key = BuildCacheKey("/frag", "mesi:${url}:${header:ACCEPT-LANGUAGE}", r)
	if key != "mesi:/frag:pl" {
		t.Errorf("expected 'mesi:/frag:pl', got '%s'", key)
	}
}

func TestBuildCacheKey_Cookie(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})

	// canonical form
	key := BuildCacheKey("/frag", "mesi:${url}:${cookie:session}", r)
	if key != "mesi:/frag:abc123" {
		t.Errorf("expected 'mesi:/frag:abc123', got '%s'", key)
	}

	// lowercase form
	key = BuildCacheKey("/frag", "mesi:${url}:${cookie:session}", r)
	if key != "mesi:/frag:abc123" {
		t.Errorf("expected 'mesi:/frag:abc123', got '%s'", key)
	}
}

func TestBuildCacheKey_UnknownPlaceholder(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	key := BuildCacheKey("/frag", "mesi:${url}:${unknown:foo}", r)
	if key != "mesi:/frag:${unknown:foo}" {
		t.Errorf("expected 'mesi:/frag:${unknown:foo}', got '%s'", key)
	}
}

func TestBuildCacheKey_MultiplePlaceholders(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("Accept-Language", "en")
	r.AddCookie(&http.Cookie{Name: "ab_test", Value: "B"})

	key := BuildCacheKey("/frag", "${url}:${header:Accept-Language}:${cookie:ab_test}", r)
	if key != "/frag:en:B" {
		t.Errorf("expected '/frag:en:B', got '%s'", key)
	}
}

func TestBuildCacheKey_EmptyTemplate(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	key := BuildCacheKey("/frag", "", r)
	if key != "" {
		t.Errorf("expected empty string, got '%s'", key)
	}
}
