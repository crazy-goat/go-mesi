package mesi

import (
	"fmt"
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
	handlerBlock := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-handlerBlock:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("content"))
		}
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

func TestExtractTryBlocks(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		wantAttempt       string
		wantExcept        string
	}{
		{
			name:        "no attempt tag",
			input:       "<esi:try>hello</esi:try>",
			wantAttempt: "",
			wantExcept:  "",
		},
		{
			name:        "attempt only",
			input:       "<esi:try><esi:attempt>hello</esi:attempt></esi:try>",
			wantAttempt: "hello",
			wantExcept:  "",
		},
		{
			name:        "attempt and except",
			input:       "<esi:try><esi:attempt>body</esi:attempt><esi:except>fallback</esi:except></esi:try>",
			wantAttempt: "body",
			wantExcept:  "fallback",
		},
		{
			name:        "attempt with nested content",
			input:       "<esi:try><esi:attempt><esi:include src=\"/x\"/></esi:attempt><esi:except>err</esi:except></esi:try>",
			wantAttempt: "<esi:include src=\"/x\"/>",
			wantExcept:  "err",
		},
		{
			name:        "empty attempt",
			input:       "<esi:try><esi:attempt></esi:attempt><esi:except>fallback</esi:except></esi:try>",
			wantAttempt: "",
			wantExcept:  "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt, except := extractTryBlocks(tt.input)
			if attempt != tt.wantAttempt {
				t.Errorf("attempt = %q, want %q", attempt, tt.wantAttempt)
			}
			if except != tt.wantExcept {
				t.Errorf("except = %q, want %q", except, tt.wantExcept)
			}
		})
	}
}

func TestESITrySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("attempt content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/ok"/>
	</esi:attempt>
	<esi:except>
		except content
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "attempt content") {
		t.Errorf("expected attempt content, got %q", result)
	}
	if strings.Contains(result, "except content") {
		t.Errorf("except content should NOT be rendered on success, got %q", result)
	}
}

func TestESITryFailure404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/missing"/>
	</esi:attempt>
	<esi:except>
		fallback content
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "fallback content") {
		t.Errorf("expected except content, got %q", result)
	}
	if strings.Contains(result, "not found") {
		t.Errorf("attempt content should NOT be rendered on failure, got %q", result)
	}
}

func TestESITryFailureTimeout(t *testing.T) {
	handlerBlock := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-handlerBlock:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("slow content"))
		}
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.Timeout = 50 * time.Millisecond
	config.BlockPrivateIPs = false

	input := `<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/slow"/>
	</esi:attempt>
	<esi:except>
		timeout fallback
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "timeout fallback") {
		t.Errorf("expected except content on timeout, got %q", result)
	}
	close(handlerBlock)
}

func TestESITryNoExcept(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/missing"/>
	</esi:attempt>
</esi:try>`

	result := MESIParse(input, config)
	if result != "" {
		t.Errorf("expected empty output when no except, got %q", result)
	}
}

func TestESITryWithOnerrorContinue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	// onerror="continue" should NOT trigger except
	input := `<esi:try>
	<esi:attempt>
		<esi:include onerror="continue" src="` + server.URL + `/fail"/>
		after include
	</esi:attempt>
	<esi:except>
		except content
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if strings.Contains(result, "except content") {
		t.Errorf("onerror=continue should NOT trigger except, got %q", result)
	}
	if !strings.Contains(result, "after include") {
		t.Errorf("content after include with onerror=continue should appear, got %q", result)
	}
}

func TestESITryWithFallbackBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	// fallback body should NOT trigger except
	input := `<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/fail">fallback body</esi:include>
	</esi:attempt>
	<esi:except>
		except content
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "fallback body") {
		t.Errorf("fallback body should be rendered, got %q", result)
	}
	if strings.Contains(result, "except content") {
		t.Errorf("fallback body should NOT trigger except, got %q", result)
	}
}

func TestESITryMultipleIncludesOneFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok content"))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		}
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	// One failing include should trigger except for the entire try block
	input := `<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/ok"/>
		<esi:include src="` + server.URL + `/missing"/>
	</esi:attempt>
	<esi:except>
		except content
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "except content") {
		t.Errorf("any failing include should trigger except, got %q", result)
	}
}

func TestESITryNested(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/inner-ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("inner ok"))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("error"))
		}
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 2
	config.BlockPrivateIPs = false

	// Outer try should succeed because inner try catches the failure
	input := `<esi:try>
	<esi:attempt>
		<esi:try>
			<esi:attempt>
				<esi:include src="` + server.URL + `/missing"/>
			</esi:attempt>
			<esi:except>
				inner caught
			</esi:except>
		</esi:try>
	</esi:attempt>
	<esi:except>
		outer except
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "inner caught") {
		t.Errorf("inner try should render except, got %q", result)
	}
	if strings.Contains(result, "outer except") {
		t.Errorf("outer try should NOT trigger except when inner catches, got %q", result)
	}
}

func TestESITryNestedIncludeInsideAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("included data"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<esi:try>
	<esi:attempt>
		before [<esi:include src="` + server.URL + `/fragment"/>] after
	</esi:attempt>
	<esi:except>
		except content
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "included data") {
		t.Errorf("include inside attempt should be processed, got %q", result)
	}
	if !strings.Contains(result, "before") || !strings.Contains(result, "after") {
		t.Errorf("static text around include should be preserved, got %q", result)
	}
	if strings.Contains(result, "except content") {
		t.Errorf("except should not be rendered on success, got %q", result)
	}
}

