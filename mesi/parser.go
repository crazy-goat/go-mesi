package mesi

import (
	"sort"
	"strings"
	"sync"
)

type Response struct {
	content string
	index   int
}

type EsiParserConfig struct {
	defaultUrl string
	maxDepth   uint
}

func (c EsiParserConfig) DecreaseMaxDepth() EsiParserConfig {
	c.maxDepth = max(c.maxDepth-1, 0)

	return c
}

// Deprecated: FunctionName is deprecated, please use mEsiParse
func Parse(input string, maxDepth int, defaultUrl string) string {
	config := EsiParserConfig{
		defaultUrl: defaultUrl,
		maxDepth:   uint(maxDepth),
	}

	return mEsiParse(input, config)
}

func mEsiParse(input string, config EsiParserConfig) string {
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
				content := include.toString(config)

				if config.maxDepth > 1 {
					newConfig := config.DecreaseMaxDepth()
					content = mEsiParse(content, newConfig)
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
