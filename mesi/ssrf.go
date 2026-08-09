// SSRF protection for ESI includes.
//
// This file contains common SSRF validation functions shared across all
// server integrations. Dial-time IP blocking (using syscall.RawConn) is
// in ssrf_dialer.go, which is excluded from Traefik plugin builds because
// Yaegi cannot interpret the syscall package (it depends on unsafe).
//
// See: https://github.com/traefik/yaegi/issues/1636
package mesi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

var (
	ErrInvalidURL  = errors.New("invalid url")
	ErrSSRFBlocked = errors.New("ssrf blocked")

	_, cgnatCIDR, _         = net.ParseCIDR("100.64.0.0/10")
	_, benchmarkCIDR, _     = net.ParseCIDR("198.18.0.0/15")
	_, reserved240CIDR, _   = net.ParseCIDR("240.0.0.0/4")
	_, documentationCIDR, _ = net.ParseCIDR("2001:db8::/32")
	_, nat64CIDR, _         = net.ParseCIDR("64:ff9b::/96")
)

func isURLSafe(requestedURL string, config EsiParserConfig) error {
	parsedURL, err := url.Parse(requestedURL)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidURL, err.Error())
	}

	host := parsedURL.Hostname()

	if parsedURL.Scheme == "" && host == "" {
		return nil
	}

	if host == "" {
		return fmt.Errorf("%w: url has no host", ErrInvalidURL)
	}

	if len(config.AllowedHosts) > 0 {
		allowed := false
		for _, allowedHost := range config.AllowedHosts {
			if hostMatches(host, allowedHost) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: host not in allowed list: %s", ErrSSRFBlocked, host)
		}
	}

	return nil
}

// hostMatches reports whether host equals allowedHost or is a subdomain of
// it. Matching is case-insensitive (EqualFold), tolerates a single trailing
// root dot on either side, and keeps the exact '.' suffix boundary so suffix
// injection (attacker-example.com vs example.com) never matches.
func hostMatches(host, allowedHost string) bool {
	host = strings.TrimSuffix(host, ".")
	allowedHost = strings.TrimSuffix(allowedHost, ".")
	return strings.EqualFold(host, allowedHost) ||
		(len(host) > len(allowedHost) && host[len(host)-len(allowedHost)-1] == '.' &&
			strings.EqualFold(host[len(host)-len(allowedHost):], allowedHost))
}

// securityPolicyFingerprint returns a canonical fingerprint of the SSRF
// policy in effect for a fetch: the sorted AllowedHosts list, the
// BlockPrivateIPs and AllowPrivateIPsForAllowedHosts flags, and the HTTP
// client type (custom caller-supplied client vs default per-request client).
// It is appended to every cache key (after the URL-derived key part) so
// content cached under one policy can never be served under a different one
// — a policy change in either direction (stricter or looser) invalidates
// previously cached entries, and entries fetched through a custom transport
// are never served to a config using the default client. The host list is
// JSON-encoded rather than joined with a delimiter so entries containing
// delimiter characters (e.g. "a,b") cannot collide with separate entries;
// duplicates are collapsed so the fingerprint is canonical.
func securityPolicyFingerprint(config EsiParserConfig) string {
	hosts := make([]string, 0, len(config.AllowedHosts))
	seen := make(map[string]struct{}, len(config.AllowedHosts))
	for _, h := range config.AllowedHosts {
		h = strings.ToLower(strings.TrimSuffix(h, "."))
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	hostsJSON, _ := json.Marshal(hosts)
	return fmt.Sprintf("|ssrf=ah:%s,bpi:%t,api4ah:%t,hc:%t",
		string(hostsJSON), config.BlockPrivateIPs, config.AllowPrivateIPsForAllowedHosts,
		config.HTTPClient != nil)
}

func isPrivateOrReservedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	v4 := ip.To4()
	if v4 != nil {
		ip = v4
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}

	if v4 != nil {
		if cgnatCIDR.Contains(v4) || benchmarkCIDR.Contains(v4) || reserved240CIDR.Contains(v4) {
			return true
		}
	} else {
		if documentationCIDR.Contains(ip) || nat64CIDR.Contains(ip) {
			return true
		}
	}

	return false
}

func hostInAllowedHosts(host string, config EsiParserConfig) bool {
	for _, allowed := range config.AllowedHosts {
		if hostMatches(host, allowed) {
			return true
		}
	}
	return false
}
