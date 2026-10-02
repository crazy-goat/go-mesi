package mesi

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestDefaultLoggerFormatsKeyValuePairs(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		msg      string
		keyvals  []interface{}
		wantTail string
	}{
		{
			name:     "no keyvals",
			level:    "DEBUG",
			msg:      "hello",
			keyvals:  nil,
			wantTail: " DEBUG hello\n",
		},
		{
			name:     "one pair",
			level:    "WARN",
			msg:      "cache miss",
			keyvals:  []interface{}{"url", "http://example.com/"},
			wantTail: " WARN cache miss url=http://example.com/\n",
		},
		{
			name:     "two pairs",
			level:    "DEBUG",
			msg:      "fetch",
			keyvals:  []interface{}{"url", "http://a/", "status", 200},
			wantTail: " DEBUG fetch url=http://a/ status=200\n",
		},
		{
			name:     "odd keyvals renders MISSING",
			level:    "WARN",
			msg:      "truncated",
			keyvals:  []interface{}{"url", "http://a/", "status"},
			wantTail: " WARN truncated url=http://a/ status=MISSING\n",
		},
		{
			name:     "empty key and value",
			level:    "DEBUG",
			msg:      "empty",
			keyvals:  []interface{}{"", ""},
			wantTail: " DEBUG empty =\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := DefaultLogger{w: &buf}

			if tt.level == "WARN" {
				logger.Warn(tt.msg, tt.keyvals...)
			} else {
				logger.Debug(tt.msg, tt.keyvals...)
			}

			got := buf.String()
			if !strings.HasSuffix(got, tt.wantTail) {
				t.Errorf("log line = %q, want suffix %q", got, tt.wantTail)
			}
		})
	}
}

func TestDefaultLoggerPrefixesRFC3339Timestamp(t *testing.T) {
	var buf bytes.Buffer
	logger := DefaultLogger{w: &buf}

	before := time.Now().Add(-time.Second)
	logger.Debug("msg")
	after := time.Now().Add(time.Second)

	got := buf.String()
	// "<RFC3339> DEBUG msg\n" — the timestamp is the first space-separated field.
	fields := strings.SplitN(got, " ", 3)
	if len(fields) != 3 {
		t.Fatalf("log line = %q, want 3 space-separated fields", got)
	}

	stamp, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		t.Fatalf("timestamp %q is not RFC3339: %v", fields[0], err)
	}
	if stamp.Before(before) || stamp.After(after) {
		t.Errorf("timestamp = %v, want within [%v, %v]", stamp, before, after)
	}
	if rest := fields[1] + " " + fields[2]; rest != "DEBUG msg\n" {
		t.Errorf("remainder = %q, want %q", rest, "DEBUG msg\n")
	}
}

// A line must reach the writer whole, so a concurrent writer cannot interleave
// with it and an unwritable stream cannot leave a partial record behind.
func TestDefaultLoggerWritesEachLineAtomically(t *testing.T) {
	var buf bytes.Buffer
	logger := DefaultLogger{w: &buf}

	logger.Debug("first", "a", 1)
	logger.Warn("second", "b", 2)

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), buf.String())
	}
	for i, want := range []string{"DEBUG first a=1", "WARN second b=2"} {
		fields := strings.Fields(lines[i])
		if got := strings.Join(fields[1:], " "); got != want {
			t.Errorf("line %d = %q, want %q", i, got, want)
		}
	}
}
