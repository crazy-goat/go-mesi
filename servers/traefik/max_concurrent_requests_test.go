package traefik

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Boundary classes for the `maxConcurrentRequests` option: every
// accepted edge (0, min, typical, accepted max) and every documented
// reject class gets its own subtest — see docs/workflow.md Rules
// ("Boundary classes ... each get a subtest").

func TestNewMaxConcurrentRequestsAcceptedValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cases := []struct {
		name  string
		value int
		want  int
	}{
		// 0 = "unlimited" (the core only installs the admission
		// semaphore when MaxConcurrentRequests > 0, mesi/parser.go:78)
		// — also the absent option's value.
		{name: "zero_unlimited", value: 0, want: 0},
		{name: "one_slot", value: 1, want: 1},
		{name: "typical_3", value: 3, want: 3},
		// Accepted max: 999999999, the #170 transport-derived cap
		// shared by Apache/nginx/php-ext/CLI/libgomesi.
		{name: "accepted_max_999999999", value: 999999999, want: 999999999},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(context.Background(), handler, &Config{MaxConcurrentRequests: tc.value}, "test")
			if err != nil {
				t.Fatalf("New with maxConcurrentRequests %d: %v", tc.value, err)
			}
			got := p.(*ResponsePlugin).maxConcurrentRequests
			if got != tc.want {
				t.Errorf("maxConcurrentRequests %d resolved to %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestNewMaxConcurrentRequestsRejectedValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cases := []struct {
		name  string
		value int
	}{
		// Negatives: the core would only warn
		// ("max_concurrent_requests_invalid") and normalize them to
		// 0 = unlimited (#329) — a malformed EXPLICIT value must
		// never silently pass as the documented "unlimited"
		// (project rule #1: no silent defaults; the same config-load
		// rejection as Apache #170, php-ext #206, CLI #192 and
		// nginx #214). The issue's proposal had no validation of
		// its own — this is the landed sibling contract.
		{name: "negative_one", value: -1},
		{name: "negative_five", value: -5},
		// 1000000000 = cap+1: the [0, 999999999] parity range of
		// every landed implementation (config.MaxMaxConcurrentRequests
		// / MESI_MAX_MAX_CONCURRENT_REQUESTS).
		{name: "rejected_max_plus_one_1000000000", value: 1000000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(context.Background(), handler, &Config{MaxConcurrentRequests: tc.value}, "test")
			if err == nil {
				t.Fatalf("expected error for maxConcurrentRequests %d, got nil", tc.value)
			}
			if !strings.Contains(err.Error(), "maxConcurrentRequests") {
				t.Errorf("expected error to name the maxConcurrentRequests option, got %v", err)
			}
		})
	}
}

