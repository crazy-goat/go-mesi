package mesi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/quick"
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
			// The leading space comes from unescape stripping the "<!--esi" and "-->" markers,
			// leaving " <esi:include src=\"x\"/>". The space before the tag is preserved as a
			// static token. The include produces empty output (IncludeErrorMarker default),
			// so only the space remains.
			name:       "max depth 0 with include produces empty (error not leaked)",
			input:      "<!--esi <esi:include src=\"x\"/>-->",
			maxDepth:   0,
			defaultUrl: "http://example.com/",
			expected:   " ",
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

func TestMESIParseWorkerPoolRespectsMaxWorkers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}

		var maxConcurrent atomic.Int64
		var current atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := current.Add(1)
		for {
			old := maxConcurrent.Load()
			if v <= old || maxConcurrent.CompareAndSwap(old, v) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		current.Add(-1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxWorkers = 2
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	// Disable HTTP-level semaphore to test worker pool independently
	config.MaxConcurrentRequests = 0

	var input string
	for i := 0; i < 10; i++ {
		input += `<!--esi <esi:include src="` + server.URL + `/` + strconv.Itoa(i) + `"/>-->`
	}

	MESIParse(input, config)

	mc := maxConcurrent.Load()
	if mc > 2 {
		t.Errorf("max concurrent includes = %d, want <= 2 (MaxWorkers=2)", mc)
	}
}

func TestMESIParseMixedStaticAndESI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("INCLUDE:" + r.URL.Path))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := "prefix" +
		`<!--esi <esi:include src="` + server.URL + `/first"/>-->` +
		"middle" +
		`<!--esi <esi:include src="` + server.URL + `/second"/>-->` +
		"suffix"

	result := MESIParse(input, config)
	// Each <!--esi block adds a leading space from unescape
	expected := "prefix INCLUDE:/firstmiddle INCLUDE:/secondsuffix"
	if result != expected {
		t.Errorf("MESIParse() = %q, want %q", result, expected)
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

func TestAssembleResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []Response
		expected string
	}{
		{"empty results", []Response{}, ""},
		{"single result", []Response{{"hello", 0}}, "hello"},
		{"multiple results in order", []Response{{"a", 0}, {"b", 1}, {"c", 2}}, "abc"},
		{"multiple results out of order", []Response{{"c", 2}, {"a", 0}, {"b", 1}}, "abc"},
		{"results with same index, stable", []Response{{"a", 0}, {"b", 0}}, "ab"},
		{"many duplicates stable", []Response{{"x", 0}, {"y", 0}, {"z", 0}, {"q", 1}}, "xyzq"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := assembleResults(tt.results)
			if result != tt.expected {
				t.Errorf("assembleResults() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func makeResponses(n int) []Response {
	res := make([]Response, n)
	for i := range res {
		res[i] = Response{content: string(rune('a' + i%26)), index: n - i}
	}
	return res
}

func BenchmarkAssembleResults(b *testing.B) {
	res := makeResponses(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copied := append([]Response(nil), res...)
		_ = assembleResults(copied)
	}
}

func TestAssembleResultsStableOrder(t *testing.T) {
	f := func(n uint8) bool {
		n = n%32 + 1
		inputs := make([]Response, n)
		for i := range inputs {
			inputs[i] = Response{content: string(rune('a' + i%26)), index: 0}
		}
		out := assembleResults(append([]Response(nil), inputs...))
		var expected strings.Builder
		for _, r := range inputs {
			expected.WriteString(r.content)
		}
		return out == expected.String()
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
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
