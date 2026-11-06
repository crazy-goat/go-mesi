package mesi

import (
	"net/http"
	"strings"
)

// BuildCacheKey evaluates a cache key template string, substituting placeholders
// with values from the URL and HTTP request.
//
// Supported placeholders:
//   - ${url}            — the include URL
//   - ${header:Name}    — request header value (case-insensitive name)
//   - ${cookie:Name}    — cookie value (case-insensitive name)
//
// Unknown and malformed placeholders are left as literals.
//
// The template is scanned once, left to right. Substituted text (the URL, a
// header value or a cookie value) is written to the result verbatim and never
// rescanned, so a value that looks like a placeholder cannot inject another
// placeholder into the key, and the result does not depend on the iteration
// order of the request header map.
func BuildCacheKey(url string, template string, r *http.Request) string {
	if !strings.Contains(template, "${") {
		return template
	}

	var b strings.Builder
	b.Grow(len(template))

	rest := template
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])
		rest = rest[start:]

		end := strings.IndexByte(rest[2:], '}')
		if end < 0 {
			// Unterminated placeholder: keep the rest literal.
			b.WriteString(rest)
			break
		}
		end += 2 // index of '}' in rest

		if value, ok := cacheKeyPlaceholder(url, rest[2:end], r); ok {
			b.WriteString(value)
		} else {
			b.WriteString(rest[:end+1])
		}
		rest = rest[end+1:]
	}

	return b.String()
}

// cacheKeyPlaceholder resolves one placeholder body (the text between "${" and
// "}"). It reports false when the placeholder is unknown, so the caller can
// keep it literal.
func cacheKeyPlaceholder(url string, name string, r *http.Request) (string, bool) {
	switch {
	case name == "url":
		return url, true
	case strings.HasPrefix(name, "header:"):
		return requestHeaderValue(r, name[len("header:"):])
	case strings.HasPrefix(name, "cookie:"):
		return requestCookieValue(r, name[len("cookie:"):])
	default:
		return "", false
	}
}

// requestHeaderValue returns the first value of the named request header,
// matching the name case-insensitively. Normal http.Header maps are canonical,
// but keys inserted directly into the map are matched too.
func requestHeaderValue(r *http.Request, name string) (string, bool) {
	if r == nil || name == "" {
		return "", false
	}

	candidates := [2]string{http.CanonicalHeaderKey(name), name}
	for _, key := range candidates {
		if key == "" {
			continue
		}
		if value, ok := firstHeaderValue(r.Header[key]); ok {
			return value, true
		}
	}

	// A manually populated map can hold several keys that differ only in case.
	// Pick the lexicographically smallest one so the result is deterministic.
	matched := ""
	found := false
	for key := range r.Header {
		if !strings.EqualFold(key, name) {
			continue
		}
		if !found || key < matched {
			matched = key
			found = true
		}
	}
	if !found {
		return "", false
	}
	return firstHeaderValue(r.Header[matched])
}

func firstHeaderValue(values []string) (string, bool) {
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// requestCookieValue returns the value of the named request cookie, matching the
// name case-insensitively. An exact name match wins, in Cookie-header order,
// which keeps the historical behaviour for duplicate cookies.
func requestCookieValue(r *http.Request, name string) (string, bool) {
	if r == nil || name == "" {
		return "", false
	}

	cookies := r.Cookies()
	for _, c := range cookies {
		if c.Name == name {
			return c.Value, true
		}
	}

	matched := ""
	value := ""
	found := false
	for _, c := range cookies {
		if !strings.EqualFold(c.Name, name) {
			continue
		}
		if !found || c.Name < matched {
			matched = c.Name
			value = c.Value
			found = true
		}
	}
	return value, found
}
