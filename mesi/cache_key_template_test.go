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

	// uppercase form
	key = BuildCacheKey("/frag", "mesi:${url}:${cookie:SESSION}", r)
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

// TestBuildCacheKey_HeaderValueNotRescanned is the regression test for #384: a
// substituted header value must never be scanned for further placeholders,
// independent of the (random) iteration order of the request header map.
func TestBuildCacheKey_HeaderValueNotRescanned(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("X-Test", "${header:Accept-Language}")
	r.Header.Set("Accept-Language", "en")

	const want = "prefix:${header:Accept-Language}:suffix"
	for i := 0; i < 1000; i++ {
		if got := BuildCacheKey("/frag", "prefix:${header:X-Test}:suffix", r); got != want {
			t.Fatalf("iteration %d: expected %q, got %q", i, want, got)
		}
	}
}

// TestBuildCacheKey_CookieValueNotRescanned is the cookie counterpart of
// TestBuildCacheKey_HeaderValueNotRescanned.
func TestBuildCacheKey_CookieValueNotRescanned(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.AddCookie(&http.Cookie{Name: "a", Value: "${cookie:b}"})
	r.AddCookie(&http.Cookie{Name: "b", Value: "secret"})

	const want = "value=${cookie:b}"
	for i := 0; i < 1000; i++ {
		if got := BuildCacheKey("/frag", "value=${cookie:a}", r); got != want {
			t.Fatalf("iteration %d: expected %q, got %q", i, want, got)
		}
	}
}

func TestBuildCacheKey_URLValueNotRescanned(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("X-Test", "header-value")

	const url = "http://backend/${header:X-Test}"
	const want = "key:http://backend/${header:X-Test}"
	for i := 0; i < 1000; i++ {
		if got := BuildCacheKey(url, "key:${url}", r); got != want {
			t.Fatalf("iteration %d: expected %q, got %q", i, want, got)
		}
	}
}

func TestBuildCacheKey_SelfReferentialValueStaysLiteral(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("X-Test", "${header:X-Test}")

	if got := BuildCacheKey("/frag", "${header:X-Test}", r); got != "${header:X-Test}" {
		t.Errorf("expected the header value verbatim, got %q", got)
	}
}

func TestBuildCacheKey_CaseInsensitivePlaceholders(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("Accept-Language", "pl")
	r.AddCookie(&http.Cookie{Name: "SessionId", Value: "abc123"})

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"header canonical", "${header:Accept-Language}", "pl"},
		{"header lowercase", "${header:accept-language}", "pl"},
		{"header uppercase", "${header:ACCEPT-LANGUAGE}", "pl"},
		{"header mixed case", "${header:Accept-language}", "pl"},
		{"header random case", "${header:aCCePt-LaNgUaGe}", "pl"},
		{"cookie canonical", "${cookie:SessionId}", "abc123"},
		{"cookie lowercase", "${cookie:sessionid}", "abc123"},
		{"cookie uppercase", "${cookie:SESSIONID}", "abc123"},
		{"cookie mixed case", "${cookie:sEsSiOnId}", "abc123"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildCacheKey("/frag", tc.template, r); got != tc.want {
				t.Errorf("template %q: expected %q, got %q", tc.template, tc.want, got)
			}
		})
	}
}

func TestBuildCacheKey_NonCanonicalHeaderKeys(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	// Header keys inserted directly into the map may not be canonical.
	r.Header["x-custom"] = []string{"lowercase-key"}

	tests := []string{
		"${header:x-custom}",
		"${header:X-Custom}",
		"${header:x-CUSTOM}",
	}
	for _, template := range tests {
		if got := BuildCacheKey("/frag", template, r); got != "lowercase-key" {
			t.Errorf("template %q: expected %q, got %q", template, "lowercase-key", got)
		}
	}
}

func TestBuildCacheKey_AdjacentPlaceholders(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("A", "1")
	r.Header.Set("B", "2")
	r.AddCookie(&http.Cookie{Name: "C", Value: "3"})

	got := BuildCacheKey("/frag", "${header:A}${header:B}${cookie:C}${url}", r)
	if got != "123/frag" {
		t.Errorf("expected '123/frag', got %q", got)
	}
}

func TestBuildCacheKey_RepeatedPlaceholders(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("Accept-Language", "pl")

	got := BuildCacheKey("/frag", "${header:Accept-Language}-${header:accept-language}-${url}", r)
	if got != "pl-pl-/frag" {
		t.Errorf("expected 'pl-pl-/frag', got %q", got)
	}
}

