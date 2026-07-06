package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/mesi/cache_memcached"
	"github.com/crazy-goat/go-mesi/mesi/cache_redis"
	"github.com/redis/go-redis/v9"
)

var (
	listen        = flag.String("listen", ":8080", "Listen address (default :8080)")
	backend       = flag.String("backend", "", "Upstream backend URL (required)")
	maxDepth      = flag.Uint("max-depth", 5, "Maximum recursion depth")
	timeout       = flag.Float64("timeout", 10.0, "Request timeout in seconds")
	parseOnHeader = flag.Bool("parse-on-header", false, "Only parse when Edge-control: dca=esi header is present")
	blockPrivate  = flag.Bool("block-private-ips", true, "Block private IP addresses")
	debug         = flag.Bool("debug", false, "Enable debug logging")

	// Cache backend flags
	cacheBackend          = flag.String("cache-backend", "", "Cache backend: memory, redis, memcached (default: off)")
	cacheSize             = flag.Int("cache-size", 10000, "Max cache entries for memory backend")
	cacheTTL              = flag.Duration("cache-ttl", 0, "Cache TTL (e.g. 30s, 5m); 0 = no expiry")
	cacheRedisAddr        = flag.String("cache-redis-addr", "localhost:6379", "Redis server address (host:port)")
	cacheRedisPassword    = flag.String("cache-redis-password", "", "Redis password")
	cacheRedisDB          = flag.Int("cache-redis-db", 0, "Redis database number")
	cacheMemcachedServers = flag.String("cache-memcached-servers", "", "Comma-separated Memcached servers (host:port)")
)

// initCache wires a cache backend into the config. It validates inputs eagerly
// so a misconfigured backend never silently degrades to "no cache".
func initCache(config mesi.EsiParserConfig, backend string, size int, ttl time.Duration,
	redisAddr, redisPassword string, redisDB int, memcachedServers string) (mesi.EsiParserConfig, error) {

	switch backend {
	case "":
		// no cache
	case "memory":
		if size < 1 {
			return config, fmt.Errorf("cache-size must be >= 1, got %d", size)
		}
		if ttl < 0 {
			return config, fmt.Errorf("cache-ttl must be >= 0, got %v", ttl)
		}
		config.Cache = mesi.NewMemoryCache(size, ttl)
		config.CacheTTL = ttl
	case "redis":
		if ttl < 0 {
			return config, fmt.Errorf("cache-ttl must be >= 0, got %v", ttl)
		}
		if err := validateHostPort(redisAddr); err != nil {
			return config, fmt.Errorf("invalid --cache-redis-addr: %w", err)
		}
		rdb := redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       redisDB,
		})
		// The caller must Close() the client — see initCacheClient wrapper
		// in main() for the lifecycle.
		config.Cache = cache_redis.NewRedisCache(rdb, ttl)
		config.CacheTTL = ttl

	case "memcached":
		servers := splitAndTrim(memcachedServers, ",")
		if len(servers) == 0 {
			return config, fmt.Errorf("--cache-memcached-servers is required for memcached backend")
		}
		if ttl < 0 {
			return config, fmt.Errorf("cache-ttl must be >= 0, got %v", ttl)
		}
		for _, s := range servers {
			if err := validateHostPort(s); err != nil {
				return config, fmt.Errorf("invalid memcached server %q: %w", s, err)
			}
		}
		mc := memcache.New(servers...)
		config.Cache = cache_memcached.NewMemcachedCache(mc, ttl)
		config.CacheTTL = ttl
	default:
		return config, fmt.Errorf("unknown cache backend %q — expected memory, redis, or memcached", backend)
	}
	return config, nil
}

// initCacheClient opens a redis.Client and wraps initCache with a deferred
// close for the caller. Only used from main(); tests exercise initCache
// directly without a real connection.
func initCacheClient(config mesi.EsiParserConfig, backend string, size int, ttl time.Duration,
	redisAddr, redisPassword string, redisDB int, memcachedServers string) (mesi.EsiParserConfig, func(), error) {

	switch backend {
	case "redis":
		rdb := redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: redisPassword,
			DB:       redisDB,
		})
		config.Cache = cache_redis.NewRedisCache(rdb, ttl)
		config.CacheTTL = ttl
		return config, func() { rdb.Close() }, nil
	default:
		// For memory, memcached, and empty — no client lifecycle needed.
		cfg, err := initCache(config, backend, size, ttl, redisAddr, redisPassword, redisDB, memcachedServers)
		return cfg, func() {}, err
	}
}

// validateHostPort checks that addr has the form host:port with a numeric
// port in [1, 65535]. This matches the validation used by Apache (#175).
func validateHostPort(addr string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%q is not host:port", addr)
	}
	if host == "" {
		return fmt.Errorf("%q has empty host", addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("port %q is not a number", portStr)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of range [1, 65535]", port)
	}
	return nil
}

// splitAndTrim splits s by sep and returns non-empty trimmed tokens.
func splitAndTrim(s, sep string) []string {
	var out []string
	for _, tok := range strings.Split(s, sep) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		out = append(out, tok)
	}
	return out
}

func cacheLabel(backend string) string {
	if backend == "" {
		return "off"
	}
	return backend
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: mesi-proxy [options]\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *backend == "" {
		fmt.Fprintf(os.Stderr, "Error: --backend is required\n")
		os.Exit(1)
	}

	config := mesi.CreateDefaultConfig()
	config.MaxDepth = *maxDepth
	config.Timeout = time.Duration(*timeout * float64(time.Second))
	config.ParseOnHeader = *parseOnHeader
	config.BlockPrivateIPs = *blockPrivate
	config.Debug = *debug

	config, cleanup, err := initCacheClient(config, *cacheBackend, *cacheSize, *cacheTTL,
		*cacheRedisAddr, *cacheRedisPassword, *cacheRedisDB, *cacheMemcachedServers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	proxy, err := NewProxy(*backend, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating proxy: %v\n", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:         *listen,
		Handler:      proxy,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Starting ESI proxy server on %s", *listen)
		log.Printf("Backend: %s", *backend)
		log.Printf("Max depth: %d, Timeout: %.1fs, Parse on header: %v, Cache: %s", *maxDepth, *timeout, *parseOnHeader, cacheLabel(*cacheBackend))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}