func TestESITryEmptyAttempt(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 1

	input := `<esi:try>
	<esi:attempt></esi:attempt>
	<esi:except>fallback</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if result != "" {
		t.Errorf("empty attempt should produce empty output, got %q", result)
	}
}

func TestExtractChooseBlocks(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantWhenCount   int
		wantWhenTests   []string
		wantWhenBodies  []string
		wantOtherwise   string
	}{
		{
			name:          "single when true",
			input:         `<esi:choose><esi:when test="true">body</esi:when></esi:choose>`,
			wantWhenCount: 1,
			wantWhenTests: []string{"true"},
			wantWhenBodies: []string{"body"},
			wantOtherwise: "",
		},
		{
			name:          "multiple whens with otherwise",
			input:         `<esi:choose><esi:when test="true">a</esi:when><esi:when test="false">b</esi:when><esi:otherwise>c</esi:otherwise></esi:choose>`,
			wantWhenCount: 2,
			wantWhenTests: []string{"true", "false"},
			wantWhenBodies: []string{"a", "b"},
			wantOtherwise: "c",
		},
		{
			name:          "otherwise only",
			input:         `<esi:choose><esi:otherwise>fallback</esi:otherwise></esi:choose>`,
			wantWhenCount: 0,
			wantOtherwise: "fallback",
		},
		{
			name:          "no whens, no otherwise",
			input:         `<esi:choose></esi:choose>`,
			wantWhenCount: 0,
			wantOtherwise: "",
		},
		{
			name:          "when with nested choose inside",
			input:         `<esi:choose><esi:when test="true"><esi:choose><esi:when test="false">nested</esi:when></esi:choose></esi:when></esi:choose>`,
			wantWhenCount: 1,
			wantWhenTests: []string{"true"},
			wantWhenBodies: []string{"<esi:choose><esi:when test=\"false\">nested</esi:when></esi:choose>"},
			wantOtherwise: "",
		},
		{
			name:          "when with include inside",
			input:         `<esi:choose><esi:when test="true"><esi:include src="/fragment"/></esi:when><esi:otherwise>fallback</esi:otherwise></esi:choose>`,
			wantWhenCount: 1,
			wantWhenTests: []string{"true"},
			wantWhenBodies: []string{"<esi:include src=\"/fragment\"/>"},
			wantOtherwise: "fallback",
		},
		{
			name:          "test attribute with extra whitespace",
			input:         `<esi:choose><esi:when test=" true ">body</esi:when></esi:choose>`,
			wantWhenCount: 1,
			wantWhenTests: []string{" true "},
			wantWhenBodies: []string{"body"},
			wantOtherwise: "",
		},
		{
			name:          "choose with body content before when (should be ignored)",
			input:         `<esi:choose>ignored text<esi:when test="true">body</esi:when></esi:choose>`,
			wantWhenCount: 1,
			wantWhenTests: []string{"true"},
			wantWhenBodies: []string{"body"},
			wantOtherwise: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			whens, otherwise := extractChooseBlocks(tt.input)
			if len(whens) != tt.wantWhenCount {
				t.Errorf("whens = %d, want %d", len(whens), tt.wantWhenCount)
			}
			if otherwise != tt.wantOtherwise {
				t.Errorf("otherwise = %q, want %q", otherwise, tt.wantOtherwise)
			}
			for i, w := range whens {
				if i >= len(tt.wantWhenTests) {
					break
				}
				if w.Test != tt.wantWhenTests[i] {
					t.Errorf("whens[%d].Test = %q, want %q", i, w.Test, tt.wantWhenTests[i])
				}
				if w.Body != tt.wantWhenBodies[i] {
					t.Errorf("whens[%d].Body = %q, want %q", i, w.Body, tt.wantWhenBodies[i])
				}
			}
		})
	}
}

func TestEvaluateTest(t *testing.T) {
	config := CreateDefaultConfig()
	tests := []struct {
		name   string
		expr   string
		expect bool
	}{
		{"true literal", "true", true},
		{"TRUE uppercase", "TRUE", false},
		{"false literal", "false", false},
		{"1 is true", "1", true},
		{"0 is false", "0", false},
		{"empty string is false", "", false},
		{"whitespace is false", "  ", false},
		{"random string is false", "some expression", false},
		{"true with spaces", "  true  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluateTest(tt.expr, config)
			if result != tt.expect {
				t.Errorf("evaluateTest(%q) = %v, want %v", tt.expr, result, tt.expect)
			}
		})
	}
}

func TestEvaluateTestWithVariables(t *testing.T) {
	config := CreateDefaultConfig()
	config.Variables = map[string]string{"FLAG": "true", "DISABLED": "false"}

	tests := []struct {
		name   string
		expr   string
		expect bool
	}{
		{"$(FLAG) resolves to true", "$(FLAG)", true},
		{"$(DISABLED) resolves to false", "$(DISABLED)", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluateTest(tt.expr, config)
			if result != tt.expect {
				t.Errorf("evaluateTest(%q) = %v, want %v", tt.expr, result, tt.expect)
			}
		})
	}
}

func TestChooseTrueRendersWhenBody(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:choose>
	<esi:when test="true">WHEN_BODY</esi:when>
	<esi:otherwise>OTHERWISE</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "WHEN_BODY") {
		t.Errorf("expected WHEN_BODY in result, got %q", result)
	}
	if strings.Contains(result, "OTHERWISE") {
		t.Errorf("otherwise should NOT be rendered when when matches, got %q", result)
	}
}

func TestChooseFalseRendersOtherwise(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:choose>
	<esi:when test="false">WHEN_BODY</esi:when>
	<esi:otherwise>OTHERWISE_BODY</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "OTHERWISE_BODY") {
		t.Errorf("expected OTHERWISE_BODY in result, got %q", result)
	}
	if strings.Contains(result, "WHEN_BODY") {
		t.Errorf("when body should NOT be rendered when test=false, got %q", result)
	}
}

func TestChooseAllFalseNoOtherwiseEmpty(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:choose>
	<esi:when test="false">FIRST</esi:when>
	<esi:when test="0">SECOND</esi:when>
</esi:choose>`

	result := MESIParse(input, config)
	if result != "" {
		t.Errorf("expected empty output, got %q", result)
	}
}