func TestMaxConcurrentRequestsDecodeRejectsOverflowAndNonIntegers(t *testing.T) {
	// Values an int field CANNOT represent never reach New(): the
	// config decode into the typed field fails first, which also
	// fails middleware creation (traefik has no ParseConfig — a
	// decode error is loud). json.Unmarshal into the int field is
	// used here as a stand-in for traefik's real pipeline (file
	// provider stringifies every scalar, then tagless mapstructure
	// coerces into the field): both share the int type constraint
	// being proven, and the reject classes were verified to fail
	// loud in that real pipeline end-to-end — the same empirical
	// finding #210 documented for the int64 `maxResponseSize` field.
	cases := []struct {
		name  string
		value string
	}{
		// 20 digits: unrepresentable in any int (MaxInt64 has 19).
		{name: "overflow_20_digits", value: "99999999999999999999"},
		// Grammar classes: decimals and non-integers must not coerce.
		{name: "decimal", value: "1.5"},
		{name: "not_a_number", value: `"abc"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(`{"maxConcurrentRequests":`+tc.value+`}`), &cfg)
			if err == nil {
				t.Fatalf("expected decode error for maxConcurrentRequests %s, got nil (value %d)", tc.value, cfg.MaxConcurrentRequests)
			}
			if !strings.Contains(err.Error(), "maxConcurrentRequests") {
				t.Errorf("expected decode error to name the maxConcurrentRequests field, got %v", err)
			}
		})
	}
}

func TestMaxConcurrentRequestsAbsentStaysUnlimited(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	// Absent option: traefik decodes YAML over CreateConfig()'s
	// defaults, which deliberately do NOT seed this field — the
	// value stays 0 = unlimited, byte-identical to the pre-#215
	// behaviour, where ServeHTTP's EsiParserConfig literal never
	// set the field (and mesi.CreateDefaultConfig(), which also
	// leaves it at 0, is never called by this plugin).
	cfg := CreateConfig()
	if cfg.MaxConcurrentRequests != 0 {
		t.Fatalf("CreateConfig MaxConcurrentRequests = %d, want 0 (unseeded — absent must stay unlimited)", cfg.MaxConcurrentRequests)
	}
	p, err := New(context.Background(), handler, cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.(*ResponsePlugin).maxConcurrentRequests; got != 0 {
		t.Errorf("absent maxConcurrentRequests resolved to %d, want 0 (unlimited)", got)
	}
	// Direct construction (nil-free struct) behaves the same.
	p2, err := New(context.Background(), handler, &Config{}, "test")
	if err != nil {
		t.Fatalf("New with zero Config: %v", err)
	}
	if got := p2.(*ResponsePlugin).maxConcurrentRequests; got != 0 {
		t.Errorf("zero-value maxConcurrentRequests resolved to %d, want 0 (unlimited)", got)
	}
}

// mcrGauge is the in-test analogue of the test-server's /hold + /track
// endpoints (servers/test-server/test-server.go:46-77, the same
// TRACK_LOCK design as servers/nginx/tests/server.py #214 and
// servers/apache/tests/server.py #170): a fragment request registers
// itself BEFORE sleeping and records the peak, so peak() is the
// maximum number of fragment fetches that had STARTED-but-not-finished
// at any one time — a deterministic, timing-free observable instead of
// a wall-clock assertion.
type mcrGauge struct {
	mu      sync.Mutex
	current int
	peak    int
}

func (g *mcrGauge) enter() {
	g.mu.Lock()
	g.current++
	if g.current > g.peak {
		g.peak = g.current
	}
	g.mu.Unlock()
}

func (g *mcrGauge) leave() {
	g.mu.Lock()
	g.current--
	g.mu.Unlock()
}

func (g *mcrGauge) peakConcurrency() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

// mcrFragmentServer serves /hold/<i>: it registers the request in the
// peak-concurrency gauge, holds for `hold`, then returns a
// "HELD-FRAG<i> Held" fragment body. Distinct paths give every
// <esi:include> of the page its own URL, and the shared "HELD-FRAG"
// prefix lets the test count delivered fragments with strings.Count.
func mcrFragmentServer(g *mcrGauge, hold time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.enter()
		defer g.leave()
		time.Sleep(hold)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("HELD-FRAG" + strings.TrimPrefix(r.URL.Path, "/hold/") + " Held"))
	}))
}

// mcrPage serves an HTML page whose n <esi:include> tags target the
// gauge fragment server (one distinct /hold/<i> URL each), bracketed
// by markers so a test can prove the render reached AND survived the
// include site (the esiPageWithInclude pattern from
// max_response_size_test.go, fanned out to n includes).
func mcrPage(frag *httptest.Server, n int) http.HandlerFunc {
	var b strings.Builder
	b.WriteString(`<html><body><h1>MCR-PAGE</h1>`)
	for i := 0; i < n; i++ {
		b.WriteString(`<esi:include src="` + frag.URL + `/hold/` + strconv.Itoa(i) + `" />`)
	}
	b.WriteString(`<p>After mcr include</p></body></html>`)
	body := b.String()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}
}

// Fan-out bound for the "unlimited" cases: MESIParse drains includes
// through a worker pool of min(MaxWorkers=NumCPU*4, n) goroutines
// (mesi/parser.go:118-129, traefik never sets MaxWorkers), i.e. at
// least 4 goroutines for a 20-include page — with a 500 ms hold an
// uncapped parse must show peak >= 4, while a cap of 3 can never
// exceed 3 (hard semaphore invariant, mesi/fetch.go:148-154).
const (
	mcrIncludeCount  = 20
	mcrFragmentHold  = 500 * time.Millisecond
	mcrUnlimitedPeak = 4
)

func TestServeHTTPMaxConcurrentRequestsCap3Funnels20Includes(t *testing.T) {
	// AC: "20 includes with limit 3 → funneled". Deterministic proof
	// via the peak-concurrency gauge — mirrors nginx #214 Test 57 and
	// Apache #170 Test 36, which assert the semaphore invariant
	// directly instead of timing (an exact peak == 3 would
	// additionally require all three first-wave dials to overlap —
	// scheduling-dependent, deliberately not asserted; peak <= 3 is
	// the hard cap, peak >= 2 proves a multi-slot queue rather than
	// a serialisation to 1). All 20 fragments must arrive: includes
	// beyond the cap WAIT for a slot, never dropped.
	g := &mcrGauge{}
	frag := mcrFragmentServer(g, mcrFragmentHold)
	defer frag.Close()

	p, err := New(context.Background(), mcrPage(frag, mcrIncludeCount), &Config{
		MaxConcurrentRequests: 3,
		BlockPrivateIPs:       false, // loopback httptest backend — keep the dial-time SSRF filter off (#494)
	}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	body := rec.Body.String()

	peak := g.peakConcurrency()
	if peak > 3 {
		t.Errorf("peak concurrent fetches %d > cap 3 — the admission semaphore did not funnel the fan-out", peak)
	}
	if peak < 2 {
		t.Errorf("peak concurrent fetches %d < 2 — cap 3 should queue in parallel slots, not serialise to 1", peak)
	}
	if fragments := strings.Count(body, "HELD-FRAG"); fragments != mcrIncludeCount {
		t.Errorf("delivered %d fragments, want %d (includes beyond the cap must WAIT, never drop)", fragments, mcrIncludeCount)
	}
	if strings.Contains(body, "<esi:include") {
		t.Errorf("raw <esi:include> tag left in response: %q", body[:min(len(body), 300)])
	}
	if !strings.Contains(body, "MCR-PAGE") || !strings.Contains(body, "After mcr include") {
		t.Errorf("page markers missing — the render itself broke: %q", body[:min(len(body), 300)])
	}
}

func TestServeHTTPMaxConcurrentRequestsExplicitZeroUnlimited(t *testing.T) {
	// Explicit 0 = the documented "unlimited": no semaphore is
	// installed, so the full worker-pool fan-out shows up at the
	// gauge (peak >= 4 — the same discriminator nginx Test 58 and
	// Apache Test 37 use against the cap-3 case), and all 20
	// fragments still arrive.
	g := &mcrGauge{}
	frag := mcrFragmentServer(g, mcrFragmentHold)
	defer frag.Close()

	p, err := New(context.Background(), mcrPage(frag, mcrIncludeCount), &Config{
		MaxConcurrentRequests: 0,
		BlockPrivateIPs:       false, // loopback httptest backend — keep the dial-time SSRF filter off (#494)
	}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	body := rec.Body.String()

	peak := g.peakConcurrency()
	if peak < mcrUnlimitedPeak {
		t.Errorf("peak concurrent fetches %d < %d under explicit maxConcurrentRequests 0 (unlimited)", peak, mcrUnlimitedPeak)
	}
	if fragments := strings.Count(body, "HELD-FRAG"); fragments != mcrIncludeCount {
		t.Errorf("delivered %d fragments, want %d", fragments, mcrIncludeCount)
	}
	if strings.Contains(body, "<esi:include") {
		t.Errorf("raw <esi:include> tag left in response: %q", body[:min(len(body), 300)])
	}
}

func TestServeHTTPMaxConcurrentRequestsAbsentUnlimited(t *testing.T) {
	// Absent option: CreateConfig()'s defaults never seed the field,
	// and ServeHTTP's EsiParserConfig literal only forwards the
	// resolved 0 — the parse is unthrottled, byte-identical to the
	// pre-#215 behaviour (the decisive absent-behaviour proof: the
	// cap-3 case above shows peak <= 3 for the same page).
	g := &mcrGauge{}
	frag := mcrFragmentServer(g, mcrFragmentHold)
	defer frag.Close()

	cfg := CreateConfig() // option absent — traefik decodes YAML over these defaults
	// The secure BlockPrivateIPs default (true) would block the
	// loopback fragment at dial time in plain Go (the real filter —
	// only Yaegi stubs it), hiding the concurrency behaviour under
	// test; disable it so absent-maxConcurrentRequests is what the
	// test observes (the same loopback opt-out every fetch unit test
	// here makes, #494).
	cfg.BlockPrivateIPs = false

	p, err := New(context.Background(), mcrPage(frag, mcrIncludeCount), cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	body := rec.Body.String()

	peak := g.peakConcurrency()
	if peak < mcrUnlimitedPeak {
		t.Errorf("peak concurrent fetches %d < %d with absent maxConcurrentRequests (must stay unlimited)", peak, mcrUnlimitedPeak)
	}
	if fragments := strings.Count(body, "HELD-FRAG"); fragments != mcrIncludeCount {
		t.Errorf("delivered %d fragments, want %d", fragments, mcrIncludeCount)
	}
	if strings.Contains(body, "<esi:include") {
		t.Errorf("raw <esi:include> tag left in response: %q", body[:min(len(body), 300)])
	}
}
