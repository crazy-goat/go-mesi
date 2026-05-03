package mesi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		maxDepth   int
		defaultUrl string
		expected   string
	}{
		{
			name:       "empty input",
			input:      "",
			maxDepth:   5,
			defaultUrl: "http://example.com/",
			expected:   "",
		},
		{
			name:       "no ESI tags",
			input:      "<html><body>Hello World</body></html>",
			maxDepth:   5,
			defaultUrl: "http://example.com/",
			expected:   "<html><body>Hello World</body></html>",
		},
		{
			name:       "max depth 0 with include triggers max depth error",
			input:      "<!--esi <esi:include src=\"x\"/>-->",
			maxDepth:   0,
			defaultUrl: "http://example.com/",
			expected:   " esi max depth",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := Parse(c.input, c.maxDepth, c.defaultUrl)
			if result != c.expected {
				t.Errorf("Parse() = %q, want %q", result, c.expected)
			}
		})
	}
}

type concurrentTest struct {
	name               string
	maxConcurrent      int
	tokenCount         int
	useFetchConcurrent bool
	expectedMax        int64
}

func runConcurrentTest(t *testing.T, tc concurrentTest) {
	t.Run(tc.name, func(t *testing.T) {
		var maxConcurrent int64
		var current atomic.Int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inc := current.Add(1)
			old := atomic.LoadInt64(&maxConcurrent)
			if inc > old {
				atomic.CompareAndSwapInt64(&maxConcurrent, old, inc)
			}

			time.Sleep(100 * time.Millisecond)

			current.Add(-1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("content"))
		}))
		defer server.Close()

		config := CreateDefaultConfig()
		config.MaxConcurrentRequests = tc.maxConcurrent
		config.DefaultUrl = server.URL + "/"
		config.MaxDepth = 1
		config.BlockPrivateIPs = false

		var input string
		for i := 0; i < tc.tokenCount; i++ {
			if tc.useFetchConcurrent {
				input += `<!--esi <esi:include src="` + server.URL + `/` + strconv.Itoa(i) + `" alt="` + server.URL + `/` + strconv.Itoa(i) + `alt" fetch-mode="concurrent"/>-->`
			} else {
				input += `<!--esi <esi:include src="` + server.URL + `/` + strconv.Itoa(i) + `"/>-->`
			}
		}

		MESIParse(input, config)

		if atomic.LoadInt64(&maxConcurrent) > tc.expectedMax {
			t.Errorf("Max concurrent = %d, expected <= %d", atomic.LoadInt64(&maxConcurrent), tc.expectedMax)
		}
	})
}

func TestMaxConcurrentRequestsLimitsConcurrency(t *testing.T) {
	runConcurrentTest(t, concurrentTest{
		name:               "basic_limit",
		maxConcurrent:      2,
		tokenCount:         5,
		useFetchConcurrent: false,
		expectedMax:        2,
	})
}

