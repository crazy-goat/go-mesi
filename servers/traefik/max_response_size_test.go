package traefik

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Boundary classes for the `maxResponseSize` option: every accepted
// edge (0, min, typical, accepted max) and every documented reject
// class gets its own subtest — see docs/workflow.md Rules
// ("Boundary classes ... each get a subtest").

func TestNewMaxResponseSizeAcceptedValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cases := []struct {
		name  string
		value int64
		want  int64
	}{
		// 0 = "unlimited" (mesi/fetch.go only limits when
		// MaxResponseSize > 0) — also the absent option's value.
		{name: "zero_unlimited", value: 0, want: 0},
		{name: "one_byte", value: 1, want: 1},
		{name: "typical_1MiB", value: 1048576, want: 1048576},
		// Accepted max: math.MaxInt64-1 — the largest value whose
		// `+1` io.LimitReader bound in the core stays positive.
		{name: "accepted_max_MaxInt64_minus_1", value: math.MaxInt64 - 1, want: math.MaxInt64 - 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(context.Background(), handler, &Config{MaxResponseSize: tc.value}, "test")
			if err != nil {
				t.Fatalf("New with maxResponseSize %d: %v", tc.value, err)
			}
			got := p.(*ResponsePlugin).maxResponseSize
			if got != tc.want {
				t.Errorf("maxResponseSize %d resolved to %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestNewMaxResponseSizeRejectedValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cases := []struct {
		name  string
		value int64
	}{
		// Negatives: the core's `> 0` check would silently treat
		// them exactly like 0 (unlimited) — a malformed EXPLICIT
		// value must never pass as a documented one (the issue's
		// implicit "anything invalid -> default" sketch is a false
		// AC; project rule #1: no silent defaults).
		{name: "negative_one", value: -1},
		{name: "negative_five", value: -5},
		// math.MaxInt64 (= cap+1): the core computes
		// MaxResponseSize+1 for io.LimitReader (mesi/fetch.go) — at
		// MaxInt64 that wraps negative and the include would
		// silently render an empty body instead of failing (#448).
		{name: "rejected_max_MaxInt64", value: math.MaxInt64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(context.Background(), handler, &Config{MaxResponseSize: tc.value}, "test")
			if err == nil {
				t.Fatalf("expected error for maxResponseSize %d, got nil", tc.value)
			}
			if !strings.Contains(err.Error(), "maxResponseSize") {
				t.Errorf("expected error to name the maxResponseSize option, got %v", err)
			}
		})
	}
}

func TestMaxResponseSizeDecodeRejectsOverflowAndNonIntegers(t *testing.T) {
	// Values an int64 field CANNOT represent never reach New(): the
	// config decode into the typed field fails first, which also fails
	// middleware creation (traefik has no ParseConfig — a decode error
	// is loud). The `json` tag here mirrors the `yaml` tag traefik
	// uses: both decode into the same int64, so both share the type
	// constraint being proven.
	cases := []struct {
		name  string
		value string
	}{
		// 9223372036854775808 = math.MaxInt64 + 1 (unrepresentable).
		{name: "maxint64_plus_one", value: "9223372036854775808"},
		// Overflow-ish: far beyond int64.
		{name: "overflow_23_digits", value: "99999999999999999999999"},
		// Grammar classes: decimals and non-integers must not coerce.
		{name: "decimal", value: "1.5"},
		{name: "not_a_number", value: `"abc"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(`{"maxResponseSize":`+tc.value+`}`), &cfg)
			if err == nil {
				t.Fatalf("expected decode error for maxResponseSize %s, got nil (value %d)", tc.value, cfg.MaxResponseSize)
			}
			if !strings.Contains(err.Error(), "maxResponseSize") {
				t.Errorf("expected decode error to name the maxResponseSize field, got %v", err)
			}
		})
	}
}

func TestMaxResponseSizeAbsentStaysUnlimited(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	// Absent option: traefik decodes YAML over CreateConfig()'s
	// defaults, which deliberately do NOT seed this field — the value
	// stays 0 = unlimited, byte-identical to the pre-#210 behaviour,
	// where ServeHTTP's EsiParserConfig literal never set the field.
	// There is NO implicit 10 MB: the 10 * 1024 * 1024 of
	// mesi.CreateDefaultConfig() (mesi/config.go:106) only reaches Go
	// callers of that constructor, which this plugin never is (the
	// issue's "default 10 MB" premise is false for traefik too).
	cfg := CreateConfig()
	if cfg.MaxResponseSize != 0 {
		t.Fatalf("CreateConfig MaxResponseSize = %d, want 0 (unseeded — absent must stay unlimited)", cfg.MaxResponseSize)
	}
	p, err := New(context.Background(), handler, cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.(*ResponsePlugin).maxResponseSize; got != 0 {
		t.Errorf("absent maxResponseSize resolved to %d, want 0 (unlimited)", got)
	}
	// Direct construction (nil-free struct) behaves the same.
	p2, err := New(context.Background(), handler, &Config{}, "test")
	if err != nil {
		t.Fatalf("New with zero Config: %v", err)
	}
	if got := p2.(*ResponsePlugin).maxResponseSize; got != 0 {
		t.Errorf("zero-value maxResponseSize resolved to %d, want 0 (unlimited)", got)
	}
}

// sizedFragment serves a body of exactly size bytes whose FIRST bytes
// are marker: truncation (wrong behaviour) would still expose the
// marker, while fail-closed rejection removes the body entirely — so
// marker-absence proves rejection, not truncation (mirrors nginx's #208
// /bytes fixture semantics).
func sizedFragment(marker string, size int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		body := make([]byte, size)
		for i := range body {
			body[i] = 'x'
		}
		copy(body, marker)
		w.Write(body)
	}))
}

// esiPageWithInclude serves an HTML page whose single <esi:include>
// targets frag+path, bracketed by markers before and after the include
// so a test can prove the render reached AND survived the include site.
func esiPageWithInclude(frag *httptest.Server, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><h1>PAGE-MARKER</h1><esi:include src="` + frag.URL + path + `" /><p>AFTER-INCLUDE</p></body></html>`))
	}
}

