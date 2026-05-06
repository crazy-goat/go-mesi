package mesi

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
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
			if host == allowedHost || strings.HasSuffix(host, "."+allowedHost) {
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
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func safeDialer(config EsiParserConfig) *net.Dialer {
	return &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("internal error: dial address expected to be IP but got: %s", host)
			}
			if config.BlockPrivateIPs && isPrivateOrReservedIP(ip) {
				return fmt.Errorf("%w: blocked dial to private/reserved ip", ErrSSRFBlocked)
			}
			return nil
		},
	}
}

// NewSSRFSafeTransport creates an http.Transport that blocks connections to
// private/reserved IP addresses at dial time, preventing SSRF via DNS rebinding.
// When config.BlockPrivateIPs is false, it returns a standard transport.
//
// Users supplying a custom HTTPClient should use this transport to ensure
// SSRF protection works correctly:
//
//	client := &http.Client{
//		Transport: mesi.NewSSRFSafeTransport(config),
//		Timeout:   config.Timeout,
//	}
func NewSSRFSafeTransport(config EsiParserConfig) *http.Transport {
	dialer := safeDialer(config)
	return &http.Transport{
		DialContext: dialer.DialContext,
	}
}
