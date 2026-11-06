package traefik

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureDebugStderr runs fn with os.Stderr replaced by a pipe and returns
// everything written to it. The core's DefaultLogger resolves os.Stderr
// when the logger is built for a parse, so this captures real plugin output
// — there is no logger seam to mock.
func captureDebugStderr(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })
	defer func() { _ = w.Close() }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

func newDebugPlugin(t *testing.T, debug bool) http.Handler {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>debug test</body></html>"))
	})

	config := CreateConfig()
	config.Debug = debug

	p, err := New(context.Background(), handler, config, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestDebugDefaultFalse(t *testing.T) {
	if CreateConfig().Debug {
		t.Error("Debug must be false when the option is absent")
	}
}

func TestDebugAbsentDoesNotLog(t *testing.T) {
	out := captureDebugStderr(t, func() {
		rec := httptest.NewRecorder()
		newDebugPlugin(t, false).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	})

	if strings.Contains(out, "DEBUG") {
		t.Errorf("absent debug must not log debug events, got %q", out)
	}
}

func TestDebugTrueLogsToStderr(t *testing.T) {
	out := captureDebugStderr(t, func() {
		rec := httptest.NewRecorder()
		newDebugPlugin(t, true).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	})

	if !strings.Contains(out, "DEBUG parse_start") {
		t.Errorf("debug: true must propagate to the parser and log parse_start, got %q", out)
	}
}
