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
