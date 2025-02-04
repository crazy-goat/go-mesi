package mesi

import (
	"sort"
	"strings"
	"sync"
	//"sync"
)

type Response struct {
	content string
	index   int
}

func Parse(input string) string {
	var wg sync.WaitGroup

	var result strings.Builder
	processed := unescape(input)
	tokens := esiTokenizer(processed)
	ch := make(chan Response)

	go func() {
		wg.Wait()
		close(ch)
	}()

	for index, token := range tokens {
		wg.Add(1)
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
				res.content = include.toString()
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
