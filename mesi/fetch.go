package mesi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrUpstreamStatus     = errors.New("upstream bad status")
	ErrTimeBudgetExceeded = errors.New("exceeded time budget")

	inflightMu    sync.Mutex
	inflightLocks = map[string]chan struct{}{}
)

// getInflightLock returns a per-key buffered channel (capacity 1) used to
// serialise concurrent fetches targeting the same cache key. When two ESI
// include workers both miss the cache for an identical URL, only one worker
// is allowed to perform the upstream fetch; the other blocks until the first
// worker has populated the cache and then serves from it. Acquiring the lock
// is a send, releasing it a receive.
func getInflightLock(cacheKey string) chan struct{} {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	if ch, ok := inflightLocks[cacheKey]; ok {
		return ch
	}
	ch := make(chan struct{}, 1)
	inflightLocks[cacheKey] = ch
	return ch
}

func IsEsiResponse(response *http.Response) bool {
	header := strings.ToLower(response.Header.Get("Edge-control"))

	for _, part := range strings.Split(header, ",") {
		if strings.TrimSpace(part) == "dca=esi" {
			return true
		}
	}
	return false
}

// noRedirectFunc prevents automatic redirect following; redirects are
// handled manually in singleFetchUrlWithContext so every hop target is
// re-validated against the AllowedHosts whitelist before it is dialed.
func noRedirectFunc(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// isRedirectStatus reports whether status is one of the redirect statuses
// Go's http.Client follows automatically (301, 302, 303, 307, 308).
func isRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// validateFetchURL applies the same scheme and AllowedHosts checks that the
// original include URL receives to an absolute fetch URL. It is used for the
// DefaultUrl-expanded relative include URL and for every redirect hop target,
// so no URL is ever dialed without passing the whitelist.
func validateFetchURL(rawURL string, config EsiParserConfig) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidURL, err.Error())
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: invalid url scheme: %s", ErrInvalidURL, parsed.Scheme)
	}
	if err := isURLSafe(rawURL, config); err != nil {
		return nil, err
	}
	return parsed, nil
}

