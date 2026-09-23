package roadrunner

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
)

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()
	if config.MaxDepth == nil || *config.MaxDepth != 5 {
		t.Errorf("Expected MaxDepth 5, got %v", config.MaxDepth)
	}
}

func TestInitDefaults(t *testing.T) {
	p := &Plugin{}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.config.MaxDepth == nil || *p.config.MaxDepth != 5 {
		t.Errorf("Expected MaxDepth 5, got %v", p.config.MaxDepth)
	}
	if p.timeout != DefaultTimeout {
		t.Errorf("Expected timeout %s, got %s", DefaultTimeout, p.timeout)
	}
	if p.cache != nil {
		t.Error("Expected nil cache with default config")
	}
	if p.config.MaxResponseSize != DefaultMaxResponseSize {
		t.Errorf("Expected max response size %d (unlimited), got %d", DefaultMaxResponseSize, p.config.MaxResponseSize)
	}
}

func TestInitMaxResponseSizeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int64
		wantErr bool
	}{
		{name: "zero_is_unlimited", size: 0},
		{name: "one_byte", size: 1},
		{name: "typical_limit", size: 1048576},
		{name: "accepted_max", size: MaxMaxResponseSize},
		{name: "negative", size: -1, wantErr: true},
		{name: "max_int64_overflow_boundary", size: MaxMaxResponseSize + 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plugin{config: &Config{MaxResponseSize: tc.size}}
			err := p.Init()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected max_response_size %d to fail", tc.size)
				}
				if !strings.Contains(err.Error(), "max_response_size") {
					t.Errorf("expected error to name max_response_size, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Init with max_response_size %d: %v", tc.size, err)
			}
		})
	}
}

func TestInitMaxWorkersBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   int
		wantErr bool
	}{
		{name: "zero_uses_library_default", value: 0},
		{name: "one_worker", value: 1},
		{name: "typical_limit", value: 8},
		{name: "accepted_max", value: MaxMaxWorkers},
		{name: "negative_one", value: -1, wantErr: true},
		{name: "negative_multiple", value: -5, wantErr: true},
		{name: "max_plus_one", value: MaxMaxWorkers + 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plugin{config: &Config{MaxWorkers: tc.value}}
			err := p.Init()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected max_workers %d to fail", tc.value)
				}
				if !strings.Contains(err.Error(), "max_workers") {
					t.Errorf("expected error to name max_workers, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Init with max_workers %d: %v", tc.value, err)
			}
			if p.config.MaxWorkers != tc.value {
				t.Errorf("MaxWorkers = %d, want configured value %d", p.config.MaxWorkers, tc.value)
			}
		})
	}
}

func TestInitMaxConcurrentRequestsBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   int
		wantErr bool
	}{
		{name: "zero_is_unlimited", value: 0},
		{name: "one_slot", value: 1},
		{name: "typical_limit", value: 3},
		{name: "accepted_max", value: MaxMaxConcurrentRequests},
		{name: "negative_one", value: -1, wantErr: true},
		{name: "negative_multiple", value: -5, wantErr: true},
		{name: "max_plus_one", value: MaxMaxConcurrentRequests + 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plugin{config: &Config{MaxConcurrentRequests: tc.value}}
			err := p.Init()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected max_concurrent_requests %d to fail", tc.value)
				}
				if !strings.Contains(err.Error(), "max_concurrent_requests") {
					t.Errorf("expected error to name max_concurrent_requests, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Init with max_concurrent_requests %d: %v", tc.value, err)
			}
		})
	}
}

func TestParseTimeoutDefaultValue(t *testing.T) {
	got, err := parseTimeout("10s")
	if err != nil {
		t.Fatalf("parseTimeout(10s): %v", err)
	}
	if got != DefaultTimeout {
		t.Errorf("parseTimeout(10s) = %s, want default %s", got, DefaultTimeout)
	}
}

