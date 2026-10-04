package mesi

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type logEntry struct {
	msg     string
	keyvals []interface{}
}

var _ Logger = &recordingLogger{}

// recordingLogger collects log entries for assertions. MESIParse drains
// includes through a worker pool, so several goroutines can log concurrently:
// the mutex makes the recorder safe in tests that parse more than one include.
type recordingLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

func (l *recordingLogger) Debug(msg string, keyvals ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{msg: msg, keyvals: keyvals})
}

func (l *recordingLogger) Warn(msg string, keyvals ...interface{}) {
	l.Debug(msg, keyvals...)
}

func (l *recordingLogger) containsMsg(substr string) bool {
	return l.countMsg(substr) > 0
}

// countMsg reports how many entries carry substr, so a test can pin that a
// diagnostic is emitted once per render instead of once per include.
func (l *recordingLogger) countMsg(substr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, e := range l.entries {
		if strings.Contains(e.msg, substr) {
			count++
		}
	}
	return count
}

func TestParseIncludeAttributes(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		wantSrc       string
		wantAlt       string
		wantTimeout   string
		wantMaxDepth  string
		wantFetchMode string
		wantABRatio   string
	}{
		{
			name:    "valid src only",
			input:   `<esi:include src="/fragment.html"/>`,
			wantSrc: "/fragment.html",
		},
		{
			name:    "with alt attribute",
			input:   `<esi:include src="/primary.html" alt="/fallback.html"/>`,
			wantSrc: "/primary.html",
			wantAlt: "/fallback.html",
		},
		{
			name:        "with timeout",
			input:       `<esi:include src="/fragment.html" timeout="5000"/>`,
			wantSrc:     "/fragment.html",
			wantTimeout: "5000",
		},
		{
			name:         "with max-depth",
			input:        `<esi:include src="/fragment.html" max-depth="3"/>`,
			wantSrc:      "/fragment.html",
			wantMaxDepth: "3",
		},
		{
			name:          "with fetch-mode",
			input:         `<esi:include src="/fragment.html" fetch-mode="concurrent"/>`,
			wantSrc:       "/fragment.html",
			wantFetchMode: "concurrent",
		},
		{
			name:        "with ab-ratio",
			input:       `<esi:include src="/a.html" alt="/b.html" ab-ratio="70:30"/>`,
			wantSrc:     "/a.html",
			wantAlt:     "/b.html",
			wantABRatio: "70:30",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := parseInclude(tc.input)
			if err != nil {
				t.Fatalf("parseInclude() error = %v", err)
			}
			if token.Src != tc.wantSrc {
				t.Errorf("Src = %q, want %q", token.Src, tc.wantSrc)
			}
			if tc.wantAlt != "" && token.Alt != tc.wantAlt {
				t.Errorf("Alt = %q, want %q", token.Alt, tc.wantAlt)
			}
			if tc.wantTimeout != "" && token.Timeout != tc.wantTimeout {
				t.Errorf("Timeout = %q, want %q", token.Timeout, tc.wantTimeout)
			}
			if tc.wantMaxDepth != "" && token.MaxDepth != tc.wantMaxDepth {
				t.Errorf("MaxDepth = %q, want %q", token.MaxDepth, tc.wantMaxDepth)
			}
			if tc.wantFetchMode != "" && token.FetchMode != tc.wantFetchMode {
				t.Errorf("FetchMode = %q, want %q", token.FetchMode, tc.wantFetchMode)
			}
			if tc.wantABRatio != "" && token.ABRatio != tc.wantABRatio {
				t.Errorf("ABRatio = %q, want %q", token.ABRatio, tc.wantABRatio)
			}
		})
	}
}

func TestParseIncludeMalformedXML(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"missing closing tag", `<esi:include src="/fragment.html"`},
		{"invalid attribute", `<esi:include src="/fragment.html" invalid=>`},
		{"empty input", ""},
		{"non-XML", `not xml at all`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseInclude(tc.input)
			if err == nil {
				t.Error("parseInclude() expected error, got nil")
			}
		})
	}
}

func TestToStringErrorDoesNotLeakInternalDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("SECRET_INTERNAL_DATA"))
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src: server.URL + "/fail",
	}

	log := &recordingLogger{}
	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.Logger = log

	data, _, _ := token.toString(config)
	if data != "" {
		t.Errorf("toString() = %q, want empty string (no error leak)", data)
	}
	if strings.Contains(data, "SECRET_INTERNAL_DATA") {
		t.Error("toString() leaked response body")
	}
	if strings.Contains(data, "500") {
		t.Error("toString() leaked status code")
	}
	if !log.containsMsg("include_failed") {
		t.Error("expected include_failed log entry")
	}
}

