package mesi

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type Logger interface {
	Debug(msg string, keyvals ...interface{})
}

type LoggerWarn interface {
	Logger
	Warn(msg string, keyvals ...interface{})
}

type DiscardLogger struct{}

func (DiscardLogger) Debug(msg string, keyvals ...interface{}) {}
func (DiscardLogger) Warn(msg string, keyvals ...interface{})  {}

type DefaultLogger struct {
	w io.Writer
}

func DefaultLoggerNew() DefaultLogger {
	return DefaultLogger{w: os.Stderr}
}

func (l DefaultLogger) Debug(msg string, keyvals ...interface{}) {
	l.log("DEBUG", msg, keyvals...)
}

func (l DefaultLogger) Warn(msg string, keyvals ...interface{}) {
	l.log("WARN", msg, keyvals...)
}

func (l DefaultLogger) log(level, msg string, keyvals ...interface{}) {
	now := time.Now().Format(time.RFC3339)
	// Log lines are built in memory and written with a single call, so an
	// unwritable stream yields one short write instead of a partial line and
	// the report cannot be interleaved with concurrent writers.
	var b strings.Builder
	b.WriteString(now)
	b.WriteByte(' ')
	b.WriteString(level)
	b.WriteByte(' ')
	b.WriteString(msg)
	if len(keyvals) > 0 {
		b.WriteByte(' ')
		for i := 0; i < len(keyvals); i += 2 {
			if i > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%v=", keyvals[i])
			if i+1 < len(keyvals) {
				fmt.Fprintf(&b, "%v", keyvals[i+1])
			} else {
				b.WriteString("MISSING")
			}
		}
	}
	b.WriteByte('\n')
	_, _ = l.w.Write([]byte(b.String()))
}
