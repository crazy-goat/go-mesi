package mesi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type logEntry struct {
	msg     string
	keyvals []interface{}
}

var _ Logger = &recordingLogger{}

type recordingLogger struct {
	entries []logEntry
}

func (l *recordingLogger) Debug(msg string, keyvals ...interface{}) {
	l.entries = append(l.entries, logEntry{msg: msg, keyvals: keyvals})
}

func (l *recordingLogger) Warn(msg string, keyvals ...interface{}) {
	l.entries = append(l.entries, logEntry{msg: msg, keyvals: keyvals})
}

func (l *recordingLogger) containsMsg(substr string) bool {
	for _, e := range l.entries {
		if strings.Contains(e.msg, substr) {
			return true
		}
	}
	return false
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

func TestParseABValidRatio(t *testing.T) {
	token := &esiIncludeToken{ABRatio: "70:30"}
	ratio := token.parseAB()
	if ratio.A != 70 {
		t.Errorf("A = %d, want 70", ratio.A)
	}
	if ratio.B != 30 {
		t.Errorf("B = %d, want 30", ratio.B)
	}
}

func TestParseABInvalidRatioDefaults(t *testing.T) {
	cases := []struct {
		name  string
		ratio string
		wantA uint
		wantB uint
	}{
		{"missing colon", "7030", 50, 50},
		{"too many parts", "70:30:10", 50, 50},
		{"non-numeric", "abc:def", 50, 50},
		{"empty string", "", 50, 50},
		{"both zero", "0:0", 50, 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := &esiIncludeToken{ABRatio: tc.ratio}
			ratio := token.parseAB()
			if ratio.A != tc.wantA {
				t.Errorf("A = %d, want %d", ratio.A, tc.wantA)
			}
			if ratio.B != tc.wantB {
				t.Errorf("B = %d, want %d", ratio.B, tc.wantB)
			}
		})
	}
}

func TestSelectUrlChoosesBasedOnRatio(t *testing.T) {
	token := &esiIncludeToken{Src: "/src.html", Alt: "/alt.html"}

	t.Run("rng_in_a_range_selects_src", func(t *testing.T) {
		ratio := abRatio{A: 80, B: 20}
		for i := 0; i < 80; i++ {
			rng := func(int) int { return i }
			selected := ratio.selectUrl(token, rng)
			if selected != "/src.html" {
				t.Errorf("rng=%d: expected src, got %q", i, selected)
			}
		}
	})

	t.Run("rng_in_b_range_selects_alt", func(t *testing.T) {
		ratio := abRatio{A: 80, B: 20}
		for i := 80; i < 100; i++ {
			rng := func(int) int { return i }
			selected := ratio.selectUrl(token, rng)
			if selected != "/alt.html" {
				t.Errorf("rng=%d: expected alt, got %q", i, selected)
			}
		}
	})

	t.Run("no_alt_returns_src_regardless_of_rng", func(t *testing.T) {
		noAltToken := &esiIncludeToken{Src: "/src.html", Alt: ""}
		ratio := abRatio{A: 50, B: 50}
		for _, v := range []int{0, 49, 50, 99} {
			rng := func(int) int { return v }
			selected := ratio.selectUrl(noAltToken, rng)
			if selected != "/src.html" {
				t.Errorf("rng=%d: expected src when no alt, got %q", v, selected)
			}
		}
	})

	t.Run("zero_sum_returns_src", func(t *testing.T) {
		ratio := abRatio{A: 0, B: 0}
		for _, v := range []int{0, 50} {
			rng := func(int) int { return v }
			selected := ratio.selectUrl(token, rng)
			if selected != "/src.html" {
				t.Errorf("rng=%d: expected src when sum=0, got %q", v, selected)
			}
		}
	})
}

func TestSelectUrlNoAltReturnsSrc(t *testing.T) {
	token := &esiIncludeToken{Src: "/src.html", Alt: ""}
	ratio := abRatio{A: 50, B: 50}
	selected := ratio.selectUrl(token, nil)
	if selected != "/src.html" {
		t.Errorf("selectUrl() = %q, want %q", selected, "/src.html")
	}
}

func TestFetchFallbackPrimaryFailsThenAlt(t *testing.T) {
	primaryCalled := false
	altCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			primaryCalled = true
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/alt" {
			altCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("alt content"))
			return
		}
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src: server.URL + "/primary",
		Alt: server.URL + "/alt",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	data, _, err := fetchFallback(token, config)

	if !primaryCalled {
		t.Error("primary URL was not called")
	}
	if !altCalled {
		t.Error("alt URL was not called after primary failed")
	}
	if err != nil {
		t.Errorf("fetchFallback() error = %v", err)
	}
	if data != "alt content" {
		t.Errorf("data = %q, want %q", data, "alt content")
	}
}

func TestFetchConcurrentHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content from " + r.URL.Path))
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src:       server.URL + "/src",
		Alt:       server.URL + "/alt",
		FetchMode: "concurrent",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	data, _, err := fetchConcurrent(token, config)

	if err != nil {
		t.Errorf("fetchConcurrent() error = %v", err)
	}
	if data == "" {
		t.Error("fetchConcurrent() returned empty data")
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

	data, _ := token.toString(config)
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

	data, _ := token.toString(config)
	if data != "<!-- esi error -->" {
		t.Errorf("toString() = %q, want %q", data, "<!-- esi error -->")
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

	data, _ := token.toString(config)
	if data != "" {
		t.Errorf("toString() = %q, want empty string (onerror=continue)", data)
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

	data, _ := token.toString(config)
	if data != "fallback content" {
		t.Errorf("toString() = %q, want %q", data, "fallback content")
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

	data, _ := token.toString(config)
	if data != "" {
		t.Errorf("toString() = %q, want empty string (no error leak)", data)
	}
	if !log.containsMsg("include_failed") {
		t.Error("expected include_failed log entry")
	}
}