func TestChooseFirstMatchWinsShortCircuit(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:choose>
	<esi:when test="true">FIRST_MATCH</esi:when>
	<esi:when test="true">SECOND_MATCH</esi:when>
	<esi:otherwise>OTHERWISE</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "FIRST_MATCH") {
		t.Errorf("expected FIRST_MATCH in result, got %q", result)
	}
	if strings.Contains(result, "SECOND_MATCH") {
		t.Errorf("second when should NOT be rendered (short-circuit), got %q", result)
	}
	if strings.Contains(result, "OTHERWISE") {
		t.Errorf("otherwise should NOT be rendered when when matches, got %q", result)
	}
}

func TestChooseNestedIncludeInsideWhenIsProcessed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("INCLUDED_CONTENT"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<esi:choose>
	<esi:when test="true">
		before [<esi:include src="` + server.URL + `/fragment"/>] after
	</esi:when>
	<esi:otherwise>fallback</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "INCLUDED_CONTENT") {
		t.Errorf("include inside when should be processed, got %q", result)
	}
	if !strings.Contains(result, "before") || !strings.Contains(result, "after") {
		t.Errorf("static text around include should be preserved, got %q", result)
	}
	if strings.Contains(result, "fallback") {
		t.Errorf("otherwise should not be rendered, got %q", result)
	}
}

func TestChooseEmptyTestAttributeIsFalse(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:choose>
	<esi:when test="">WHEN_BODY</esi:when>
	<esi:otherwise>OTHERWISE_BODY</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "OTHERWISE_BODY") {
		t.Errorf("expected OTHERWISE_BODY when test is empty, got %q", result)
	}
}

func TestChooseMalformedWhenWithoutCloseTag(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	// Missing </esi:when> inside properly-closed <esi:choose>
	// The tokenizer produces an ESI_CHOOSE token; we should not panic.
	input := `<esi:choose><esi:when test="true">body</esi:choose>`

	result := MESIParse(input, config)
	_ = result
}

func TestChooseMalformedOtherwiseWithoutCloseTag(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	// Missing </esi:otherwise> inside properly-closed <esi:choose>
	input := `<esi:choose><esi:when test="false">when</esi:when><esi:otherwise>body</esi:choose>`

	result := MESIParse(input, config)
	_ = result
}

func TestChooseMalformedMissingCloseTag(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	// Missing </esi:choose> should not crash
	input := `<esi:choose>
	<esi:when test="true">body
	<esi:otherwise>fallback`

	result := MESIParse(input, config)
	_ = result
}

func TestChooseMultipleBlocks(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:choose>
	<esi:when test="true">FIRST</esi:when>
</esi:choose>
---
<esi:choose>
	<esi:when test="false">NOT_RENDERED</esi:when>
	<esi:otherwise>SECOND</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "FIRST") {
		t.Errorf("expected FIRST in result, got %q", result)
	}
	if !strings.Contains(result, "SECOND") {
		t.Errorf("expected SECOND in result, got %q", result)
	}
	if strings.Contains(result, "NOT_RENDERED") {
		t.Errorf("NOT_RENDERED should not appear, got %q", result)
	}
}

func TestChooseRepeatedInDocument(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:choose>
	<esi:when test="true">A</esi:when>
	<esi:otherwise>B</esi:otherwise>
</esi:choose>
<esi:choose>
	<esi:when test="true">C</esi:when>
	<esi:otherwise>D</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "A") {
		t.Errorf("expected A in result, got %q", result)
	}
	if !strings.Contains(result, "C") {
		t.Errorf("expected C in result, got %q", result)
	}
}