func TestParseTimeoutAcceptedValues(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "minimum_1s", value: "1s", want: time.Second},
		{name: "typical_5s", value: "5s", want: 5 * time.Second},
		{name: "minutes_1m", value: "1m", want: time.Minute},
		{name: "maximum_24h", value: "24h", want: 24 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTimeout(tc.value)
			if err != nil {
				t.Fatalf("parseTimeout(%q): %v", tc.value, err)
			}
			if got != tc.want {
				t.Errorf("parseTimeout(%q) = %s, want %s", tc.value, got, tc.want)
			}
		})
	}
}

func TestParseTimeoutRejectedValues(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{name: "explicit_empty", value: ""},
		{name: "zero", value: "0s"},
		{name: "negative", value: "-1s"},
		{name: "subsecond", value: "999ms"},
		{name: "above_maximum_seconds", value: "86401s"},
		{name: "above_maximum_duration", value: "25h"},
		{name: "missing_unit", value: "15"},
		{name: "decimal", value: "1.5"},
		{name: "trailing_garbage", value: "3foo"},
		{name: "not_a_duration", value: "abc"},
		{name: "overflow", value: "999999999999999999999s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTimeout(tc.value)
			if err == nil {
				t.Fatalf("expected parseTimeout(%q) to fail", tc.value)
			}
			if !strings.Contains(err.Error(), "timeout") {
				t.Errorf("expected error to name timeout, got %v", err)
			}
		})
	}
}

func TestInitRejectsInvalidTimeout(t *testing.T) {
	for _, value := range []string{"abc", "0s", "25h"} {
		t.Run(value, func(t *testing.T) {
			p := &Plugin{config: &Config{Timeout: value}}
			if err := p.Init(); err == nil {
				t.Fatalf("expected Init to reject timeout %q", value)
			}
		})
	}
}

func TestInitMemoryCache(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheSize = 100

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestInitMemoryCacheDefaultSize(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Fatal("Expected non-nil cache")
	}
}

func TestInitInvalidCacheTTL(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "invalid"

	p := &Plugin{config: config}
	if err := p.Init(); err == nil {
		t.Fatal("Expected error for invalid cache TTL")
	}
}

func TestInitCacheBackendWithoutTTL(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.cache == nil {
		t.Error("Expected non-nil cache for memory backend even without TTL")
	}
	if p.cacheTTL != 0 {
		t.Errorf("Expected zero cacheTTL when CacheTTL is empty, got %v", p.cacheTTL)
	}
}

func TestName(t *testing.T) {
	p := &Plugin{}
	if p.Name() != "mesi" {
		t.Errorf("Expected name 'mesi', got %s", p.Name())
	}
}