func TestIncludeErrorMarkerCustom(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src: server.URL + "/fail",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.IncludeErrorMarker = "<!-- esi error -->"

	data, _, err := token.toString(config)
	if data != "<!-- esi error -->" {
		t.Errorf("toString() = %q, want %q", data, "<!-- esi error -->")
	}
	if err == nil {
		t.Error("toString() expected error for unhandled include failure")
	}
}

// TestIncludeMaxInt64ResponseCapFailsLoud covers #448 at the include level:
// a MaxResponseSize the fetch path cannot enforce (math.MaxInt64 wraps the
// MaxResponseSize+1 read bound negative) must surface as an include error with
// a typed cause, never as a silently empty body. The diagnostic is emitted
// once per render, not once per include, the convention #329 established for a
// rejected configuration value.
func TestIncludeMaxInt64ResponseCapFailsLoud(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("upstream body"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.IncludeErrorMarker = "[ERR]"
	config.MaxResponseSize = math.MaxInt64
	log := &recordingLogger{}
	config.Logger = log

	token := &esiIncludeToken{Src: server.URL + "/include"}

	data, _, err := token.toString(config)
	if err == nil {
		t.Fatal("toString() expected an error for math.MaxInt64 MaxResponseSize, got nil")
	}
	var typed *ErrInvalidMaxResponseSize
	if !errors.As(err, &typed) {
		t.Errorf("toString() error = %T (%v), want a *ErrInvalidMaxResponseSize in the chain", err, err)
	}
	if data != "[ERR]" {
		t.Errorf("toString() = %q, want the IncludeErrorMarker %q", data, "[ERR]")
	}

	// The rendered page must show the failure, not an empty body where the
	// includes were, and the rejected value must be reported through the
	// logger so the cause does not get lost between the rendered error marker
	// and the operator. Two includes on one page, one warning.
	src := server.URL + "/include"
	input := "before<esi:include src=\"" + src + "\"></esi:include>mid<esi:include src=\"" + src + "\"></esi:include>after"
	if got := MESIParse(input, config); got != "before[ERR]mid[ERR]after" {
		t.Errorf("MESIParse() = %q, want %q", got, "before[ERR]mid[ERR]after")
	}
	if n := log.countMsg("max_response_size_invalid"); n != 1 {
		t.Errorf("max_response_size_invalid logged %d times for one page with two includes, want 1", n)
	}
}

// TestParseValidMaxResponseSizeIsNotWarned guards the hoisted diagnostic: a
// cap inside the accepted range must not produce a warning, including the
// documented unlimited value.
func TestParseValidMaxResponseSizeIsNotWarned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("upstream body"))
	}))
	defer server.Close()

	for _, limit := range []int64{0, 1024, MaxMaxResponseSize} {
		t.Run("max_response_size="+strconv.FormatInt(limit, 10), func(t *testing.T) {
			config := CreateDefaultConfig()
			config.MaxDepth = 1
			config.BlockPrivateIPs = false
			config.MaxResponseSize = limit
			log := &recordingLogger{}
			config.Logger = log

			input := "<esi:include src=\"" + server.URL + "/include\"></esi:include>"
			if got := MESIParse(input, config); got != "upstream body" {
				t.Errorf("MESIParse() = %q, want %q", got, "upstream body")
			}
			if n := log.countMsg("max_response_size_invalid"); n != 0 {
				t.Errorf("max_response_size_invalid logged %d times for an accepted cap, want 0", n)
			}
		})
	}
}

func TestToStringWithOnerrorContinue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src:     server.URL + "/fail",
		OnError: "continue",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	data, _, err := token.toString(config)
	if data != "" {
		t.Errorf("toString() = %q, want empty string (onerror=continue)", data)
	}
	if err != nil {
		t.Errorf("toString() unexpected error for onerror=continue: %v", err)
	}
}

func TestToStringWithFallbackContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src:     server.URL + "/fail",
		Content: "fallback content",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	data, _, err := token.toString(config)
	if data != "fallback content" {
		t.Errorf("toString() = %q, want %q", data, "fallback content")
	}
	if err != nil {
		t.Errorf("toString() unexpected error for fallback content: %v", err)
	}
}

func TestToStringWithMaxDepthExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called when max depth exceeded")
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src: server.URL + "/fragment",
	}

	log := &recordingLogger{}
	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 0
	config.BlockPrivateIPs = false
	config.Logger = log

	data, _, err := token.toString(config)
	if data != "" {
		t.Errorf("toString() = %q, want empty string (no error leak)", data)
	}
	if err == nil {
		t.Error("toString() expected error for max depth exceeded")
	}
	if !log.containsMsg("include_failed") {
		t.Error("expected include_failed log entry")
	}
}