func TestESITryAndChooseE2EFixture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/variant-a":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Variant A Content"))
		case "/variant-b":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Variant B Content"))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not Found"))
		}
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	// Test 1: choose + try combination
	input := `<esi:choose>
	<esi:when test="true">
		<esi:try>
			<esi:attempt>
				<esi:include src="` + server.URL + `/variant-a"/>
			</esi:attempt>
			<esi:except>
				fallback
			</esi:except>
		</esi:try>
	</esi:when>
	<esi:otherwise>
		<esi:include src="` + server.URL + `/variant-b"/>
	</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "Variant A Content") {
		t.Errorf("expected Variant A Content, got %q", result)
	}
	if strings.Contains(result, "Variant B Content") {
		t.Errorf("Variant B should not appear")
	}
	if strings.Contains(result, "fallback") {
		t.Errorf("fallback should not appear")
	}
}

func TestESITryChooseNestedInAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("INCLUDED"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	// choose inside try/attempt
	input := `<esi:try>
	<esi:attempt>
		before
		<esi:choose>
			<esi:when test="true">
				<esi:include src="` + server.URL + `/fragment"/>
			</esi:when>
			<esi:otherwise>otherwise body</esi:otherwise>
		</esi:choose>
		after
	</esi:attempt>
	<esi:except>
		except body
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "INCLUDED") {
		t.Errorf("expected INCLUDED from choose inside attempt, got %q", result)
	}
	if !strings.Contains(result, "before") || !strings.Contains(result, "after") {
		t.Errorf("static text around choose should be preserved, got %q", result)
	}
	if strings.Contains(result, "otherwise body") {
		t.Errorf("otherwise should not be rendered when when matches")
	}
	if strings.Contains(result, "except body") {
		t.Errorf("except should not be rendered on success")
	}
}

func TestESITryChooseNestedInExcept(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	// choose inside try/except
	input := `<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/missing"/>
	</esi:attempt>
	<esi:except>
		<esi:choose>
			<esi:when test="true">except when body</esi:when>
			<esi:otherwise>except otherwise</esi:otherwise>
		</esi:choose>
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "except when body") {
		t.Errorf("expected except when body, got %q", result)
	}
}

func TestESITryChooseNestedNestedChoose(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	// Nested choose blocks
	input := `<esi:choose>
	<esi:when test="true">
		outer true:
		<esi:choose>
			<esi:when test="false">inner false</esi:when>
			<esi:otherwise>inner otherwise</esi:otherwise>
		</esi:choose>
	</esi:when>
	<esi:otherwise>
		outer otherwise
	</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "outer true") {
		t.Errorf("expected outer true text, got %q", result)
	}
	if !strings.Contains(result, "inner otherwise") {
		t.Errorf("expected inner otherwise, got %q", result)
	}
	if strings.Contains(result, "inner false") {
		t.Errorf("inner false should not be rendered")
	}
	if strings.Contains(result, "outer otherwise") {
		t.Errorf("outer otherwise should not be rendered")
	}
}

