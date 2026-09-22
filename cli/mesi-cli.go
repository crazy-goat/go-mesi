package main

import (
	"flag"
	"fmt"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/mesi/cache_memcached"
	"github.com/crazy-goat/go-mesi/mesi/cache_redis"
	"github.com/redis/go-redis/v9"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func isURL(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

// allowedHostsFromFlag converts the -allowed-hosts flag value into the
// config allowlist. An empty value keeps the default nil allowlist (all
// hosts allowed, subject to BlockPrivateIPs) — backward compatible with
// the pre-flag behaviour. Entries are passed verbatim to the shared-core
// matcher (exact or subdomain-suffix match with a '.' boundary,
// case-insensitive, ports ignored), so entries are matched as written.
func allowedHostsFromFlag(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

// maxMaxResponseSize mirrors libgomesi's config.MaxMaxResponseSize
// (libgomesi/internal/config/max_response_size.go, #169). That package
// lives under libgomesi/internal and cannot be imported from the separate
// cli module, so the bound is mirrored here (the same keep-in-sync pattern
// as Apache's MESI_MAX_MAX_RESPONSE_SIZE). It is math.MaxInt64-1: the core
// computes MaxResponseSize+1 for its io.LimitReader bound
// (mesi/fetch.go:290), and at math.MaxInt64 that wraps negative, the
// LimitedReader reports EOF immediately, and the include would silently
// render an EMPTY body instead of failing the size check (#448).
const maxMaxResponseSize int64 = math.MaxInt64 - 1

// validateMaxResponseSize rejects -max-response-size values outside
// [0, maxMaxResponseSize]. 0 is the documented "unlimited" value (the core
// only limits when MaxResponseSize > 0, mesi/fetch.go:288); negatives are
// rejected instead of silently passing as 0 through the core's > 0 check.
func validateMaxResponseSize(v int64) error {
	if v < 0 || v > maxMaxResponseSize {
		return fmt.Errorf("max-response-size must be in [0, %d], got %d", maxMaxResponseSize, v)
	}
	return nil
}

func main() {
	defaultUrl := flag.String("default-url", "http://127.0.0.1/", "Default URL to parse")
	maxDepth := flag.Uint("max-depth", 5, "Maximum depth of parsing")
	timeout := flag.Float64("timeout", 10.0, "Request timeout duration in seconds")
	parseOnHeader := flag.Bool("parse-on-header", false, "Enable parsing on header")
	debug := flag.Bool("debug", false, "Enable debug logging")
	cacheBackend := flag.String("cache-backend", "",
		"Cache backend for ESI includes: memory, redis, memcached (default: off)")
	cacheSize := flag.Int("cache-size", 10000,
		"Max cache entries for memory backend")
	cacheTTL := flag.Duration("cache-ttl", 0,
		"Cache TTL (e.g. 30s, 5m); 0 = no expiry")
	cacheRedisAddr := flag.String("cache-redis-addr", "localhost:6379",
		"Redis server address (host:port)")
	cacheRedisPassword := flag.String("cache-redis-password", "",
		"Redis password")
	cacheRedisDB := flag.Int("cache-redis-db", 0,
		"Redis database number")
	cacheMemcachedServers := flag.String("cache-memcached-servers", "",
		"Comma-separated Memcached servers (host:port)")
	cacheKeyTemplate := flag.String("cache-key-template", "",
		"Custom cache key template with placeholders: ${url}, ${header:Name}, ${cookie:Name}")
	allowPrivateIPs := flag.Bool("allow-private-ips", false,
		"Allow ESI includes to private/reserved IP ranges (for local testing)")
	allowedHosts := flag.String("allowed-hosts", "", "Comma-separated list of allowed hosts for ESI includes")
	allowPrivateIPsForAllowedHosts := flag.Bool("allowPrivateIPsForAllowedHosts", false,
		"Allow ESI includes to private/reserved IP ranges for hosts listed in -allowed-hosts (trusts DNS)")
	maxWorkers := flag.Int("max-workers", 0,
		"Max concurrent ESI include goroutines (0 = NumCPU*4)")
	// Default derived from CreateDefaultConfig() so that an absent flag is
	// byte-identical to the pre-flag CLI behaviour (which never overrode the
	// field and therefore always used the 10 MB constructor default).
	maxResponseSize := flag.Int64("max-response-size",
		mesi.CreateDefaultConfig().MaxResponseSize,
		"Max ESI include response size in bytes (0 = unlimited)")
	sharedHTTPClient := flag.Bool("shared-http-client", false,
		"Share HTTP client across ESI includes for connection pooling")
	includeErrorMarker := flag.String("include-error-marker", "",
		"Marker string rendered for failed ESI includes (e.g. '<!-- esi error -->')")

	flag.Parse()
	if uint64(*maxDepth) > mesi.MaxMaxDepth {
		fmt.Fprintf(os.Stderr, "Error: max-depth must be in [0, %d], got %d\n", mesi.MaxMaxDepth, *maxDepth)
		os.Exit(1)
	}
	if err := validateMaxResponseSize(*maxResponseSize); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Error: Missing file|url path argument.")
		fmt.Println("Usage: go run main.go [flags] <file_path|url>")
		flag.PrintDefaults()
		return
	}

	config := mesi.CreateDefaultConfig()
	config.DefaultUrl = *defaultUrl
	config.MaxDepth = *maxDepth
	config.Timeout = time.Duration(*timeout * float64(time.Second))
	config.ParseOnHeader = *parseOnHeader
	config.Debug = *debug
	config.BlockPrivateIPs = !*allowPrivateIPs
	config.AllowedHosts = allowedHostsFromFlag(*allowedHosts)
	config.AllowPrivateIPsForAllowedHosts = *allowPrivateIPsForAllowedHosts
	config.MaxWorkers = *maxWorkers
	// Explicitly assigned even though CreateDefaultConfig() already set the
	// same value: the flag default IS that value, so an absent flag stays
	// byte-identical, while an explicit 0 means "unlimited" (#169 contract).
	config.MaxResponseSize = *maxResponseSize
	config.IncludeErrorMarker = *includeErrorMarker

	if *sharedHTTPClient {
		config.HTTPClient = &http.Client{
			Transport: mesi.NewSSRFSafeTransport(config),
			Timeout:   config.Timeout,
		}
	}

	switch *cacheBackend {
	case "":
	case "memory":
		config.Cache = mesi.NewMemoryCache(*cacheSize, *cacheTTL)
		config.CacheTTL = *cacheTTL
	case "redis":
		rdb := redis.NewClient(&redis.Options{
			Addr:     *cacheRedisAddr,
			Password: *cacheRedisPassword,
			DB:       *cacheRedisDB,
		})
		defer func() { _ = rdb.Close() }()
		config.Cache = cache_redis.NewRedisCache(rdb, *cacheTTL)
		config.CacheTTL = *cacheTTL
	case "memcached":
		servers := strings.Split(*cacheMemcachedServers, ",")
		if *cacheMemcachedServers == "" || len(servers) == 0 || servers[0] == "" {
			log.Fatal("cache-memcached-servers required for memcached backend")
		}
		mc := memcache.New(servers...)
		config.Cache = cache_memcached.NewMemcachedCache(mc, *cacheTTL)
		config.CacheTTL = *cacheTTL
	default:
		log.Fatalf("unknown cache backend: %s", *cacheBackend)
	}

	if *cacheKeyTemplate != "" {
		tmpl := *cacheKeyTemplate
		config.CacheKeyFunc = func(url string) string {
			// CLI mode: only ${url} is supported since there is no HTTP request context.
			// For full header/cookie support, use the Caddy or nginx integration.
			return strings.ReplaceAll(tmpl, "${url}", url)
		}
	}

	pathOrUrl := args[0]
	var data string
	if isURL(pathOrUrl) {
		parsedURL, err := url.Parse(pathOrUrl)
		if err != nil {
			fmt.Println("Error parsing URL:", err)
			return
		}

		config.DefaultUrl = parsedURL.String()

		client := http.Client{
			Timeout: config.Timeout,
		}
		req, err := http.NewRequest("GET", pathOrUrl, nil)
		if err != nil {
			fmt.Println("Error creating request:", err)
			os.Exit(1)
		}
		req.Header.Set("Surrogate-Capability", "ESI/1.0")

		content, err := client.Do(req)
		if err != nil {
			fmt.Println("Error fetching url:", err)
			return
		}

		if !mesi.IsEsiResponse(content) && config.ParseOnHeader {
			fmt.Println("Error response missing Edge-control header:")
			return
		}

		body, err := io.ReadAll(content.Body)
		if err != nil {
			fmt.Println("Error reading response:", err)
			return
		}

		if content.StatusCode >= 400 {
			fmt.Println("Invalid status code:", content.StatusCode)
			return
		}

		data = string(body)
	} else {
		fileContent, err := os.ReadFile(pathOrUrl)
		if err != nil {
			fmt.Println("Error reading file:", err)
			return
		}

		data = string(fileContent)
	}

	fmt.Println(mesi.MESIParse(data, config))
}
