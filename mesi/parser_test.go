package mesi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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
			expected:   "esi max depth",
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