func TestClose(t *testing.T) {
	p := &Plugin{}
	if err := p.Close(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestMiddlewareUsesConfiguredTimeout(t *testing.T) {
	fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("SLOW_FRAGMENT"))
	}))
	t.Cleanup(fragment.Close)

	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><esi:include src="` + fragment.URL + `/slow" /></body></html>`))
	})
	blockPrivateIPs := false
	p := &Plugin{config: &Config{Timeout: "1s", BlockPrivateIPs: &blockPrivateIPs}}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	start := time.Now()
	rec := httptest.NewRecorder()
	p.Middleware(upstream).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	elapsed := time.Since(start)

	if elapsed < 700*time.Millisecond || elapsed > 2500*time.Millisecond {
		t.Errorf("configured 1s timeout took %v; want approximately 1s and below the backend's 3s delay", elapsed)
	}
	if strings.Contains(rec.Body.String(), "SLOW_FRAGMENT") {
		t.Errorf("slow fragment was included despite timeout: %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<esi:include") {
		t.Errorf("raw include tag left in response: %q", rec.Body.String())
	}
}

func TestInitDefaultsMaxConcurrentRequestsUnlimited(t *testing.T) {
	p := &Plugin{}
	if err := p.Init(); err != nil {
		t.Fatalf("Init with absent max_concurrent_requests: %v", err)
	}
	if p.config.MaxConcurrentRequests != 0 {
		t.Fatalf("absent max_concurrent_requests = %d, want 0 (unlimited)", p.config.MaxConcurrentRequests)
	}

	defaults := CreateConfig()
	if err := (&Plugin{config: defaults}).Init(); err != nil {
		t.Fatalf("Init with actual CreateConfig defaults: %v", err)
	}
	if defaults.MaxConcurrentRequests != 0 {
		t.Fatalf("CreateConfig MaxConcurrentRequests = %d, want zero-value unlimited", defaults.MaxConcurrentRequests)
	}
}

func TestInitMaxWorkersAbsentDefaultsToLibraryValue(t *testing.T) {
	p := &Plugin{}
	if err := p.Init(); err != nil {
		t.Fatalf("Init with absent max_workers: %v", err)
	}
	if p.config.MaxWorkers != 0 {
		t.Fatalf("absent max_workers = %d, want 0 (library default)", p.config.MaxWorkers)
	}
	defaults := CreateConfig()
	if err := (&Plugin{config: defaults}).Init(); err != nil {
		t.Fatalf("Init with CreateConfig defaults: %v", err)
	}
	if defaults.MaxWorkers != 0 {
		t.Fatalf("CreateConfig MaxWorkers = %d, want zero-value library default", defaults.MaxWorkers)
	}
}

func TestMiddlewareMaxWorkersMapping(t *testing.T) {
	const maxWorkers = 2
	const includeCount = 8
	var fetched []string
	var mu sync.Mutex
	var page strings.Builder
	fragmentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetched = append(fetched, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "WORKER-FRAGMENT-%s", strings.TrimPrefix(r.URL.Path, "/fragment/"))
	}))
	t.Cleanup(fragmentServer.Close)
	// Use the server's actual URL in includes while keeping a deterministic,
	// fully rendered response assertion. This confirms the middleware copies
	// the configured worker value into EsiParserConfig.
	page.WriteString("<html><body>WORKERS-PAGE")
	for i := 0; i < includeCount; i++ {
		fmt.Fprintf(&page, `<esi:include src="%s/fragment/%d" />`, fragmentServer.URL, i)
	}
	page.WriteString("WORKERS-PAGE-END</body></html>")
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page.String()))
	})

	block := false
	p := &Plugin{config: &Config{MaxWorkers: maxWorkers, BlockPrivateIPs: &block}}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	rec := httptest.NewRecorder()
	p.Middleware(upstream).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	body := rec.Body.String()
	mu.Lock()
	gotFetched := len(fetched)
	mu.Unlock()
	if gotFetched != includeCount {
		t.Errorf("fetched %d fragments, want %d", gotFetched, includeCount)
	}
	for i := 0; i < includeCount; i++ {
		if !strings.Contains(body, fmt.Sprintf("WORKER-FRAGMENT-%d", i)) {
			t.Errorf("missing fragment %d from response %q", i, body)
		}
	}
	if strings.Contains(body, "<esi:include") {
		t.Errorf("raw include tag remained in response: %q", body)
	}
	if !strings.Contains(body, "WORKERS-PAGE") || !strings.Contains(body, "WORKERS-PAGE-END") {
		t.Errorf("page markers missing from response: %q", body)
	}
}

func TestMiddlewareMaxResponseSize(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int
		limit int64
		want  bool
	}{
		{name: "at_limit_accepted", size: 100, limit: 100, want: true},
		{name: "one_over_limit_rejected", size: 101, limit: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(strings.Repeat("x", tc.size)))
			}))
			t.Cleanup(fragment.Close)

			upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<html><body><esi:include src="` + fragment.URL + `" /></body></html>`))
			})
			block := false
			p := &Plugin{config: &Config{MaxResponseSize: tc.limit, BlockPrivateIPs: &block}}
			if err := p.Init(); err != nil {
				t.Fatalf("Init: %v", err)
			}

			rec := httptest.NewRecorder()
			p.Middleware(upstream).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}
			got := strings.Contains(rec.Body.String(), strings.Repeat("x", tc.size))
			if got != tc.want {
				t.Errorf("included body presence = %v, want %v (body %q)", got, tc.want, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "esi:include") {
				t.Errorf("include tag was not processed: %q", rec.Body.String())
			}
		})
	}
}

// concurrencyGauge measures the peak number of delayed include handlers
// active at once. Distinct include URLs make each fetch independent.
type concurrencyGauge struct {
	mu      sync.Mutex
	current int
	peak    int
}