func TestServeHTTPMaxResponseSizeRejectsOversizedInclude(t *testing.T) {
	// AC: 100-byte limit -> 200-byte include rejected.
	frag := sizedFragment("MesiBytesPayload 200 ", 200)
	defer frag.Close()

	p, err := New(context.Background(), esiPageWithInclude(frag, "/frag"), &Config{MaxResponseSize: 100}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	body := rec.Body.String()

	if strings.Contains(body, "MesiBytesPayload") {
		t.Errorf("over-limit fragment delivered (or truncated) despite 100-byte cap — fail closed expected: %q", body[:min(len(body), 200)])
	}
	if strings.Contains(body, "<esi:include") {
		t.Errorf("raw <esi:include> tag left in response: %q", body)
	}
	if !strings.Contains(body, "PAGE-MARKER") {
		t.Errorf("page marker missing — the render itself broke: %q", body)
	}
	if !strings.Contains(body, "AFTER-INCLUDE") {
		t.Errorf("content after the include missing — the render died at the include site: %q", body)
	}
}

func TestServeHTTPMaxResponseSizeRejectRendersIncludeErrorMarker(t *testing.T) {
	// Same rejection, but with an explicit IncludeErrorMarker: the
	// over-limit include must surface through the core's include-error
	// path (mesi/include.go) as the marker — not as a truncated body.
	frag := sizedFragment("MesiBytesPayload 200 ", 200)
	defer frag.Close()

	cfg := &Config{MaxResponseSize: 100, IncludeErrorMarker: "<!-- esi error -->"}
	p, err := New(context.Background(), esiPageWithInclude(frag, "/frag"), cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "<!-- esi error -->") {
		t.Errorf("IncludeErrorMarker not rendered for the over-limit include: %q", body)
	}
	if strings.Contains(body, "MesiBytesPayload") {
		t.Errorf("over-limit fragment delivered despite 100-byte cap: %q", body)
	}
}

func TestServeHTTPMaxResponseSizeAllowsWithinLimit(t *testing.T) {
	// Large-enough cap delivers the include in full (AC counterpart:
	// a limit that fits the body must not interfere).
	const fragSize = 4096
	frag := sizedFragment("MesiBytesPayload 4096 ", fragSize)
	defer frag.Close()

	p, err := New(context.Background(), esiPageWithInclude(frag, "/frag"), &Config{MaxResponseSize: 1048576}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "MesiBytesPayload 4096") {
		t.Errorf("fragment missing under a 1 MiB cap: %q", body[:min(len(body), 200)])
	}
	if len(rec.Body.Bytes()) < fragSize {
		t.Errorf("response body %d bytes, want >= %d (fragment delivered in full)", len(rec.Body.Bytes()), fragSize)
	}
	if strings.Contains(body, "<esi:include") {
		t.Errorf("raw <esi:include> tag left in response: %q", body)
	}
	if !strings.Contains(body, "AFTER-INCLUDE") {
		t.Errorf("content after the include missing: %q", body)
	}
}

func TestServeHTTPMaxResponseSizeAbsentDeliversBeyondTenMB(t *testing.T) {
	// The decisive absent-behaviour proof: 10 MB + 1 byte. The issue's
	// assumed implicit 10 MB default would REJECT this include, so a
	// full delivery pins "absent -> 0 -> unlimited" byte-compatible
	// with pre-#210 behaviour (ServeHTTP never called
	// mesi.CreateDefaultConfig()). Explicit 0 resolves to the same
	// field value and is therefore byte-identical.
	const fragSize = 10*1024*1024 + 1
	frag := sizedFragment("MesiBytesPayload 10485761 ", fragSize)
	defer frag.Close()

	cfg := CreateConfig() // option absent — traefik decodes YAML over these defaults
	// The secure BlockPrivateIPs default (true) would block the
	// loopback fragment at dial time in plain Go (the real filter —
	// only Yaegi stubs it), hiding the SIZE behaviour under test;
	// disable it so absent-maxResponseSize is what the test observes
	// (the same loopback opt-out every fetch unit test here makes).
	cfg.BlockPrivateIPs = false

	p, err := New(context.Background(), esiPageWithInclude(frag, "/frag"), cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	body := rec.Body.String()

	if !strings.Contains(body, "MesiBytesPayload 10485761") {
		t.Errorf("10 MB + 1 byte include rejected — absent maxResponseSize is NOT unlimited (implicit 10 MB leaked in?)")
	}
	if rec.Body.Len() < fragSize {
		t.Errorf("response body %d bytes, want >= %d (fragment delivered in full)", rec.Body.Len(), fragSize)
	}
	if strings.Contains(body, "<esi:include") {
		t.Errorf("raw <esi:include> tag left in response")
	}
	if !strings.Contains(body, "AFTER-INCLUDE") {
		t.Errorf("content after the include missing — the render died at the include site")
	}
}
