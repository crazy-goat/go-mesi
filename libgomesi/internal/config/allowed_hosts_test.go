package config

import (
	"testing"
)

// capturingLogger records Debug and Warn entries so tests can assert
// which severity the allowedHosts diagnostic used. It implements
// mesi.LoggerWarn (Warn included), so AllowedHosts should report
// through Warn.
type capturingLogger struct {
	debugs []string
	warns  []string
}

func (l *capturingLogger) Debug(msg string, keyvals ...interface{}) {
	l.debugs = append(l.debugs, msg)
}

func (l *capturingLogger) Warn(msg string, keyvals ...interface{}) {
	l.warns = append(l.warns, msg)
}

// debugOnlyLogger implements only mesi.Logger — no Warn method — to
// exercise the Debug fallback in AllowedHosts.
type debugOnlyLogger struct {
	debugs []string
}

func (l *debugOnlyLogger) Debug(msg string, keyvals ...interface{}) {
	l.debugs = append(l.debugs, msg)
}

func TestAllowedHosts(t *testing.T) {
	t.Run("empty string keeps no-restriction semantics without a warning", func(t *testing.T) {
		logger := &capturingLogger{}
		hosts := AllowedHosts("", logger)
		if len(hosts) != 0 {
			t.Fatalf("empty input: want 0 hosts, got %v", hosts)
		}
		if len(logger.warns) != 0 || len(logger.debugs) != 0 {
			t.Fatalf("empty input: want no log entries, got warns=%v debugs=%v", logger.warns, logger.debugs)
		}
	})

	t.Run("whitespace-only string warns and yields no restriction", func(t *testing.T) {
		logger := &capturingLogger{}
		hosts := AllowedHosts("   \t\n", logger)
		if len(hosts) != 0 {
			t.Fatalf("whitespace-only input: want 0 hosts, got %v", hosts)
		}
		if len(logger.warns) != 1 || logger.warns[0] != "allowed_hosts_empty_after_parsing" {
			t.Fatalf("whitespace-only input: want exactly one warn 'allowed_hosts_empty_after_parsing', got %v", logger.warns)
		}
		if len(logger.debugs) != 0 {
			t.Fatalf("whitespace-only input: want no debug entries when Warn is available, got %v", logger.debugs)
		}
	})

	t.Run("unicode whitespace U+00A0 tokenizes to zero hosts and warns per #354", func(t *testing.T) {
		// strings.Fields trims unicode.IsSpace, which includes U+00A0
		// (NBSP) — the same whitespace class #354 made nginx/PHP reject
		// at config load. Reaching libgomesi, it must count as
		// whitespace-only: zero tokens + warn, never a bogus host token.
		logger := &capturingLogger{}
		hosts := AllowedHosts("\u00a0", logger)
		if len(hosts) != 0 {
			t.Fatalf("U+00A0 input: want 0 host tokens, got %v", hosts)
		}
		if len(logger.warns) != 1 || logger.warns[0] != "allowed_hosts_empty_after_parsing" {
			t.Fatalf("U+00A0 input: want exactly one warn 'allowed_hosts_empty_after_parsing', got %v", logger.warns)
		}
	})

	t.Run("logger without Warn support reports via Debug instead", func(t *testing.T) {
		logger := &debugOnlyLogger{}
		hosts := AllowedHosts("  ", logger)
		if len(hosts) != 0 {
			t.Fatalf("whitespace-only input: want 0 hosts, got %v", hosts)
		}
		if len(logger.debugs) != 1 || logger.debugs[0] != "allowed_hosts_empty_after_parsing" {
			t.Fatalf("whitespace-only input: want exactly one debug 'allowed_hosts_empty_after_parsing', got %v", logger.debugs)
		}
	})

	t.Run("populated list warns nothing and restricts normally", func(t *testing.T) {
		logger := &capturingLogger{}
		hosts := AllowedHosts("backend.internal cdn.trusted.com", logger)
		if len(hosts) != 2 || hosts[0] != "backend.internal" || hosts[1] != "cdn.trusted.com" {
			t.Fatalf("populated input: want [backend.internal cdn.trusted.com], got %v", hosts)
		}
		if len(logger.warns) != 0 || len(logger.debugs) != 0 {
			t.Fatalf("populated input: want no log entries, got warns=%v debugs=%v", logger.warns, logger.debugs)
		}
	})
}