func (g *concurrencyGauge) enter() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current++
	if g.current > g.peak {
		g.peak = g.current
	}
}

func (g *concurrencyGauge) leave() {
	g.mu.Lock()
	g.current--
	g.mu.Unlock()
}

func (g *concurrencyGauge) peakValue() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

func TestMiddlewareMaxConcurrentRequests(t *testing.T) {
	const includeCount = 12
	for _, tc := range []struct {
		name       string
		limit      int
		wantCapped bool
	}{
		{name: "cap_3", limit: 3, wantCapped: true},
		{name: "explicit_zero_unlimited", limit: 0},
		{name: "absent_unlimited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &concurrencyGauge{}
			fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				g.enter()
				defer g.leave()
				time.Sleep(150 * time.Millisecond)
				w.Header().Set("Content-Type", "text/plain")
				_, _ = fmt.Fprintf(w, "CONCURRENT-FRAGMENT-%s", strings.TrimPrefix(r.URL.Path, "/fragment/"))
			}))
			t.Cleanup(fragment.Close)

			var page strings.Builder
			page.WriteString("<html><body>CONCURRENT-PAGE")
			for i := 0; i < includeCount; i++ {
				fmt.Fprintf(&page, `<esi:include src="%s/fragment/%s" />`, fragment.URL, strconv.Itoa(i))
			}
			page.WriteString("CONCURRENT-END</body></html>")
			upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(page.String()))
			})

			block := false
			cfg := &Config{BlockPrivateIPs: &block}
			if tc.name != "absent_unlimited" {
				cfg.MaxConcurrentRequests = tc.limit
			}
			p := &Plugin{config: cfg}
			if err := p.Init(); err != nil {
				t.Fatalf("Init: %v", err)
			}

			rec := httptest.NewRecorder()
			p.Middleware(upstream).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
			body := rec.Body.String()
			peak := g.peakValue()
			if tc.wantCapped {
				if peak > tc.limit {
					t.Errorf("peak concurrent fetches %d > cap %d", peak, tc.limit)
				}
				if peak < 2 {
					t.Errorf("peak concurrent fetches %d < 2, expected parallel slots", peak)
				}
			} else if peak < 4 {
				t.Errorf("peak concurrent fetches %d < 4; zero/absent must fan out", peak)
			}
			if got := strings.Count(body, "CONCURRENT-FRAGMENT-"); got != includeCount {
				t.Errorf("delivered %d fragments, want %d (queued includes must not be dropped)", got, includeCount)
			}
			if strings.Contains(body, "<esi:include") {
				t.Errorf("raw include tag left in response: %q", body)
			}
			if !strings.Contains(body, "CONCURRENT-PAGE") || !strings.Contains(body, "CONCURRENT-END") {
				t.Errorf("page markers missing: %q", body)
			}
		})
	}
}

