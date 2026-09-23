package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Boundary classes for the `timeout` option: every accepted edge (min,
// max) and every documented reject class gets its own subtest — see
// docs/workflow.md Rules ("Boundary classes ... each get a subtest").

func TestNewTimeoutAcceptedValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "minimum_1s", value: "1s", want: time.Second},
		{name: "typical_5s", value: "5s", want: 5 * time.Second},
		{name: "minutes_1m", value: "1m", want: time.Minute},
		{name: "composite_1h30m", value: "1h30m", want: 90 * time.Minute},
		{name: "decimal_1500ms", value: "1500ms", want: 1500 * time.Millisecond},
		{name: "maximum_24h", value: "24h", want: 24 * time.Hour},
		{name: "maximum_86400s", value: "86400s", want: 24 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(context.Background(), handler, &Config{Timeout: strPtr(tc.value)}, "test")
			if err != nil {
				t.Fatalf("New with timeout %q: %v", tc.value, err)
			}
			got := p.(*ResponsePlugin).timeout
			if got != tc.want {
				t.Errorf("timeout %q resolved to %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestNewTimeoutRejectedValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cases := []struct {
		name  string
		value string
	}{
		// The issue's ACs proposed "" and "abc" falling back to the
		// default — false ACs: a malformed EXPLICIT value must never
		// be silently replaced (project rule #1: no silent defaults).
		{name: "explicit_empty", value: ""},
		{name: "not_a_duration", value: "abc"},
		// 0 / negatives: Timeout <= 0 fails EVERY include with
		// ErrTimeBudgetExceeded (mesi/fetch.go) — it is not
		// "unlimited", so it can never be a valid budget.
		{name: "zero_0s", value: "0s"},
		{name: "negative_seconds", value: "-1s"},
		{name: "negative_minutes", value: "-5m"},
		// Below the landed [1s, 24h] range (config.ValidateTimeout's
		// [1, 86400] seconds).
		{name: "below_minimum_999ms", value: "999ms"},
		{name: "below_minimum_500ms", value: "500ms"},
		{name: "above_cap_86401s", value: "86401s"},
		{name: "above_cap_25h", value: "25h"},
		// Grammar classes: plain integers lack a unit (rejected by
		// Caddy's ParseDuration too — no hybrid seconds reinterpretation),
		// trailing garbage and overflow do not parse.
		{name: "missing_unit_integer", value: "15"},
		{name: "trailing_garbage", value: "3foo"},
		{name: "overflow", value: "999999999999999999999s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(context.Background(), handler, &Config{Timeout: strPtr(tc.value)}, "test")
			if err == nil {
				t.Fatalf("expected error for timeout %q, got nil", tc.value)
			}
			if !strings.Contains(err.Error(), "timeout") {
				t.Errorf("expected error to name the timeout option, got %v", err)
			}
		})
	}
}

func TestNewTimeoutUnsetDefaultsToTenSeconds(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	// Absent option: traefik decodes YAML over CreateConfig()'s defaults,
	// which seed the historical 10s.
	cfg := CreateConfig()
	if cfg.Timeout == nil || *cfg.Timeout != "10s" {
		t.Fatalf("CreateConfig Timeout = %v, want pointer to \"10s\"", cfg.Timeout)
	}
	p, err := New(context.Background(), handler, cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.(*ResponsePlugin).timeout; got != 10*time.Second {
		t.Errorf("absent timeout resolved to %v, want 10s", got)
	}
}

func TestNewTimeoutNilDefaultsToTenSeconds(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	// Direct construction: nil is the "unset" sentinel (same pattern as
	// MaxDepth *int).
	p, err := New(context.Background(), handler, &Config{}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.(*ResponsePlugin).timeout; got != 10*time.Second {
		t.Errorf("nil timeout resolved to %v, want 10s", got)
	}
}

func TestServeHTTPTimeoutAbortsSlowInclude(t *testing.T) {
	frag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("SLOW-FRAGMENT"))
	}))
	defer frag.Close()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + frag.URL + `/slow" /></body></html>`))
	})

	p, err := New(context.Background(), next, &Config{Timeout: strPtr("1s")}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	elapsed := time.Since(start)

	// Floor: the fetch was in flight for roughly the configured budget —
	// a ~0s budget would fail instantly (ErrTimeBudgetExceeded wiring bug).
	if elapsed < 700*time.Millisecond {
		t.Errorf("aborted after %v, expected ~1s budget (floor 700ms)", elapsed)
	}
	// Ceiling: the budget beat the 3s sleep — the hardcoded 10s default
	// (or a lost config value) would let the include complete at ~3s.
	if elapsed > 2500*time.Millisecond {
		t.Errorf("took %v, expected abort at ~1s (ceiling 2.5s)", elapsed)
	}
	body := rec.Body.String()
	if strings.Contains(body, "SLOW-FRAGMENT") {
		t.Errorf("fragment delivered despite 1s budget: %q", body)
	}
	if strings.Contains(body, "<esi:include") {
		t.Errorf("raw include tag left in response: %q", body)
	}
}

func TestServeHTTPTimeoutAllowsIncludeWithinBudget(t *testing.T) {
	frag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("FAST-FRAGMENT"))
	}))
	defer frag.Close()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + frag.URL + `/fast" /></body></html>`))
	})

	p, err := New(context.Background(), next, &Config{Timeout: strPtr("5s")}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("took %v, expected an include within the 5s budget", elapsed)
	}
	if body := rec.Body.String(); !strings.Contains(body, "FAST-FRAGMENT") {
		t.Errorf("fragment missing within budget: %q", body)
	}
}