func TestBuildCacheKey_UnknownPlaceholderWithMatchingHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	// A header whose name looks like the placeholder must not turn an unknown
	// placeholder into a known one.
	r.Header.Set("unknown", "bar")
	r.Header.Set("foo", "baz")
	r.Header.Set("Accept-Language", "pl")

	tests := []struct {
		template string
		want     string
	}{
		{"${unknown:foo}", "${unknown:foo}"},
		{"${unknown:foo}${header:Accept-Language}", "${unknown:foo}pl"},
		{"${unknown:unknown}", "${unknown:unknown}"},
		{"${foo:bar}", "${foo:bar}"},
		{"${url:foo}", "${url:foo}"},
		{"${URL}", "${URL}"},
		{"${url }", "${url }"},
		{"${header}", "${header}"},
		{"${cookie}", "${cookie}"},
		{"${ header:Accept-Language}", "${ header:Accept-Language}"},
		{"${header: Accept-Language}", "${header: Accept-Language}"},
	}

	for _, tc := range tests {
		if got := BuildCacheKey("/frag", tc.template, r); got != tc.want {
			t.Errorf("template %q: expected %q, got %q", tc.template, tc.want, got)
		}
	}
}

func TestBuildCacheKey_MalformedPlaceholders(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("Accept-Language", "pl")
	r.AddCookie(&http.Cookie{Name: "session", Value: "abc"})

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{"lone dollar brace", "${", "${"},
		{"trailing dollar brace", "a${", "a${"},
		{"unterminated url", "${url", "${url"},
		{"unterminated header", "${header:Accept-Language", "${header:Accept-Language"},
		{"unterminated cookie", "${cookie:session", "${cookie:session"},
		{"empty placeholder", "${}", "${}"},
		{"empty header name", "${header:}", "${header:}"},
		{"empty cookie name", "${cookie:}", "${cookie:}"},
		{"nested placeholder", "${header:${url}}", "${header:${url}}"},
		{"extra closing brace", "${url}}", "/frag}"},
		{"url then unterminated", "${url}${", "/frag${"},
		{"double dollar", "$${url}", "$/frag"},
		{"backslash", `\${url}`, `\/frag`},
		{"only a closing brace", "}", "}"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildCacheKey("/frag", tc.template, r); got != tc.want {
				t.Errorf("template %q: expected %q, got %q", tc.template, tc.want, got)
			}
		})
	}
}

func TestBuildCacheKey_EmptyNamesAreLiteral(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header[""] = []string{"empty-name"}

	tests := []string{"${}", "${header:}", "${cookie:}"}
	for _, template := range tests {
		if got := BuildCacheKey("/frag", template, r); got != template {
			t.Errorf("template %q: expected the literal, got %q", template, got)
		}
	}
}

func TestBuildCacheKey_HeaderWithoutValuesStaysLiteral(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header["X-Empty"] = []string{}
	r.Header["X-Nil"] = nil

	tests := []string{"${header:X-Empty}", "${header:X-Nil}"}
	for _, template := range tests {
		if got := BuildCacheKey("/frag", template, r); got != template {
			t.Errorf("template %q: expected the literal, got %q", template, got)
		}
	}
}

func TestBuildCacheKey_HeaderFirstValue(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header["X-Multi"] = []string{"first", "second"}

	if got := BuildCacheKey("/frag", "${header:X-Multi}", r); got != "first" {
		t.Errorf("expected the first header value, got %q", got)
	}
}

func TestBuildCacheKey_DuplicateCookieFirstValue(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("Cookie", "a=first; a=second")

	if got := BuildCacheKey("/frag", "${cookie:a}", r); got != "first" {
		t.Errorf("expected the first cookie value, got %q", got)
	}
}

func TestBuildCacheKey_NoPlaceholdersUnchanged(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)

	const template = "plain-$-{}-$x:{y}"
	if got := BuildCacheKey("/frag", template, r); got != template {
		t.Errorf("expected the template unchanged, got %q", got)
	}
}

func TestBuildCacheKey_MissingHeaderAndCookieStayLiteral(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)

	tests := []string{
		"${header:Missing}",
		"${cookie:missing}",
		"a${header:Missing}b${cookie:missing}c",
	}
	for _, template := range tests {
		if got := BuildCacheKey("/frag", template, r); got != template {
			t.Errorf("template %q: expected the literal, got %q", template, got)
		}
	}
}