func TestMiddlewareMaxResponseSizeLegacyDefaultUnlimited(t *testing.T) {
	fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", 200)))
	}))
	t.Cleanup(fragment.Close)

	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><esi:include src="` + fragment.URL + `" /></body></html>`))
	})
	block := false
	p := &Plugin{config: &Config{BlockPrivateIPs: &block}}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	rec := httptest.NewRecorder()
	p.Middleware(upstream).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), strings.Repeat("x", 200)) {
		t.Errorf("historical unlimited default did not include fragment")
	}
	if strings.Contains(rec.Body.String(), "esi:include") {
		t.Errorf("include tag was not processed: %q", rec.Body.String())
	}
}

func TestMiddlewareNonHTML(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	config := CreateConfig()
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(handler)
	req := httptest.NewRequest("GET", "http://example.com/api", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("Expected body {'status':'ok'}, got %s", rec.Body.String())
	}
}

func TestMiddlewareHTML(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
	})

	config := CreateConfig()
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(handler)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestMiddlewareWithCache(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><esi:include src=\"/fragment\" /></body></html>"))
	})

	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(handler)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestMiddlewareSurrogateCapability(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>content</body></html>"))
	})

	config := CreateConfig()
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(handler)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if req.Header.Get("Surrogate-Capability") != "ESI/1.0" {
		t.Errorf("Expected Surrogate-Capability header, got %s", req.Header.Get("Surrogate-Capability"))
	}
}

func TestMiddlewareIncludeErrorMarker(t *testing.T) {
	config := CreateConfig()
	config.IncludeErrorMarker = "<!-- esi error -->"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if p.config.IncludeErrorMarker != "<!-- esi error -->" {
		t.Errorf("Expected include_error_marker '<!-- esi error -->', got %s", p.config.IncludeErrorMarker)
	}
}

func TestInitCacheKeyTemplate(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"
	config.CacheKeyTemplate = "mesi:${url}:${header:Accept-Language}"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.config.CacheKeyTemplate != "mesi:${url}:${header:Accept-Language}" {
		t.Errorf("Expected cache_key_template 'mesi:${url}:${header:Accept-Language}', got %s", p.config.CacheKeyTemplate)
	}
}

func TestInitCacheKeyTemplateDefaultEmpty(t *testing.T) {
	config := CreateConfig()
	config.CacheBackend = "memory"
	config.CacheTTL = "60s"

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.config.CacheKeyTemplate != "" {
		t.Errorf("Expected empty cache_key_template, got %s", p.config.CacheKeyTemplate)
	}
}

// newBlockPrivateIPsTestServers spins up a private-IP (127.0.0.1) fragment
// server and an upstream server that emits an <esi:include> pointing at it.
// 127.0.0.1 is a loopback/reserved address, so it is blocked by the SSRF
// dial-time check whenever BlockPrivateIPs is enabled.
func newBlockPrivateIPsTestServers(t *testing.T) (fragmentURL string, upstream http.Handler) {
	t.Helper()

	fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("FRAGMENT_OK"))
	}))
	t.Cleanup(fragment.Close)

	upstream = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf("<html><body><esi:include src=\"%s/fragment\" /></body></html>", fragment.URL)))
	})

	return fragment.URL, upstream
}

func TestBlockPrivateIPsDefaultBlocks(t *testing.T) {
	_, upstream := newBlockPrivateIPsTestServers(t)

	config := CreateConfig()
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(upstream)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected private-IP include to be blocked by default, got body: %s", rec.Body.String())
	}
}

func TestBlockPrivateIPsTrueBlocks(t *testing.T) {
	_, upstream := newBlockPrivateIPsTestServers(t)

	block := true
	config := CreateConfig()
	config.BlockPrivateIPs = &block
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(upstream)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected private-IP include to be blocked when block_private_ips=true, got body: %s", rec.Body.String())
	}
}

func TestBlockPrivateIPsFalseAllows(t *testing.T) {
	_, upstream := newBlockPrivateIPsTestServers(t)

	block := false
	config := CreateConfig()
	config.BlockPrivateIPs = &block
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	middleware := p.Middleware(upstream)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected private-IP include to be allowed when block_private_ips=false, got body: %s", rec.Body.String())
	}
}

// newAllowedHostsTestServers spins up the same loopback fragment pair as
// newBlockPrivateIPsTestServers and returns the include URL with the
// hostname replaced by the caller-provided one, so hostname-based
// allowed_hosts matching can be exercised without DNS. Callers MUST also
// set block_private_ips=false: the loopback fragment is a private IP, and
// AllowedHosts does NOT bypass the dial-time BlockPrivateIPs check by
// design (defense-in-depth, see docs/features.md).
func newAllowedHostsTestServers(t *testing.T, hostname string) (fragmentURL string, upstream http.Handler) {
	t.Helper()

	fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("FRAGMENT_OK"))
	}))
	t.Cleanup(fragment.Close)

	// 127.0.0.1 -> caller-provided hostname (e.g. "127.0.0.1" itself).
	includeURL := "http://" + hostname + strings.TrimPrefix(fragment.URL, "http://127.0.0.1")

	upstream = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf("<html><body><esi:include src=\"%s/fragment\" /></body></html>", includeURL)))
	})

	return includeURL, upstream
}

// newAllowedHostsTestPlugin returns an initialized plugin with
// block_private_ips=false and the given allowed_hosts list, ready to serve
// the upstream handler.
func newAllowedHostsTestPlugin(t *testing.T, allowedHosts []string) http.Handler {
	t.Helper()

	_, upstream := newAllowedHostsTestServers(t, "127.0.0.1")

	block := false
	config := CreateConfig()
	config.BlockPrivateIPs = &block
	config.AllowedHosts = allowedHosts
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	return p.Middleware(upstream)
}

func TestAllowedHostsDefaultAllows(t *testing.T) {
	// Backward compatibility: absent/empty allowed_hosts keeps the legacy
	// unrestricted behaviour once BlockPrivateIPs permits the dial.
	handler := newAllowedHostsTestPlugin(t, nil)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to be allowed with empty allowed_hosts, got body: %s", rec.Body.String())
	}
}

func TestAllowedHostsListedHostAllows(t *testing.T) {
	handler := newAllowedHostsTestPlugin(t, []string{"127.0.0.1"})
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to be allowed when the host is listed in allowed_hosts, got body: %s", rec.Body.String())
	}
}

func TestAllowedHostsSuffixMatchAllows(t *testing.T) {
	// Subdomain semantics through the plugin wiring: the core matches exact
	// host or dot-boundary suffix (hostMatches in mesi/ssrf.go), so
	// sub.example.com matches example.com. Exercised DNS-free here with an
	// IP-literal suffix: "127.0.0.1" matches the allowed entry "0.0.1"
	// (host[len(host)-len(allowed)-1] == '.').
	handler := newAllowedHostsTestPlugin(t, []string{"0.0.1"})
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to be allowed via suffix match, got body: %s", rec.Body.String())
	}
}

func TestAllowedHostsUnlistedHostBlocks(t *testing.T) {
	handler := newAllowedHostsTestPlugin(t, []string{"example.com"})
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to be blocked when the host is NOT listed in allowed_hosts, got body: %s", rec.Body.String())
	}
	// Guard against a vacuous pass (ESI processing silently disabled): the
	// raw <esi:include> tag must also be gone from the output.
	if strings.Contains(rec.Body.String(), "esi:include") {
		t.Errorf("Expected the <esi:include> tag to be processed away, got body: %s", rec.Body.String())
	}
}

// newBypassTestPlugin returns an initialized plugin exercising the
// allow_private_ips_for_allowed_hosts path: BlockPrivateIPs is left at the
// safe default (true), the include host must be in allowedHosts, and the
// bypass flag is set to allowBypass. sharedHTTPClient optionally enables the
// shared-client path (where the bypass is documented to have no effect).
func newBypassTestPlugin(t *testing.T, allowedHosts []string, allowBypass, sharedHTTPClient bool) http.Handler {
	t.Helper()

	_, upstream := newAllowedHostsTestServers(t, "127.0.0.1")

	config := CreateConfig()
	config.AllowedHosts = allowedHosts
	config.AllowPrivateIPsForAllowedHosts = allowBypass
	config.SharedHTTPClient = sharedHTTPClient
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	return p.Middleware(upstream)
}

func TestAllowPrivateIPsForAllowedHostsBypassAllows(t *testing.T) {
	// Bypass opt-in: a listed host may resolve to a private/reserved IP
	// without tripping the dial-time block (block_private_ips stays true).
	handler := newBypassTestPlugin(t, []string{"127.0.0.1"}, true, false)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to be allowed via bypass for listed host, got body: %s", rec.Body.String())
	}
}

func TestAllowPrivateIPsForAllowedHostsDefaultBlocks(t *testing.T) {
	// Backward compatibility: flag absent/false (default) keeps the
	// dial-time private-IP block for listed hosts.
	handler := newBypassTestPlugin(t, []string{"127.0.0.1"}, false, false)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to be blocked without the bypass flag, got body: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "esi:include") {
		t.Errorf("Expected the <esi:include> tag to be processed away, got body: %s", rec.Body.String())
	}
}

func TestAllowPrivateIPsForAllowedHostsUnlistedHostStillBlocked(t *testing.T) {
	// The bypass only covers hosts present in allowed_hosts: a private host
	// outside the whitelist stays blocked even with the flag on.
	handler := newBypassTestPlugin(t, []string{"example.com"}, true, false)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to private host outside allowed_hosts to stay blocked, got body: %s", rec.Body.String())
	}
	// Guard against a vacuous pass: the raw <esi:include> tag must also be gone.
	if strings.Contains(rec.Body.String(), "esi:include") {
		t.Errorf("Expected the <esi:include> tag to be processed away, got body: %s", rec.Body.String())
	}
}

func TestAllowPrivateIPsForAllowedHostsSharedClientStillBlocks(t *testing.T) {
	// Documented limitation: with shared_http_client the bypass is not
	// consulted in the core (fetchClientForURL returns the wrapped shared
	// client whose transport bakes block_private_ips at startup), so the
	// include stays blocked even with the flag set.
	handler := newBypassTestPlugin(t, []string{"127.0.0.1"}, true, true)
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to stay blocked under shared_http_client, got body: %s", rec.Body.String())
	}
	// Guard against a vacuous pass: the raw <esi:include> tag must also be gone.
	if strings.Contains(rec.Body.String(), "esi:include") {
		t.Errorf("Expected the <esi:include> tag to be processed away, got body: %s", rec.Body.String())
	}
}

func TestAllowedHostsMultipleHostsAllows(t *testing.T) {
	// A multi-entry allowed_hosts list: the include host matched by the
	// second entry still resolves (ordering must not matter, and one
	// unrelated entry must not break matching for the listed host).
	handler := newAllowedHostsTestPlugin(t, []string{"someother.host", "127.0.0.1"})
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FRAGMENT_OK") {
		t.Errorf("Expected include to be allowed when the host is listed (2nd entry) in allowed_hosts, got body: %s", rec.Body.String())
	}
}

func TestInitExplicitZeroKeepsPassthrough(t *testing.T) {
	cfg := &Config{MaxDepth: intPtr(0)}
	p := &Plugin{config: cfg}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.config.MaxDepth == nil || *p.config.MaxDepth != 0 {
		t.Errorf("expected explicit 0 to stay 0, got %v", p.config.MaxDepth)
	}
	if p.maxDepth() != 0 {
		t.Errorf("expected maxDepth()=0, got %d", p.maxDepth())
	}
}

func TestInitNilMaxDepthDefaultsToFive(t *testing.T) {
	p := &Plugin{config: &Config{}}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.config.MaxDepth == nil || *p.config.MaxDepth != 5 {
		t.Errorf("expected unset MaxDepth to become 5, got %v", p.config.MaxDepth)
	}
}

func TestInitExplicitCustomMaxDepthKept(t *testing.T) {
	// AC (#183): max_depth: 3 must round-trip — a custom value is neither
	// coerced to the default 5 nor rejected, and maxDepth() (the accessor
	// feeding EsiParserConfig.MaxDepth in Middleware) returns it verbatim.
	config := CreateConfig()
	config.MaxDepth = intPtr(3)

	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.config.MaxDepth == nil || *p.config.MaxDepth != 3 {
		t.Errorf("expected max_depth 3 to be kept, got %v", p.config.MaxDepth)
	}
	if p.maxDepth() != 3 {
		t.Errorf("expected maxDepth()=3, got %d", p.maxDepth())
	}
}

func TestInitRejectsNegativeMaxDepth(t *testing.T) {
	p := &Plugin{config: &Config{MaxDepth: intPtr(-1)}}
	err := p.Init()
	if err == nil {
		t.Fatal("expected error for negative max_depth")
	}
	if !strings.Contains(err.Error(), "max_depth") {
		t.Errorf("expected max_depth in error, got %v", err)
	}
}

func TestInitRejectsMaxDepthAboveCap(t *testing.T) {
	p := &Plugin{config: &Config{MaxDepth: intPtr(int(mesi.MaxMaxDepth) + 1)}}
	err := p.Init()
	if err == nil {
		t.Fatal("expected error for max_depth above MaxMaxDepth")
	}
	if !strings.Contains(err.Error(), "max_depth") {
		t.Errorf("expected max_depth in error, got %v", err)
	}
}

func TestInitAcceptsMaxMaxDepth(t *testing.T) {
	// Boundary: mesi.MaxMaxDepth is the inclusive upper bound — the
	// rejection above it (max+1) must not come with an off-by-one that
	// also rejects the accepted max itself.
	p := &Plugin{config: &Config{MaxDepth: intPtr(int(mesi.MaxMaxDepth))}}
	if err := p.Init(); err != nil {
		t.Fatalf("Init rejected max_depth == MaxMaxDepth (%d): %v", mesi.MaxMaxDepth, err)
	}
	if p.maxDepth() != int(mesi.MaxMaxDepth) {
		t.Errorf("expected maxDepth()=%d, got %d", mesi.MaxMaxDepth, p.maxDepth())
	}
}

func TestMiddlewareMaxDepthZeroPassthrough(t *testing.T) {
	fragmentCalls := 0
	frag := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fragmentCalls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fragment"))
	}))
	defer frag.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + frag.URL + `/frag" /></body></html>`))
	})

	p := &Plugin{config: &Config{MaxDepth: intPtr(0)}}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	rec := httptest.NewRecorder()
	p.Middleware(handler).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))

	if fragmentCalls != 0 {
		t.Errorf("expected 0 fragment fetches at max_depth 0, got %d", fragmentCalls)
	}
	if rec.Body.String() != "<html><body></body></html>" {
		t.Errorf("expected passthrough empty include, got %q", rec.Body.String())
	}
}

