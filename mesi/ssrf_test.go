package mesi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsURLSafe_DoesNotBlockPrivateIPs(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"localhost", "http://localhost/test"},
		{"127.0.0.1", "http://127.0.0.1/test"},
		{"10.0.0.1", "http://10.0.0.1/test"},
		{"public IP", "http://8.8.8.8/test"},
	}

	config := EsiParserConfig{
		BlockPrivateIPs: true,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := isURLSafe(tt.url, config)
			if err != nil {
				t.Errorf("isURLSafe should not check private IPs, got error: %v", err)
			}
		})
	}
}

func TestIsURLSafe_AllowedHosts(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		allowedHosts []string
		wantErr      bool
	}{
		{"allowed exact", "http://example.com/test", []string{"example.com"}, false},
		{"allowed subdomain", "http://api.example.com/test", []string{"example.com"}, false},
		{"not allowed", "http://other.com/test", []string{"example.com"}, true},
		{"multiple allowed", "http://foo.com/test", []string{"example.com", "foo.com"}, false},
		{"empty allowed list", "http://example.com/test", []string{}, false},
		{"allowed host with port", "http://example.com:8080/test", []string{"example.com"}, false},
		{"allowed subdomain with port", "http://api.example.com:443/test", []string{"example.com"}, false},
		{"not allowed with port", "http://other.com:8080/test", []string{"example.com"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{
				BlockPrivateIPs: true,
				AllowedHosts:    tt.allowedHosts,
			}
			err := isURLSafe(tt.url, config)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestIsURLSafe_IgnoresBlockPrivateIPsFlag(t *testing.T) {
	config := EsiParserConfig{
		BlockPrivateIPs: false,
	}

	err := isURLSafe("http://127.0.0.1/test", config)
	if err != nil {
		t.Errorf("expected no error since isURLSafe does not check BlockPrivateIPs, got: %v", err)
	}
}

func TestIsURLSafe_InvalidURL(t *testing.T) {
	config := EsiParserConfig{
		BlockPrivateIPs: true,
	}

	tests := []struct {
		name string
		url  string
	}{
		{"invalid url", "://invalid"},
		{"no host", "http:///path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := isURLSafe(tt.url, config)
			if err == nil {
				t.Error("expected error for invalid URL")
			}
		})
	}
}

func TestSingleFetchUrlSSRFValidation(t *testing.T) {
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: true,
		Logger:          DiscardLogger{},
	}

	_, _, err := singleFetchUrl("http://127.0.0.1/test", config)
	if err == nil {
		t.Error("expected SSRF error for private IP")
	}
	if !strings.Contains(err.Error(), "blocked dial to private/reserved ip") {
		t.Errorf("expected dial-time SSRF error, got: %v", err)
	}
}

func TestIsPrivateOrReservedIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"loopback", "127.0.0.1", true},
		{"10.0.0.0/8", "10.0.0.1", true},
		{"10.255.255.255", "10.255.255.255", true},
		{"172.16.0.0/12", "172.16.0.1", true},
		{"172.31.255.255", "172.31.255.255", true},
		{"192.168.0.0/16", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"unspecified", "0.0.0.0", true},
		{"multicast", "224.0.0.1", true},
		{"reserved", "240.0.0.1", true},
		{"public", "8.8.8.8", false},
		{"public 2", "1.1.1.1", false},
		{"ipv6 loopback", "::1", true},
		{"ipv6 ULA fd00", "fd00::1", true},
		{"ipv6 ULA fc00", "fc00::1", true},
		{"ipv6 link-local", "fe80::1", true},
		{"ipv6 unspecified", "::", true},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"ipv4-mapped private", "::ffff:10.0.0.1", true},
		{"ipv6 documentation", "2001:db8::1", true},
		{"ipv6 multicast", "ff02::1", true},
		{"nat64", "64:ff9b::8.8.8.8", true},
		{"cgnat", "100.64.0.1", true},
		{"benchmark", "198.18.0.1", true},
		{"public ipv6", "2606:4700:4700::1111", false},
		{"public ipv6 google", "2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %s", tt.ip)
			}
			result := isPrivateOrReservedIP(ip)
			if result != tt.expected {
				t.Errorf("isPrivateOrReservedIP(%s) = %v, expected %v", tt.ip, result, tt.expected)
			}
		})
	}
}

func TestSSRFDialBlocksPrivateIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	config := EsiParserConfig{
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        1,
		Timeout:         2 * time.Second,
		BlockPrivateIPs: true,
		Logger:          DiscardLogger{},
	}

	_, _, err := singleFetchUrlWithContext(server.URL, config, context.Background())
	if err == nil {
		t.Fatal("expected error when fetching from private IP with BlockPrivateIPs=true, got nil")
	}
	if !contains(err.Error(), "blocked dial to private/reserved ip") {
		t.Errorf("expected 'blocked dial' error, got: %v", err)
	}
}

func TestSSRFDialAllowsPublicIP(t *testing.T) {
	config := EsiParserConfig{
		BlockPrivateIPs: true,
	}

	dialer := safeDialer(config)

	err := dialer.Control("tcp", "8.8.8.8:80", nil)
	if err != nil {
		t.Errorf("expected public IP 8.8.8.8 to be allowed, got error: %v", err)
	}

	err = dialer.Control("tcp", "1.1.1.1:443", nil)
	if err != nil {
		t.Errorf("expected public IP 1.1.1.1 to be allowed, got error: %v", err)
	}

	err = dialer.Control("tcp", "127.0.0.1:80", nil)
	if err == nil {
		t.Error("expected private IP 127.0.0.1 to be blocked")
	}

	err = dialer.Control("tcp", "10.0.0.1:80", nil)
	if err == nil {
		t.Error("expected private IP 10.0.0.1 to be blocked")
	}
}

func TestSSRFDialerWithBlockPrivateIPsDisabled(t *testing.T) {
	config := EsiParserConfig{
		BlockPrivateIPs: false,
	}

	dialer := safeDialer(config)

	err := dialer.Control("tcp", "127.0.0.1:80", nil)
	if err != nil {
		t.Errorf("expected private IP to be allowed when BlockPrivateIPs=false, got: %v", err)
	}

	err = dialer.Control("tcp", "10.0.0.1:80", nil)
	if err != nil {
		t.Errorf("expected private IP to be allowed when BlockPrivateIPs=false, got: %v", err)
	}

	err = dialer.Control("tcp", "8.8.8.8:80", nil)
	if err != nil {
		t.Errorf("expected public IP to be allowed when BlockPrivateIPs=false, got: %v", err)
	}
}

func TestNewSSRFSafeTransport(t *testing.T) {
	config := EsiParserConfig{
		BlockPrivateIPs: true,
	}

	transport := NewSSRFSafeTransport(config)
	if transport == nil {
		t.Fatal("NewSSRFSafeTransport returned nil")
	}

	if transport.DialContext == nil {
		t.Fatal("transport.DialContext is nil")
	}

	config2 := EsiParserConfig{
		BlockPrivateIPs: false,
	}
	transport2 := NewSSRFSafeTransport(config2)
	if transport2 == nil {
		t.Fatal("NewSSRFSafeTransport returned nil for BlockPrivateIPs=false")
	}
}

func TestSSRFBlocksIPv6Loopback(t *testing.T) {
	log := &recordingLogger{}
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: true,
		Logger:          log,
	}

	html := `<html><body><esi:include src="http://[::1]/test"/></body></html>`
	result := MESIParse(html, config)

	if strings.Contains(result, "::1") {
		t.Errorf("output leaked internal IP: %q", result)
	}
	if !log.containsMsg("include_failed") {
		t.Errorf("expected include_failed log for IPv6 loopback block, got: %q", result)
	}
}

func TestSSRFBlocksIPv6ULA(t *testing.T) {
	log := &recordingLogger{}
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: true,
		Logger:          log,
	}

	html := `<html><body><esi:include src="http://[fd00::1]/test"/></body></html>`
	result := MESIParse(html, config)

	if strings.Contains(result, "fd00") {
		t.Errorf("output leaked internal IP: %q", result)
	}
	if !log.containsMsg("include_failed") {
		t.Errorf("expected include_failed log for IPv6 ULA block, got: %q", result)
	}
}

func TestSSRFBlocksIPv4MappedIPv6(t *testing.T) {
	log := &recordingLogger{}
	config := EsiParserConfig{
		DefaultUrl:      "http://example.com/",
		MaxDepth:        1,
		Timeout:         1 * time.Second,
		BlockPrivateIPs: true,
		Logger:          log,
	}

	html := `<html><body><esi:include src="http://[::ffff:127.0.0.1]/test"/></body></html>`
	result := MESIParse(html, config)

	if strings.Contains(result, "127.0.0.1") {
		t.Errorf("output leaked internal IP: %q", result)
	}
	if !log.containsMsg("include_failed") {
		t.Errorf("expected include_failed log for IPv4-mapped IPv6 block, got: %q", result)
	}
}