// fetchClientForURL picks the HTTP client for the URL about to be fetched:
// the caller-provided shared client (wrapped, never mutated, so its transport
// is reused but redirects stay under our control), a plain client for
// allowlisted hosts with the private-IP bypass opt-in, or the SSRF-safe
// transport client. All clients disable automatic redirect following.
func fetchClientForURL(fetchURL string, config EsiParserConfig) httpDoer {
	if config.HTTPClient != nil {
		return &http.Client{
			Transport:     config.HTTPClient.Transport,
			Timeout:       config.HTTPClient.Timeout,
			Jar:           config.HTTPClient.Jar,
			CheckRedirect: noRedirectFunc,
		}
	}
	parsed, _ := url.Parse(fetchURL)
	if config.AllowPrivateIPsForAllowedHosts && parsed != nil && hostInAllowedHosts(parsed.Hostname(), config) {
		// Allowed host with private-IP bypass opt-in - use standard client
		// without SSRF protection.
		return &http.Client{Timeout: config.Timeout, CheckRedirect: noRedirectFunc}
	}
	return &http.Client{
		Timeout:       config.Timeout,
		Transport:     NewSSRFSafeTransport(config),
		CheckRedirect: noRedirectFunc,
	}
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

	if config.Timeout <= 0 {
		logger.Debug("fetch_timeout", "url", requestedURL, "error", "exceeded time budget")
		return "", false, fmt.Errorf("%w", ErrTimeBudgetExceeded)
	}

	// One deadline for the whole fetch: every redirect hop request and the
	// final response-body read share the same budget, so a chain of redirects
	// cannot restart the timeout per hop (previously each hop got a fresh
	// client.Timeout, allowing roughly 11x the configured budget). The
	// deadline is established before the admission-control waits below, so
	// the semaphore acquisition and the same-key dedup wait are also bounded
	// by the budget: a queued include can never wait longer than the timeout
	// behind a slow earlier fetch. The per-hop client Timeout below remains
	// as a secondary per-hop bound.
	fetchCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	if semaphore := config.getSemaphore(); semaphore != nil {
		select {
		case semaphore <- struct{}{}:
		case <-fetchCtx.Done():
			return "", false, errors.Join(ErrTimeBudgetExceeded, fetchCtx.Err())
		}
		defer func() { <-semaphore }()
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

	var urlToFetch string
	if parsed.Scheme == "" {
		if config.DefaultUrl == "" {
			return "", false, fmt.Errorf("%w: default url can't be empty, on relative urls: %s", ErrInvalidURL, requestedURL)
		}
		urlToFetch = strings.TrimRight(config.DefaultUrl, "/") + "/" + strings.TrimLeft(requestedURL, "/")
	} else {
		urlToFetch = requestedURL
	}

	// The DefaultUrl-expanded relative include is subject to the same scheme
	// and AllowedHosts checks as the original URL: a relative include must
	// never bypass the whitelist by resolving against a DefaultUrl whose host
	// is not allowlisted.
	if _, err := validateFetchURL(urlToFetch, config); err != nil {
		logger.Debug("fetch_ssrf_error", "url", urlToFetch, "error", err.Error())
		return "", false, err
	}

	cacheKey := ""
	if config.Cache != nil {
		cacheKeyFunc := config.CacheKeyFunc
		if cacheKeyFunc == nil {
			cacheKeyFunc = DefaultCacheKey
		}
		cacheKey = cacheKeyFunc(urlToFetch)
		// Namespace the key by the SSRF policy in effect for this fetch:
		// caches are process-wide/shared, so without this a body fetched
		// under a broader policy could be served to a stricter one.
		cacheKey += securityPolicyFingerprint(config)
		if val, ok, err := config.Cache.Get(fetchCtx, cacheKey); err != nil {
			config.warn("cache_get_error", "key", cacheKey, "error", err.Error())
		} else if ok {
			return val, false, nil
		}
	}

	// Serialise concurrent fetches for the same cache key. Without this
	// guard two workers processing identical <esi:include> URLs both
	// miss the cache, both perform an upstream round-trip, and return
	// different responses — breaking the same-page dedup guarantee that
	// memory-backed and external caches are expected to provide.
	if cacheKey != "" {
		lock := getInflightLock(cacheKey)
		select {
		case lock <- struct{}{}:
			// Slot acquired: this goroutine performs the fetch.
		case <-fetchCtx.Done():
			return "", false, errors.Join(ErrTimeBudgetExceeded, fetchCtx.Err())
		}
		if fetchCtx.Err() != nil {
			<-lock
			return "", false, errors.Join(ErrTimeBudgetExceeded, fetchCtx.Err())
		}
		// Double-check: another goroutine may have populated the
		// cache while we were waiting for the slot.
		if val, ok, _ := config.Cache.Get(fetchCtx, cacheKey); ok {
			<-lock
			return val, false, nil
		}
		defer func() { <-lock }()
	}

	// Redirects are followed manually: every client below is built with
	// CheckRedirect returning http.ErrUseLastResponse so Go never auto-follows.
	// Each hop target is resolved and re-validated (scheme + AllowedHosts)
	// before it is dialed, so an allowlisted backend can no longer redirect to
	// an arbitrary host.
	const maxRedirects = 10

	hopURL := urlToFetch
	reqStart := time.Now()
	var content *http.Response
	for hop := 0; ; hop++ {
		req, err := http.NewRequestWithContext(fetchCtx, "GET", hopURL, nil)
		if err != nil {
			return "", false, fmt.Errorf("%w: %s", ErrInvalidURL, err.Error())
		}
		req.Header.Set("Surrogate-Capability", "ESI/1.0")

		logger.Debug("fetch_start", "url", hopURL, "timeout", config.Timeout)
		content, err = fetchClientForURL(hopURL, config).Do(req)
		if err != nil {
			logger.Debug("fetch_error", "url", hopURL, "error", err.Error())
			return "", false, errors.Join(ErrUpstreamStatus, err)
		}
		logger.Debug("fetch_done", "url", hopURL, "duration", time.Since(reqStart), "status", content.StatusCode)

		if !isRedirectStatus(content.StatusCode) {
			break
		}
		location := content.Header.Get("Location")
		if location == "" {
			// A 3xx without Location is returned as the final response.
			break
		}
		if hop >= maxRedirects {
			_ = content.Body.Close()
			return "", false, errors.Join(ErrUpstreamStatus, fmt.Errorf("stopped after %d redirects", maxRedirects))
		}
		ref, err := url.Parse(location)
		if err != nil {
			_ = content.Body.Close()
			return "", false, errors.Join(ErrUpstreamStatus, fmt.Errorf("invalid redirect location: %s", location))
		}
		nextURL := req.URL.ResolveReference(ref)
		if _, err := validateFetchURL(nextURL.String(), config); err != nil {
			_ = content.Body.Close()
			logger.Debug("fetch_ssrf_error", "url", nextURL.String(), "error", err.Error())
			return "", false, err
		}
		_ = content.Body.Close()
		hopURL = nextURL.String()
	}
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
		if err := config.Cache.Set(fetchCtx, cacheKey, contentStr, config.CacheTTL); err != nil {
			config.warn("cache_set_error", "key", cacheKey, "error", err.Error())
		}
	}
	return contentStr, IsEsiResponse(content), nil
}
