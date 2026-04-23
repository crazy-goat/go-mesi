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
	"strconv"
	"strings"
	"syscall"
	"time"
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
		return errors.New("invalid url: " + err.Error())
	}

	host := parsedURL.Host

	// Relative URLs have no host and no scheme - they will be resolved against DefaultUrl
	if parsedURL.Scheme == "" && host == "" {
		return nil
	}

	// Absolute URLs must have a host
	if host == "" {
		return errors.New("url has no host")
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
			return errors.New("host not in allowed list: " + host)
		}
	}

	return nil
}

func isPrivateOrReservedIP(ip net.IP) bool {
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"0.0.0.0/8",
		"224.0.0.0/4",
		"240.0.0.0/4",
	}

	for _, block := range privateBlocks {
		_, cidr, _ := net.ParseCIDR(block)
		if cidr.Contains(ip) {
			return true
		}
	}

	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
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
				return fmt.Errorf("dial address is not an IP: %s", host)
			}
			if config.BlockPrivateIPs && isPrivateOrReservedIP(ip) {
				return fmt.Errorf("blocked dial to private/reserved ip: %s", ip)
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
		return "", false, errors.New("exceeded time budget")
	}

	parsed, err := url.Parse(requestedURL)
	if err != nil {
		return "", false, errors.New("invalid url: " + err.Error())
	}

	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false, errors.New("invalid url scheme: " + parsed.Scheme)
	}

	if err := isURLSafe(requestedURL, config); err != nil {
		logger.Debug("fetch_ssrf_error", "url", requestedURL, "error", err.Error())
		return "", false, errors.New("ssrf validation failed: " + err.Error())
	}

	var client httpDoer
	if config.HTTPClient != nil {
		client = config.HTTPClient
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
			return "", false, errors.New("default url can't be empty, on relative urls: " + requestedURL)
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
		return "", false, errors.New("failed to create request: " + err.Error())
	}
	req.Header.Set("Surrogate-Capability", "ESI/1.0")

	logger.Debug("fetch_start", "url", urlToFetch, "timeout", config.Timeout)
	reqStart := time.Now()
	content, err := client.Do(req)
	if err != nil {
		logger.Debug("fetch_error", "url", urlToFetch, "error", err.Error())
		return "", false, err
	}
	logger.Debug("fetch_done", "url", urlToFetch, "duration", time.Since(reqStart), "status", content.StatusCode)
	defer func() { _ = content.Body.Close() }()

	var dataBytes []byte
	if config.MaxResponseSize > 0 {
		// Use LimitReader to cap response size
		limitedReader := io.LimitReader(content.Body, config.MaxResponseSize+1)
		dataBytes, err = io.ReadAll(limitedReader)
		if err != nil {
			return "", false, err
		}
		if int64(len(dataBytes)) > config.MaxResponseSize {
			return "", false, fmt.Errorf("response body exceeds maximum allowed size of %d bytes", config.MaxResponseSize)
		}
	} else {
		// No limit - backward compatibility
		dataBytes, err = io.ReadAll(content.Body)
		if err != nil {
			return "", false, err
		}
	}

	if content.StatusCode >= 400 {
		return "", false, errors.New("upstream returned status " + strconv.Itoa(content.StatusCode))
	}
	contentStr := string(dataBytes)
	if config.Cache != nil && cacheKey != "" {
		if err := config.Cache.Set(ctx, cacheKey, contentStr, config.CacheTTL); err != nil {
			log.Printf("mesi: cache set error for key %q: %v", cacheKey, err)
		}
	}
	return contentStr, IsEsiResponse(content), nil
}