func TestESITryChooseWithVarsInTest(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0
	config.Variables = map[string]string{"FEATURE_ENABLED": "true"}

	input := `<esi:choose>
	<esi:when test="$(FEATURE_ENABLED)">feature is on</esi:when>
	<esi:otherwise>feature is off</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "feature is on") {
		t.Errorf("expected 'feature is on', got %q", result)
	}
}

func TestESITryChooseWithVarsInTestFalse(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0
	config.Variables = map[string]string{"FEATURE_ENABLED": "false"}

	input := `<esi:choose>
	<esi:when test="$(FEATURE_ENABLED)">feature is on</esi:when>
	<esi:otherwise>feature is off</esi:otherwise>
</esi:choose>`

	result := MESIParse(input, config)
	if !strings.Contains(result, "feature is off") {
		t.Errorf("expected 'feature is off', got %q", result)
	}
}

func TestInlineBodyRenderedVerbatim(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:inline>Hello from inline</esi:inline>`
	result := MESIParse(input, config)
	expected := "Hello from inline"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestInlineIncludeInsideNotProcessed(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:inline><esi:include src="/fragment"/></esi:inline>`
	result := MESIParse(input, config)
	expected := `<esi:include src="/fragment"/>`
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestInlineWithHTMLMarkup(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:inline><div class="foo"><p>Hello</p></div></esi:inline>`
	result := MESIParse(input, config)
	expected := `<div class="foo"><p>Hello</p></div>`
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestInlineEmptyBody(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:inline></esi:inline>`
	result := MESIParse(input, config)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestInlineWithSurroundingContent(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `before<esi:inline>middle</esi:inline>after`
	result := MESIParse(input, config)
	expected := "beforemiddleafter"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestInlineWithAttributesPassthrough(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:inline name="foo">content</esi:inline>`
	result := MESIParse(input, config)
	expected := "content"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestInlineInsideAttemptRendersVerbatim(t *testing.T) {
	config := CreateDefaultConfig()
	config.MaxDepth = 0

	input := `<esi:try><esi:attempt><esi:inline><esi:include src="/x"/></esi:inline></esi:attempt></esi:try>`
	result := MESIParse(input, config)
	expected := `<esi:include src="/x"/>`
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestInlineInsideExceptRendersVerbatim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/fail"/>
	</esi:attempt>
	<esi:except>
		<esi:inline><esi:include src="/should-be-escaped"/></esi:inline>
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)
	if !strings.Contains(result, `<esi:include src="/should-be-escaped"/>`) {
		t.Errorf("expected escaped include in result, got %q", result)
	}
}

func TestProcessInlineBlock(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "<esi:inline>hello</esi:inline>", "hello"},
		{"with newlines", "<esi:inline>\nhello\n</esi:inline>", "\nhello\n"},
		{"with attributes", `<esi:inline name="foo">body</esi:inline>`, "body"},
		{"empty", "<esi:inline></esi:inline>", ""},
		{"no closing tag", "<esi:inline>hello", "hello"},
		{"with ESI inside", "<esi:inline><esi:include src=\"/x\"/></esi:inline>", "<esi:include src=\"/x\"/>"},
		{"nested angle brackets", "<esi:inline>a > b</esi:inline>", "a > b"},
		{"multi-byte UTF-8", "<esi:inline>zażółć gęślą jaźń</esi:inline>", "zażółć gęślą jaźń"},
		{"quote in attribute", `<esi:inline data-foo="a>b">body</esi:inline>`, "body"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := processInlineBlock(c.input)
			if result != c.expected {
				t.Errorf("processInlineBlock(%q) = %q, want %q", c.input, result, c.expected)
			}
		})
	}
}

func TestESITryE2EFixture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Hello World"))
		case "/status/code/500":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(http.StatusText(http.StatusInternalServerError)))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(http.StatusText(http.StatusNotFound)))
		}
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<esi:try>
	<esi:attempt>
		before [<esi:include src="` + server.URL + `/hello"/>] after
	</esi:attempt>
	<esi:except>
		except should not appear
	</esi:except>
</esi:try>
---
<esi:try>
	<esi:attempt>
		<esi:include src="` + server.URL + `/status/code/500"/>
	</esi:attempt>
	<esi:except>
		fallback rendered
	</esi:except>
</esi:try>
---
<esi:try>
	<esi:attempt>
		<esi:include onerror="continue" src="` + server.URL + `/status/code/500"/>
		after error
	</esi:attempt>
	<esi:except>
		except should not appear
	</esi:except>
