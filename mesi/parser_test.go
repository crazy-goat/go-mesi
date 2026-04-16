package mesi

import (
	"net/http"
	"net/http/httptest"
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

func TestMaxConcurrentRequestsLimitsConcurrency(t *testing.T) {
	var maxConcurrent int64
	var current atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inc := current.Add(1)
		old := atomic.LoadInt64(&maxConcurrent)
		if inc > old {
			atomic.CompareAndSwapInt64(&maxConcurrent, old, inc)
		}

		time.Sleep(50 * time.Millisecond)

		current.Add(-1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxConcurrentRequests = 2
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	input := `<!--esi <esi:include src="` + server.URL + `/1"/>--><!--esi <esi:include src="` + server.URL + `/2"/>--><!--esi <esi:include src="` + server.URL + `/3"/>--><!--esi <esi:include src="` + server.URL + `/4"/>--><!--esi <esi:include src="` + server.URL + `/5"/>-->`

	MESIParse(input, config)

	if atomic.LoadInt64(&maxConcurrent) > 2 {
		t.Errorf("Max concurrent = %d, expected <= 2", atomic.LoadInt64(&maxConcurrent))
	}
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