func TestMaxConcurrentRequestsZeroMeansUnlimited(t *testing.T) {
	var maxConcurrent int64
	var current atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inc := current.Add(1)
		old := atomic.LoadInt64(&maxConcurrent)
		if inc > old {
			atomic.CompareAndSwapInt64(&maxConcurrent, old, inc)
		}

		time.Sleep(20 * time.Millisecond)

		current.Add(-1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxConcurrentRequests = 0
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<!--esi <esi:include src="` + server.URL + `/1"/>--><!--esi <esi:include src="` + server.URL + `/2"/>--><!--esi <esi:include src="` + server.URL + `/3"/>-->`

	MESIParse(input, config)

	if atomic.LoadInt64(&maxConcurrent) != 3 {
		t.Errorf("Max concurrent = %d, expected 3 (unlimited)", atomic.LoadInt64(&maxConcurrent))
	}
}

func TestSharedHTTPClientIsUsed(t *testing.T) {
	var clientCount int64
	var transportCalls int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&clientCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	customTransport := &customTransport{rt: http.DefaultTransport, calls: &transportCalls}
	config := CreateDefaultConfig()
	config.HTTPClient = &http.Client{Transport: customTransport}
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<!--esi <esi:include src="` + server.URL + `/1"/>--><!--esi <esi:include src="` + server.URL + `/2"/>--><!--esi <esi:include src="` + server.URL + `/3"/>-->`

	MESIParse(input, config)

	if atomic.LoadInt64(&clientCount) != 3 {
		t.Errorf("HTTP client used %d times, expected 3 (shared client)", atomic.LoadInt64(&clientCount))
	}
	if atomic.LoadInt64(&transportCalls) != 3 {
		t.Errorf("Transport used %d times, expected 3 (same client instance)", atomic.LoadInt64(&transportCalls))
	}
}

type customTransport struct {
	rt    http.RoundTripper
	calls *int64
}

func (ct *customTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	atomic.AddInt64(ct.calls, 1)
	return ct.rt.RoundTrip(r)
}

func TestFetchConcurrentRespectsMaxConcurrentRequests(t *testing.T) {
	runConcurrentTest(t, concurrentTest{
		name:               "fetch_concurrent_with_alt",
		maxConcurrent:      2,
		tokenCount:         3,
		useFetchConcurrent: true,
		expectedMax:        2,
	})
}

func TestNilHTTPClientFallsBackToDefault(t *testing.T) {
	var requestCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.HTTPClient = nil
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<!--esi <esi:include src="` + server.URL + `/1"/>-->`

	MESIParse(input, config)

	if atomic.LoadInt64(&requestCount) != 1 {
		t.Errorf("Request count = %d, expected 1", atomic.LoadInt64(&requestCount))
	}
}

func TestCreateDefaultConfig(t *testing.T) {
	config := CreateDefaultConfig()

	if config.Context == nil {
		t.Error("Context should not be nil")
	}
	if config.DefaultUrl != "http://127.0.0.1/" {
		t.Errorf("DefaultUrl = %q, want %q", config.DefaultUrl, "http://127.0.0.1/")
	}
	if config.MaxDepth != 5 {
		t.Errorf("MaxDepth = %d, want 5", config.MaxDepth)
	}
	if config.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", config.Timeout)
	}
	if config.ParseOnHeader != false {
		t.Error("ParseOnHeader should be false")
	}
	if config.BlockPrivateIPs != true {
		t.Error("BlockPrivateIPs should be true")
	}
	if config.MaxResponseSize != 10*1024*1024 {
		t.Errorf("MaxResponseSize = %d, want 10MB", config.MaxResponseSize)
	}
	if config.CacheKeyFunc == nil {
		t.Error("CacheKeyFunc should not be nil")
	}
}

func TestCanGoDeeper(t *testing.T) {
	tests := []struct {
		name     string
		maxDepth uint
		timeout  time.Duration
		elapsed  time.Duration
		expected bool
	}{
		{"can go deeper", 5, 10 * time.Second, 2 * time.Second, true},
		{"max depth zero", 0, 10 * time.Second, 2 * time.Second, false},
		{"timeout exceeded", 5, 10 * time.Second, 15 * time.Second, false},
		{"timeout equal elapsed", 5, 10 * time.Second, 10 * time.Second, false},
		{"max depth one with time", 1, 10 * time.Second, 5 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{MaxDepth: tt.maxDepth, Timeout: tt.timeout}
			if got := config.CanGoDeeper(tt.elapsed); got != tt.expected {
				t.Errorf("CanGoDeeper() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseOnly(t *testing.T) {
	tests := []struct {
		name     string
		maxDepth uint
		expected bool
	}{
		{"parse only when zero", 0, true},
		{"not parse only when positive", 1, false},
		{"not parse only when five", 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{MaxDepth: tt.maxDepth}
			if got := config.ParseOnly(); got != tt.expected {
				t.Errorf("ParseOnly() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDecreaseMaxDepth(t *testing.T) {
	tests := []struct {
		name     string
		maxDepth uint
		expected uint
	}{
		{"decrease from five", 5, 4},
		{"decrease from one", 1, 0},
		{"stay at zero", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{MaxDepth: tt.maxDepth}
			result := config.DecreaseMaxDepth()
			if result.MaxDepth != tt.expected {
				t.Errorf("DecreaseMaxDepth() MaxDepth = %d, want %d", result.MaxDepth, tt.expected)
			}
		})
	}
}

func TestWithElapsedTime(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		elapsed  time.Duration
		expected time.Duration
	}{
		{"subtract elapsed", 10 * time.Second, 3 * time.Second, 7 * time.Second},
		{"elapsed equals timeout", 10 * time.Second, 10 * time.Second, 0},
		{"elapsed exceeds timeout", 10 * time.Second, 15 * time.Second, 0},
		{"no elapsed time", 10 * time.Second, 0, 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{Timeout: tt.timeout}
			result := config.WithElapsedTime(tt.elapsed)
			if result.Timeout != tt.expected {
				t.Errorf("WithElapsedTime() Timeout = %v, want %v", result.Timeout, tt.expected)
			}
		})
	}
}

func TestOverrideConfigWithTimeout(t *testing.T) {
	tests := []struct {
		name         string
		configTTL    time.Duration
		tokenTimeout string
		expected     time.Duration
	}{
		{"token timeout smaller", 10 * time.Second, "5", 5 * time.Second},
		{"token timeout larger", 5 * time.Second, "10", 5 * time.Second},
		{"invalid timeout", 10 * time.Second, "invalid", 10 * time.Second},
		{"empty timeout", 10 * time.Second, "", 10 * time.Second},
		{"zero timeout", 10 * time.Second, "0", 10 * time.Second},
		{"negative timeout", 10 * time.Second, "-1", 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{Timeout: tt.configTTL}
			token := esiIncludeToken{Timeout: tt.tokenTimeout}
			result := config.OverrideConfig(token)
			if result.Timeout != tt.expected {
				t.Errorf("OverrideConfig() Timeout = %v, want %v", result.Timeout, tt.expected)
			}
		})
	}
}

func TestOverrideConfigWithMaxDepth(t *testing.T) {
	tests := []struct {
		name          string
		configDepth   uint
		tokenMaxDepth string
		expected      uint
	}{
		{"token limit lower than config", 10, "3", 4},
		{"token limit higher than config", 5, "10", 5},
		{"invalid max depth", 10, "invalid", 10},
		{"empty max depth", 10, "", 10},
		{"zero max depth becomes limit 1", 10, "0", 1},
		{"negative max depth ignored", 10, "-1", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{MaxDepth: tt.configDepth}
			token := esiIncludeToken{MaxDepth: tt.tokenMaxDepth}
			result := config.OverrideConfig(token)
			if result.MaxDepth != tt.expected {
				t.Errorf("OverrideConfig() MaxDepth = %d, want %d", result.MaxDepth, tt.expected)
			}
		})
	}
}

func TestMESIParseSimpleStaticContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"plain text", "Hello World", "Hello World"},
		{"html without esi", "<html><body>Test</body></html>", "<html><body>Test</body></html>"},
		{"esi comment without tags", "<!--esi plain text-->", " plain text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := CreateDefaultConfig()
			config.MaxDepth = 0
			result := MESIParse(tt.input, config)
			if result != tt.expected {
				t.Errorf("MESIParse() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMESIParseWithInclude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("included content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<!--esi <esi:include src="` + server.URL + `/test"/>-->`
	result := MESIParse(input, config)

	if result != " included content" {
		t.Errorf("MESIParse() = %q, want %q", result, " included content")
	}
}

func TestMESIParseRespectsMaxDepth(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 0
	config.BlockPrivateIPs = false

	input := `<!--esi <esi:include src="` + server.URL + `/test"/>-->`
	MESIParse(input, config)

	if callCount.Load() != 0 {
		t.Errorf("Expected 0 HTTP calls with MaxDepth=0, got %d", callCount.Load())
	}
}

func TestMESIParseRespectsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.Timeout = 100 * time.Millisecond
	config.BlockPrivateIPs = false

	input := `<!--esi <esi:include src="` + server.URL + `/test"/>-->`
	result := MESIParse(input, config)

	if strings.Contains(result, "content") {
		t.Errorf("Expected timeout to prevent full content, got %q", result)
	}
}

func TestParseDeprecatedCreatesCorrectConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("response"))
	}))
	defer server.Close()

	result := Parse("static content", 0, server.URL+"/")
	if result != "static content" {
		t.Errorf("Parse() = %q, want %q", result, "static content")
	}
}

func TestOverrideConfigWithBothTimeoutAndMaxDepth(t *testing.T) {
	config := EsiParserConfig{
		Timeout:  10 * time.Second,
		MaxDepth: 10,
	}
	token := esiIncludeToken{
		Timeout:  "3",
		MaxDepth: "2",
	}
	result := config.OverrideConfig(token)

	if result.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v, want 3s", result.Timeout)
	}
	if result.MaxDepth != 3 {
		t.Errorf("MaxDepth = %d, want 3", result.MaxDepth)
	}
}

func TestAssembleResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []Response
		expected string
		anyOf    []string
	}{
		{"empty results", []Response{}, "", nil},
		{"single result", []Response{{"hello", 0}}, "hello", nil},
		{"multiple results in order", []Response{{"a", 0}, {"b", 1}, {"c", 2}}, "abc", nil},
		{"multiple results out of order", []Response{{"c", 2}, {"a", 0}, {"b", 1}}, "abc", nil},
		{"results with same index", []Response{{"a", 0}, {"b", 0}}, "", []string{"ab", "ba"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var builder strings.Builder
			result := assembleResults(tt.results, builder)
			if tt.anyOf != nil {
				valid := false
				for _, exp := range tt.anyOf {
					if result == exp {
						valid = true
						break
					}
				}
				if !valid {
					t.Errorf("assembleResults() = %q, want one of %v", result, tt.anyOf)
				}
			} else if result != tt.expected {
				t.Errorf("assembleResults() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMESIParseNestedIncludes(t *testing.T) {
	var callCount atomic.Int32
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		switch r.URL.Path {
		case "/outer":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!--esi <esi:include src="` + serverURL + `/inner"/>-->`))
		case "/inner":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("inner content"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("unknown"))
		}
	}))
	defer server.Close()
	serverURL = server.URL

	config := CreateDefaultConfig()
	config.DefaultUrl = serverURL + "/"
	config.MaxDepth = 2
	config.BlockPrivateIPs = false

	input := `<!--esi <esi:include src="` + serverURL + `/outer"/>-->`
	result := MESIParse(input, config)

	if callCount.Load() != 2 {
		t.Errorf("Expected 2 HTTP calls for nested includes, got %d", callCount.Load())
	}
	if result != "  inner content" {
		t.Errorf("MESIParse() = %q, want %q", result, "  inner content")
	}
}