// newNestedChainServer serves a chain of nested ESI pages on one test
// server: /level-1 → /level-2 → … → /level-<levels>, where /level-<levels>
// has no include. Every level body carries a LEVEL-<n>-BODY marker, so the
// final response proves exactly how many include levels were fetched and
// inlined — the same per-level marker scheme as
// servers/nginx/tests/nested_depth_{outer,inner}.txt (issue #428: a
// marker-less fixture cannot distinguish "not fetched" from "fetched but
// empty"). Returns the server's base URL.
func newNestedChainServer(t *testing.T, levels int) string {
	t.Helper()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for i := 1; i <= levels; i++ {
		body := fmt.Sprintf("LEVEL-%d-BODY", i)
		if i < levels {
			body += fmt.Sprintf(`<esi:include src="%s/level-%d" />`, srv.URL, i+1)
		}
		mux.HandleFunc(fmt.Sprintf("/level-%d", i), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(body))
		})
	}

	return srv.URL
}

func TestMiddlewareCustomMaxDepthThreeProcessesExactLevels(t *testing.T) {
	// AC (#183): a custom max_depth: 3 must reach EsiParserConfig.MaxDepth
	// unchanged. The chain requires exactly three fetches at depth 3:
	// levels 1-3 are inlined (markers present), while the level-3 include's
	// target is parsed with MaxDepth=0 — not fetched (LEVEL-4 marker
	// absent) and stripped through the include-error path (no raw tag left).
	// A regression to the default 5 would fetch LEVEL-4; a regression to 1
	// would stop after LEVEL-1 — both fail this test.
	baseURL := newNestedChainServer(t, 4)

	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><esi:include src="` + baseURL + `/level-1" /></body></html>`))
	})

	block := false
	config := CreateConfig()
	config.MaxDepth = intPtr(3)
	config.BlockPrivateIPs = &block
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	rec := httptest.NewRecorder()
	p.Middleware(upstream).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))

	body := rec.Body.String()
	for _, want := range []string{"LEVEL-1-BODY", "LEVEL-2-BODY", "LEVEL-3-BODY"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %s at max_depth 3, got body: %s", want, body)
		}
	}
	if strings.Contains(body, "LEVEL-4-BODY") {
		t.Errorf("expected level-4 include NOT fetched at max_depth 3, got body: %s", body)
	}
	if strings.Contains(body, "esi:include") {
		t.Errorf("expected leftover include tag stripped, got body: %s", body)
	}
}
