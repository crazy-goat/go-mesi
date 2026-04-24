package mesi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Response struct {
	content string
	index   int
}

type EsiParserConfig struct {
	Context               context.Context
	DefaultUrl            string
	MaxDepth              uint
	Timeout               time.Duration
	ParseOnHeader         bool
	AllowedHosts          []string
	BlockPrivateIPs       bool
	MaxResponseSize       int64         // 0 = unlimited, default 10MB
	MaxConcurrentRequests int           // 0 = unlimited (backward compatible)
	HTTPClient            *http.Client  // shared client for connection pooling, nil = create per request
	Cache                 Cache         // nil = no caching (backward compatible)
	CacheTTL              time.Duration // Default TTL for cached entries
	CacheKeyFunc          CacheKeyFunc  // Custom cache key function (nil = DefaultCacheKey)
	Debug                 bool          // Enable debug logging
	Logger                Logger        // Custom logger (nil = DiscardLogger when Debug is false)
	requestSemaphore      chan struct{} // semaphore for limiting HTTP requests
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

	ch := make(chan Response, len(tokens))

	var semaphore chan struct{}
	if config.MaxConcurrentRequests < 0 {
		config.MaxConcurrentRequests = 0
	}
	if config.MaxConcurrentRequests > 0 {
		semaphore = make(chan struct{}, config.MaxConcurrentRequests)
		config = config.setSemaphore(semaphore)
	}

	for index, token := range tokens {
		go func(id int, token esiToken, ch chan<- Response, cfg EsiParserConfig, l Logger) {
			res := Response{"", id}
			if !token.isEsi() {
				res.content = token.staticContent
			} else if token.esiTagType == "include" {
				l.Debug("token_processing", "token_type", token.esiTagType, "index", id)

				include, err := parseInclude(token.esiTagContent)
				if err != nil {
					l.Debug("parse_error", "error", err.Error())
					ch <- res
					return
				}
				newConfig := cfg.OverrideConfig(include).WithElapsedTime(time.Since(start))
				content, isEsiResponse := include.toString(newConfig)

				if cfg.CanGoDeeper(time.Since(start)) && (isEsiResponse || !newConfig.ParseOnHeader) {
					content = MESIParse(content, newConfig.DecreaseMaxDepth().WithElapsedTime(time.Since(start)))
				}

				res.content = content
			} else {
				l.Debug("token_processing", "token_type", token.esiTagType, "index", id)
			}

			ch <- res
		}(index, token, ch, config, logger)
	}

	var results []Response
ResultLoop:
	for range tokens {
		select {
		case <-ctx.Done():
			break ResultLoop
		case res := <-ch:
			results = append(results, res)
		}
	}

	return assembleResults(results, result)
}
