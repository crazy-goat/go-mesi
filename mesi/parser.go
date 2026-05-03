package mesi

import (
	"context"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Response struct {
	content string
	index   int
}

type EsiParserConfig struct {
	Context       context.Context
	DefaultUrl    string
	MaxDepth      uint
	Timeout       time.Duration
	ParseOnHeader bool
	// AllowedHosts restricts ESI includes to specified domains.
	// Empty list allows all hosts (subject to BlockPrivateIPs).
	//
	// Note: AllowedHosts does NOT bypass BlockPrivateIPs by default.
	// Use AllowPrivateIPsForAllowedHosts to enable private-IP bypass.
	AllowedHosts    []string
	BlockPrivateIPs bool
	// AllowPrivateIPsForAllowedHosts allows hosts in AllowedHosts to bypass
	// the BlockPrivateIPs check.
	//
	// SECURITY WARNING: This creates a potential SSRF vector if an attacker
	// can control DNS for a host in AllowedHosts. Only use in trusted environments.
	//
	// Default: false (private IPs always blocked regardless of AllowedHosts).
	AllowPrivateIPsForAllowedHosts bool
	MaxResponseSize                int64         // 0 = unlimited, default 10MB
	MaxConcurrentRequests          int           // 0 = unlimited (backward compatible)
	HTTPClient                     *http.Client  // shared client for connection pooling, nil = create per request
	Cache                          Cache         // nil = no caching (backward compatible)
	CacheTTL                       time.Duration // Default TTL for cached entries
	CacheKeyFunc                   CacheKeyFunc  // Custom cache key function (nil = DefaultCacheKey)
	Debug                          bool          // Enable debug logging
	Logger                         Logger        // Custom logger (nil = DiscardLogger when Debug is false)
	// IncludeErrorMarker is rendered in place of a failed include when no
	// onerror="continue" and no fallback <esi:include> body is present.
	// Default: "" (silent). Set to something like "<!-- esi error -->" for
	// debugging, but NEVER include the raw error — see security advisory.
	IncludeErrorMarker             string
	// MaxWorkers caps the number of goroutines used to process ESI tokens
	// within a single MESIParse call. Zero means runtime.NumCPU()*4.
	// Static tokens do not count against this limit.
	MaxWorkers                     int
	requestSemaphore               chan struct{} // semaphore for limiting HTTP requests
}

func (c EsiParserConfig) getSemaphore() chan struct{} {
	return c.requestSemaphore
}

func (c EsiParserConfig) setSemaphore(s chan struct{}) EsiParserConfig {
	c.requestSemaphore = s
	return c
}

var discardLogger = DiscardLogger{}

func (c EsiParserConfig) getLogger() Logger {
	if c.Logger != nil {
		return c.Logger
	}
	if c.Debug {
		return DefaultLoggerNew()
	}
	return discardLogger
}

func (c EsiParserConfig) warn(msg string, keyvals ...interface{}) {
	logger := c.getLogger()
	if w, ok := logger.(LoggerWarn); ok {
		w.Warn(msg, keyvals...)
	} else {
		logger.Debug(msg, keyvals...)
	}
}

func (c EsiParserConfig) SetContext(ctx context.Context) EsiParserConfig {
	c.Context = ctx
	return c
}

func CreateDefaultConfig() EsiParserConfig {
	return EsiParserConfig{
		Context:         context.Background(),
		DefaultUrl:      "http://127.0.0.1/",
		MaxDepth:        5,
		Timeout:         10 * time.Second,
		ParseOnHeader:   false,
		BlockPrivateIPs: true,
		MaxResponseSize: 10 * 1024 * 1024, // 10MB default
		CacheKeyFunc:    DefaultCacheKey,
		Logger:          DiscardLogger{},
	}
}

func (c EsiParserConfig) CanGoDeeper(t time.Duration) bool {
	return c.MaxDepth >= 1 && c.Timeout > t
}

func (c EsiParserConfig) ParseOnly() bool {
	return c.MaxDepth < 1
}

func (c EsiParserConfig) DecreaseMaxDepth() EsiParserConfig {
	if c.MaxDepth > 0 {
		c.MaxDepth = c.MaxDepth - 1
	} else {
		c.MaxDepth = 0
	}

	return c
}

func (c EsiParserConfig) WithElapsedTime(t time.Duration) EsiParserConfig {
	if c.Timeout-t > 0 {
		c.Timeout = c.Timeout - t
	} else {
		c.Timeout = 0
	}

	return c
}

func (c EsiParserConfig) OverrideConfig(token esiIncludeToken) EsiParserConfig {
	if token.Timeout != "" {
		tokenTimeout, err := strconv.ParseFloat(token.Timeout, 64)
		if err == nil && tokenTimeout > 0 {
			timeoutLimit := time.Duration(tokenTimeout * float64(time.Second))
			if c.Timeout > timeoutLimit {
				c.Timeout = timeoutLimit
			}
		}
	}

	if token.MaxDepth != "" {
		tokenMaxDepth, err := strconv.Atoi(token.MaxDepth)
		if err == nil && tokenMaxDepth >= 0 {
			limit := uint(tokenMaxDepth) + 1
			if c.MaxDepth > limit {
				c.MaxDepth = limit
			}
		}
	}

	return c
}

func assembleResults(results []Response, result strings.Builder) string {
	sort.Slice(results, func(i, j int) bool {
		return results[i].index < results[j].index
	})

	for _, res := range results {
		result.WriteString(res.content)
	}

	return result.String()
}

// Deprecated: FunctionName is deprecated, please use mEsiParse
func Parse(input string, maxDepth int, defaultUrl string) string {
	config := EsiParserConfig{
		Context:         context.Background(),
		DefaultUrl:      defaultUrl,
		MaxDepth:        uint(maxDepth),
		Timeout:         10 * time.Second,
		BlockPrivateIPs: true,
	}

	return MESIParse(input, config)
}

func MESIParse(input string, config EsiParserConfig) string {
	if config.Context == nil {
		config.Context = context.Background()
	}
	ctx, cancel := context.WithCancel(config.Context)
	defer cancel()

	config.Context = ctx

	logger := config.getLogger()
	start := time.Now()

	var result strings.Builder
	processed := unescape(input)
	tokens := esiTokenizer(processed)

	logger.Debug("parse_start", "input_size", len(input), "token_count", len(tokens))

	var semaphore chan struct{}
	if config.MaxConcurrentRequests < 0 {
		config.MaxConcurrentRequests = 0
	}
	if config.MaxConcurrentRequests > 0 {
		semaphore = make(chan struct{}, config.MaxConcurrentRequests)
		config = config.setSemaphore(semaphore)
	}

	type esiJob struct {
		id    int
		token esiToken
	}
	var esiJobs []esiJob
	var results []Response

	for index, token := range tokens {
		if !token.isEsi() {
			results = append(results, Response{token.staticContent, index})
		} else {
			esiJobs = append(esiJobs, esiJob{index, token})
		}
	}

	if len(esiJobs) > 0 {
		maxWorkers := config.MaxWorkers
		if maxWorkers <= 0 {
			maxWorkers = runtime.NumCPU() * 4
		}

		ch := make(chan Response, len(esiJobs))

		workerCount := maxWorkers
		if workerCount > len(esiJobs) {
			workerCount = len(esiJobs)
		}

		var wg sync.WaitGroup
		jobs := make(chan esiJob)

		for range workerCount {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					id := job.id
					token := job.token
					res := Response{"", id}

					if token.esiTagType == "include" {
						logger.Debug("token_processing", "token_type", token.esiTagType, "index", id)

						include, err := parseInclude(token.esiTagContent)
						if err != nil {
							logger.Debug("parse_error", "error", err.Error())
							ch <- res
							continue
						}
						newConfig := config.OverrideConfig(include).WithElapsedTime(time.Since(start))
						content, isEsiResponse := include.toString(newConfig)

						if config.CanGoDeeper(time.Since(start)) && (isEsiResponse || !newConfig.ParseOnHeader) {
							content = MESIParse(content, newConfig.DecreaseMaxDepth().WithElapsedTime(time.Since(start)))
						}

						res.content = content
					} else {
						logger.Debug("token_processing", "token_type", token.esiTagType, "index", id)
					}

					ch <- res
				}
			}()
		}

		for _, job := range esiJobs {
			jobs <- job
		}
		close(jobs)

	ResultLoop:
		for i := 0; i < len(esiJobs); i++ {
			select {
			case <-ctx.Done():
				// Workers will send to buffered channel (capacity = len(esiJobs))
				// and exit when jobs channel is fully consumed.
				break ResultLoop
			case res := <-ch:
				results = append(results, res)
			}
		}
	}

	return assembleResults(results, result)
}
