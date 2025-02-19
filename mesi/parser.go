package mesi

import (
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
	defaultUrl string
	maxDepth   uint
	timeout    time.Duration
}

func (c EsiParserConfig) CanGoDeeper(t time.Duration) bool {
	return c.maxDepth >= 1 && c.timeout > t
}

func (c EsiParserConfig) ParseOnly() bool {
	return c.maxDepth < 1
}

func (c EsiParserConfig) DecreaseMaxDepth() EsiParserConfig {
	c.maxDepth = max(c.maxDepth-1, 0)

	return c
}

func (c EsiParserConfig) WithElapsedTime(t time.Duration) EsiParserConfig {
	c.timeout = max(c.timeout-t, 0)

	return c
}

func (c EsiParserConfig) OverrideConfig(token esiIncludeToken) EsiParserConfig {
	if token.Timeout != "" {
		tokenTimeout, err := strconv.ParseFloat(token.Timeout, 64)
		if err == nil && tokenTimeout > 0 {
			c.timeout = min(c.timeout, time.Duration(tokenTimeout*float64(time.Second)))
		}
	}

	if token.MaxDepth != "" {
		tokenMaxDepth, err := strconv.Atoi(token.MaxDepth)
		if err == nil && tokenMaxDepth >= 0 {
			c.maxDepth = min(c.maxDepth, uint(tokenMaxDepth)+1)
		}
	}

	return c
}

// Deprecated: FunctionName is deprecated, please use mEsiParse
func Parse(input string, maxDepth int, defaultUrl string) string {
	config := EsiParserConfig{
		defaultUrl: defaultUrl,
		maxDepth:   uint(maxDepth),
		timeout:    10 * time.Second, // default value 5 sec
	}

	return mEsiParse(input, config)
}

func mEsiParse(input string, config EsiParserConfig) string {
	start := time.Now()
	var wg sync.WaitGroup

	var result strings.Builder
	processed := unescape(input)
	tokens := esiTokenizer(processed)
	ch := make(chan Response)
	wg.Add(len(tokens))
	go func() {
		wg.Wait()
		close(ch)
	}()

	for index, token := range tokens {
		go func(id int, token esiToken, wg *sync.WaitGroup, ch chan<- Response) {
			defer wg.Done()
			res := Response{"", id}
			if !token.isEsi() {
				res.content = token.staticContent
			} else if token.esiTagType == "include" {

				include, err := parseInclude(token.esiTagContent)
				if err != nil {
					ch <- res
					return
				}
				newConfig := config.OverrideConfig(include).WithElapsedTime(time.Since(start))
				content := include.toString(newConfig)

				if config.CanGoDeeper(time.Since(start)) {
					content = mEsiParse(content, newConfig.DecreaseMaxDepth().WithElapsedTime(time.Since(start)))
				}

				res.content = content
			}

			ch <- res
		}(index, token, &wg, ch)
	}

	var results []Response
	for res := range ch {
		results = append(results, res)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].index < results[j].index
	})

	for _, res := range results {
		result.WriteString(res.content)
	}

	return result.String()
}
