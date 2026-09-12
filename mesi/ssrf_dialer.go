// Dial-time SSRF protection using syscall.RawConn.
//
// This file is excluded from Traefik plugin (Yaegi) builds via Dockerfile
// because Yaegi cannot interpret the syscall package — it depends on unsafe,
// which Yaegi does not support. A stub NewSSRFSafeTransport is provided
// in ssrf_yaegi.go for Traefik builds instead.
//
// All other server integrations (Apache, Nginx, Caddy, etc.) use this file
// for full dial-time private IP blocking.
package mesi

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
)

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
