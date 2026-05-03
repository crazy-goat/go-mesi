package mesi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

var (
	ErrInvalidURL         = errors.New("invalid url")
	ErrSSRFBlocked        = errors.New("ssrf blocked")
	ErrUpstreamStatus     = errors.New("upstream bad status")
	ErrTimeBudgetExceeded = errors.New("exceeded time budget")

	_, cgnatCIDR, _         = net.ParseCIDR("100.64.0.0/10")
	_, benchmarkCIDR, _     = net.ParseCIDR("198.18.0.0/15")
	_, reserved240CIDR, _   = net.ParseCIDR("240.0.0.0/4")
	_, documentationCIDR, _ = net.ParseCIDR("2001:db8::/32")
	_, nat64CIDR, _         = net.ParseCIDR("64:ff9b::/96")
)

func IsEsiResponse(response *http.Response) bool {
	header := strings.ToLower(response.Header.Get("Edge-control"))

	for _, part := range strings.Split(header, ",") {
		if strings.TrimSpace(part) == "dca=esi" {
			return true
		}
	}
	return false
}

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func isURLSafe(requestedURL string, config EsiParserConfig) error {
	parsedURL, err := url.Parse(requestedURL)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidURL, err.Error())
	}

	host := parsedURL.Hostname()

	// Relative URLs have no host and no scheme - they will be resolved against DefaultUrl
	if parsedURL.Scheme == "" && host == "" {
		return nil
	}

	// Absolute URLs must have a host
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

// hostInAllowedHosts checks if a hostname matches any entry in AllowedHosts.
// Matches exact hostnames and subdomains (e.g., "api.example.com" matches "example.com").
func hostInAllowedHosts(host string, config EsiParserConfig) bool {
	for _, allowed := range config.AllowedHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// safeDialer returns a net.Dialer with a Control callback that blocks
// connections to private/reserved IP addresses at dial time.
// This prevents SSRF via DNS rebinding attacks (TOCTOU between validation and dial).
func safeDialer(config EsiParserConfig) *net.Dialer {
	return &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				// This should not happen - Control receives resolved IP:port
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

// Deprecated: singleFetchUrl does not support context propagation.
// Use singleFetchUrlWithContext instead for proper cancellation support.
func singleFetchUrl(requestedURL string, config EsiParserConfig) (data string, esiResponse bool, err error) {
	return singleFetchUrlWithContext(requestedURL, config, config.Context)
}

// singleFetchUrlWithContext fetches a URL with context support for proper cancellation.
func singleFetchUrlWithContext(requestedURL string, config EsiParserConfig, ctx context.Context) (data string, esiResponse bool, err error) {
	logger := config.getLogger()
	if ctx == nil {
		ctx = context.Background()
	}

	if semaphore := config.getSemaphore(); semaphore != nil {
		semaphore <- struct{}{}
		defer func() { <-semaphore }()
	}

	if config.Timeout <= 0 {
		logger.Debug("fetch_timeout", "url", requestedURL, "error", "exceeded time budget")
		return "", false, fmt.Errorf("%w", ErrTimeBudgetExceeded)
	}

	parsed, err := url.Parse(requestedURL)
	if err != nil {
		return "", false, fmt.Errorf("%w: %s", ErrInvalidURL, err.Error())
	}

	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false, fmt.Errorf("%w: invalid url scheme: %s", ErrInvalidURL, parsed.Scheme)
	}

	if err := isURLSafe(requestedURL, config); err != nil {
		logger.Debug("fetch_ssrf_error", "url", requestedURL, "error", err.Error())
		return "", false, fmt.Errorf("%w: %s", ErrSSRFBlocked, err.Error())
	}

	var client httpDoer
	if config.HTTPClient != nil {
		client = config.HTTPClient
	} else if config.AllowPrivateIPsForAllowedHosts && hostInAllowedHosts(parsed.Hostname(), config) {
		// Allowed host with private-IP bypass opt-in - use standard client without SSRF protection.
		client = &http.Client{Timeout: config.Timeout}
	} else {
		transport := NewSSRFSafeTransport(config)
		client = &http.Client{
			Timeout:   config.Timeout,
			Transport: transport,
		}
	}
	// Note: When HTTPClient is provided, callers are responsible for setting
	// appropriate timeouts and SSRF protection on the client.
	// Use NewSSRFSafeTransport(config) to create a transport with dial-time
	// private IP blocking.

	var urlToFetch string
	if parsed.Scheme == "" {
		if config.DefaultUrl == "" {
			return "", false, fmt.Errorf("%w: default url can't be empty, on relative urls: %s", ErrInvalidURL, requestedURL)
		}
		urlToFetch = strings.TrimRight(config.DefaultUrl, "/") + "/" + strings.TrimLeft(requestedURL, "/")
	} else {
		urlToFetch = requestedURL
	}

	cacheKey := ""
	if config.Cache != nil {
		cacheKeyFunc := config.CacheKeyFunc
		if cacheKeyFunc == nil {
			cacheKeyFunc = DefaultCacheKey
		}
		cacheKey = cacheKeyFunc(urlToFetch)
		if val, ok, err := config.Cache.Get(ctx, cacheKey); err != nil {
			log.Printf("mesi: cache get error for key %q: %v", cacheKey, err)
		} else if ok {
			return val, false, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlToFetch, nil)
	if err != nil {
		return "", false, fmt.Errorf("%w: %s", ErrInvalidURL, err.Error())
	}
	req.Header.Set("Surrogate-Capability", "ESI/1.0")

	logger.Debug("fetch_start", "url", urlToFetch, "timeout", config.Timeout)
	reqStart := time.Now()
	content, err := client.Do(req)
	if err != nil {
		logger.Debug("fetch_error", "url", urlToFetch, "error", err.Error())
		return "", false, errors.Join(ErrUpstreamStatus, err)
	}
	logger.Debug("fetch_done", "url", urlToFetch, "duration", time.Since(reqStart), "status", content.StatusCode)
	defer func() { _ = content.Body.Close() }()

	var dataBytes []byte
	if config.MaxResponseSize > 0 {
		// Use LimitReader to cap response size
		limitedReader := io.LimitReader(content.Body, config.MaxResponseSize+1)
		dataBytes, err = io.ReadAll(limitedReader)
		if err != nil {
			return "", false, errors.Join(ErrUpstreamStatus, err)
		}
		if int64(len(dataBytes)) > config.MaxResponseSize {
			return "", false, fmt.Errorf("response body exceeds maximum allowed size of %d bytes", config.MaxResponseSize)
		}
	} else {
		// No limit - backward compatibility
		dataBytes, err = io.ReadAll(content.Body)
		if err != nil {
			return "", false, errors.Join(ErrUpstreamStatus, err)
		}
	}

	if content.StatusCode >= 400 {
		return "", false, fmt.Errorf("%w: upstream returned status %d", ErrUpstreamStatus, content.StatusCode)
	}
	contentStr := string(dataBytes)
	if config.Cache != nil && cacheKey != "" {
		if err := config.Cache.Set(ctx, cacheKey, contentStr, config.CacheTTL); err != nil {
			log.Printf("mesi: cache set error for key %q: %v", cacheKey, err)
		}
	}
	return contentStr, IsEsiResponse(content), nil
}
