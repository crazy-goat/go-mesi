package mesi

import (
	"context"
	"time"
)

// fetchConcurrent fetches Src and Alt in parallel and returns the first successful result.
// If both fail, it returns the last error. The losing request is cancelled via context.
func fetchConcurrent(token *esiIncludeToken, config EsiParserConfig) (string, bool, error) {
	if token.Alt == "" {
		return singleFetchUrlWithContext(token.Src, config, config.Context)
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if config.Context != nil {
		ctx, cancel = context.WithCancel(config.Context)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	resultChan := make(chan esiResponse, 2)

	runTask := func(url string) {
		data, isEsiResponse, err := singleFetchUrlWithContext(url, config, ctx)
		select {
		case resultChan <- esiResponse{Data: data, IsEsiResponse: isEsiResponse, Error: err}:
		case <-ctx.Done():
		}
	}

	go runTask(token.Src)
	go runTask(token.Alt)

	var lastErr error
	for i := 0; i < 2; i++ {
		select {
		case result := <-resultChan:
			if result.Error == nil {
				cancel()
				return result.Data, result.IsEsiResponse, nil
			}
			lastErr = result.Error
		case <-ctx.Done():
			cancel()
			if lastErr == nil {
				return "", false, ctx.Err()
			}
			return "", false, lastErr
		}
	}
	cancel()
	return "", false, lastErr
}

func fetchFallback(token *esiIncludeToken, config EsiParserConfig) (string, bool, error) {
	logger := config.getLogger()
	start := time.Now()
	var data string
	var err error
	var isEsiResponse bool

	data, isEsiResponse, err = singleFetchUrlWithContext(token.Src, config, config.Context)
	if err != nil && token.Alt != "" {
		logger.Debug("fallback_triggered", "primary", token.Src, "alt", token.Alt, "error", err.Error())
		return singleFetchUrlWithContext(token.Alt, config.WithElapsedTime(time.Since(start)), config.Context)
	}

	return data, isEsiResponse, err
}
