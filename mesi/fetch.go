package mesi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrUpstreamStatus     = errors.New("upstream bad status")
	ErrTimeBudgetExceeded = errors.New("exceeded time budget")
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
		return "", false, err
	}

	var client httpDoer
	if config.HTTPClient != nil {
		// When HTTPClient is provided, callers are responsible for setting
		// appropriate timeouts and SSRF protection on the client.
		// Use NewSSRFSafeTransport(config) to create a transport with
		// dial-time private IP blocking.
		client = config.HTTPClient
	} else if config.AllowPrivateIPsForAllowedHosts && hostInAllowedHosts(parsed.Hostname(), config) {
		// Allowed host with private-IP bypass opt-in - use standard client
		// without SSRF protection.
		client = &http.Client{Timeout: config.Timeout}
	} else {
		transport := NewSSRFSafeTransport(config)
		client = &http.Client{
			Timeout:   config.Timeout,
			Transport: transport,
		}
	}

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
			config.warn("cache_get_error", "key", cacheKey, "error", err.Error())
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
		// Use LimitReader to cap response size.
		limitedReader := io.LimitReader(content.Body, config.MaxResponseSize+1)
		dataBytes, err = io.ReadAll(limitedReader)
		if err != nil {
			return "", false, errors.Join(ErrUpstreamStatus, err)
		}
		if int64(len(dataBytes)) > config.MaxResponseSize {
			return "", false, fmt.Errorf("response body exceeds maximum allowed size of %d bytes", config.MaxResponseSize)
		}
	} else {
		// No limit - backward compatibility.
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
			config.warn("cache_set_error", "key", cacheKey, "error", err.Error())
		}
	}
	return contentStr, IsEsiResponse(content), nil
}
