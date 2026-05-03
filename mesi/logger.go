package mesi

import (
	"fmt"
	"io"
	"os"
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
func (DiscardLogger) Warn(msg string, keyvals ...interface{}) {}

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
	fmt.Fprintf(l.w, "%s %s %s", now, level, msg)
	if len(keyvals) > 0 {
		fmt.Fprint(l.w, " ")
		for i := 0; i < len(keyvals); i += 2 {
			if i+1 < len(keyvals) {
				fmt.Fprintf(l.w, "%v=%v", keyvals[i], keyvals[i+1])
			} else {
				fmt.Fprintf(l.w, "%v=MISSING", keyvals[i])
			}
			if i+2 < len(keyvals) {
				fmt.Fprint(l.w, " ")
			}
		}
	}
	fmt.Fprintln(l.w)
}