</esi:try>`

	result := MESIParse(input, config)

	if !strings.Contains(result, "before [Hello World] after") {
		t.Errorf("first try: expected include content, got %q", result)
	}
	if !strings.Contains(result, "fallback rendered") {
		t.Errorf("second try: expected except fallback, got %q", result)
	}
	if strings.Contains(result, "except should not appear") {
		t.Errorf("except should not be rendered when not triggered")
	}
	if !strings.Contains(result, "after error") {
		t.Errorf("third try: onerror=continue content should appear, got %q", result)
	}
}


func TestMESIParseABRatioRejectsInvalidInput(t *testing.T) {
	// The mock upstream must never be touched when the ab-ratio attribute
	// fails validation. We count requests; any non-zero count indicates
	// the legacy "silent default" behaviour leaked back in.
	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("UPSTREAM"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.IncludeErrorMarker = "[ERR]"

	cases := []struct {
		name        string
		abRatio     string
		wantErrMark bool
		wantContent string // substring inside [ERR] when err-mark path is taken
	}{
		{"missing colon", "7030", true, "[ERR]"},
		{"too many parts", "70:30:10", true, "[ERR]"},
		{"both zero", "0:0", true, "[ERR]"},
		{"negative A", "-5:10", true, "[ERR]"},
		{"A above max", "1000001:1", true, "[ERR]"},
		{"decimal value", "70.5:29.5", true, "[ERR]"},
		{"non-numeric", "abc:def", true, "[ERR]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstreamHits.Store(0)
			input := `<esi:include fetch-mode="ab" ab-ratio="` + tc.abRatio + `" src="` + server.URL + `/A" alt="` + server.URL + `/B"/>`
			result := MESIParse(input, config)

			if upstreamHits.Load() != 0 {
				t.Errorf("upstream fetched %d times despite invalid ab-ratio %q — silent fallback leaked through",
					upstreamHits.Load(), tc.abRatio)
			}
			if tc.wantErrMark && !strings.Contains(result, tc.wantContent) {
				t.Errorf("expected IncludeErrorMarker %q in result for ab-ratio=%q, got %q", tc.wantContent, tc.abRatio, result)
			}
		})
	}
}

func TestMESIParseABRatioAcceptedBoundaries(t *testing.T) {
	// Accepted boundaries must return the upstream body. Boundary at
	// MaxABRatio is the largest value we promise to honour.
	cases := []struct {
		name    string
		abRatio string
		want    string
	}{
		{"min 0:1", "0:1", "B"},
		{"min 1:0", "1:0", "A"},
		{"max 1000000:0", "1000000:0", "A"},
		{"max 0:1000000", "0:1000000", "B"},
		{"both at max 1000000:1000000", "1000000:1000000", ""}, // pick is rng-dependent; just verify it returns A or B (not error mark)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.want))
			}))
			defer server.Close()

			config := CreateDefaultConfig()
			config.DefaultUrl = server.URL + "/"
			config.MaxDepth = 1
			config.BlockPrivateIPs = false
			config.IncludeErrorMarker = "[ERR]"

			if tc.name == "both at max 1000000:1000000" {
				// rng-driven; just verify no error mark shows up
				input := `<esi:include fetch-mode="ab" ab-ratio="` + tc.abRatio + `" src="` + server.URL + `/A" alt="` + server.URL + `/B"/>`
				result := MESIParse(input, config)
				if strings.Contains(result, "[ERR]") {
					t.Errorf("both-at-max should not error, got %q", result)
				}
				return
			}

			input := `<esi:include fetch-mode="ab" ab-ratio="` + tc.abRatio + `" src="` + server.URL + `/A" alt="` + server.URL + `/B"/>`
			result := MESIParse(input, config)
			if result != tc.want {
				t.Errorf("ab-ratio=%q -> %q, want %q", tc.abRatio, result, tc.want)
			}
		})
	}
}

func TestMESIParseMaxDepthRejectsInvalidInput(t *testing.T) {
	// The legacy bug (#317) was: a hostile `max-depth` override silently
	// downgraded the parent's MaxDepth (the most striking case being a
	// wrap-to-zero on MaxUint64-class inputs), turning the include into
	// a parse-only tag. The fix says: an invalid override must surface
	// through the logger while letting the parent's depth remain intact.
	//
	// We mock an upstream that returns a nested <esi:include> pointing
	// at a second endpoint we also mock. With the parent MaxDepth set
	// high enough to recurse once into the inner include, an invalid
	// override must leave both fetches intact (the parent's depth
	// survives); a working override that respects MaxDepth=5 shows the
	// same fetch count, so we additionally verify the response renders
	// the inner result rather than `IncludeErrorMarker`.
	innerBody := "INNER_TEXT"
	var innerHits atomic.Int32
	innerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(innerBody))
	}))
	defer innerSrv.Close()

	var outerHits atomic.Int32
	outerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		outerHits.Add(1)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "outer: [<esi:include src=%q />]", innerSrv.URL+"/inner")
	}))
	defer outerSrv.Close()

	cfg := CreateDefaultConfig()
	cfg.DefaultUrl = outerSrv.URL + "/"
	cfg.MaxDepth = 5
	cfg.BlockPrivateIPs = false
	cfg.IncludeErrorMarker = "[ERR]"

	cases := []struct {
		name     string
		maxDepth string
	}{
		{"alpha", "abc"},
		{"negative", "-1"},
		{"decimal", "5.5"},
		{"above documented maximum", "10001"},
		{"above uint64 max (true overflow)", "99999999999999999999999999999999999"},
		{"huge but in uint64 (still huge)", "18446744073709551615"},
		{"uint64 max minus one", "18446744073709551614"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			innerHits.Store(0)
			outerHits.Store(0)

			input := fmt.Sprintf(
				`<esi:include src=%q max-depth=%q />`,
				outerSrv.URL+"/outer", tc.maxDepth,
			)
			result := MESIParse(input, cfg)

			if outerHits.Load() != 1 {
				t.Errorf("outer include fetched %d times for invalid max-depth=%q (want 1: parent's depth must allow fetching the outer response)",
					outerHits.Load(), tc.maxDepth)
			}
			if innerHits.Load() != 1 {
				t.Errorf("inner include fetched %d times for invalid max-depth=%q (want 1: parent's depth must NOT be downgraded)",
					innerHits.Load(), tc.maxDepth)
			}
			if strings.Contains(result, "[ERR]") {
				t.Errorf("invalid max-depth=%q must not invalidate the include entirely; result=%q",
					tc.maxDepth, result)
			}
			if !strings.Contains(result, innerBody) {
				t.Errorf("inner body %q not rendered for invalid max-depth=%q; result=%q — parent's MaxDepth was not applied",
					innerBody, tc.maxDepth, result)
			}
		})
	}
}

func TestMESIParseNegativeMaxConcurrentRequestsProducesWarning(t *testing.T) {
	var log recordingLogger
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.MaxConcurrentRequests = -5
	config.Logger = &log

	input := `<!--esi <esi:include src="` + server.URL + `/test"/>-->`
	result := MESIParse(input, config)

	if !strings.Contains(result, "ok") {
		t.Errorf("expected include to succeed despite negative MaxConcurrentRequests, got %q", result)
	}
	if !log.containsMsg("max_concurrent_requests_invalid") {
		t.Errorf("expected logger to receive max_concurrent_requests_invalid for negative MaxConcurrentRequests=-5")
	}
}

func TestMESIParseMaxDepthValidOverridesApplied(t *testing.T) {
	// Positive path: a well-formed max-depth override must tighten the
	// parent's depth exactly as documented.
	cases := []struct {
		name     string
		maxDepth string
		wantHits int32 // expected count of upstream fetches
	}{
		{"max-depth=2 honoured", "2", 1},
		{"max-depth=10000 honoured", "10000", 1},
		{"max-depth=0 reduces to depth+1=1", "0", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			}))
			defer server.Close()

			config := CreateDefaultConfig()
			config.DefaultUrl = server.URL + "/"
			config.MaxDepth = 5
			config.BlockPrivateIPs = false

			input := `<esi:include src="` + server.URL + `/x" max-depth="` + tc.maxDepth + `"/>`
			MESIParse(input, config)

			if hits.Load() != tc.wantHits {
				t.Errorf("upstream fetched %d times with max-depth=%q (want %d)",
					hits.Load(), tc.maxDepth, tc.wantHits)
			}
		})
	}
}
