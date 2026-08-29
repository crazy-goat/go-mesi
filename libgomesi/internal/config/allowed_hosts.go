// Package config holds libgomesi configuration helpers that are
// testable with plain Go unit tests (the cgo package main of
// libgomesi cannot be linked into a test binary).
package config

import (
	"strings"

	"github.com/crazy-goat/go-mesi/mesi"
)

// AllowedHosts tokenizes the whitespace-separated allowedHosts string
// into the parser's host whitelist. An empty string keeps the
// documented "no restriction" semantics. A non-empty string that
// tokenizes to zero hosts (whitespace-only input) can only come from a
// misconfiguration — several callers (nginx, the PHP extension, Apache
// after #358) reject it at their own config layer, but libgomesi itself
// must never fail open silently (#357): the condition is reported
// through the logger at Warn severity when the logger implements
// mesi.LoggerWarn, and through Debug otherwise (the same contract as
// mesi.EsiParserConfig.warn).
func AllowedHosts(hostsStr string, logger mesi.Logger) []string {
	hosts := strings.Fields(hostsStr)
	if hostsStr != "" && len(hosts) == 0 {
		msg := "allowed_hosts_empty_after_parsing"
		if w, ok := logger.(mesi.LoggerWarn); ok {
			w.Warn(msg, "value", hostsStr)
		} else {
			logger.Debug(msg, "value", hostsStr)
		}
	}
	return hosts
}
