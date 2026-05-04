package mesi

import (
	"context"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Response struct {
	content string
	index   int
}

func assembleResults(results []Response) string {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].index < results[j].index
	})

	var result strings.Builder
	total := 0
	for _, r := range results {
		total += len(r.content)
	}
	result.Grow(total)

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
		jobs := make(chan esiJob, len(esiJobs))

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
				break ResultLoop
			case res := <-ch:
				results = append(results, res)
			}
		}

		wg.Wait()
	}

	return assembleResults(results)
}
