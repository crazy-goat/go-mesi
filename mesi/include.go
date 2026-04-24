package mesi

import (
	"context"
	"encoding/xml"
	"errors"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

type esiResponse struct {
	Data          string
	IsEsiResponse bool
	Error         error
}

type abRatio struct {
	A uint
	B uint
}

type esiIncludeToken struct {
	XMLName   xml.Name `xml:"include"`
	Src       string   `xml:"src,attr"`
	Alt       string   `xml:"alt,attr"`
	Timeout   string   `xml:"timeout,attr"`
	MaxDepth  string   `xml:"max-depth,attr"`
	FetchMode string   `xml:"fetch-mode,attr"`
	ABRatio   string   `xml:"ab-ratio,attr"`
	OnError   string   `xml:"onerror,attr"`
	Content   string   `xml:",innerxml"`
}

func parseInclude(input string) (token esiIncludeToken, err error) {
	var esi esiIncludeToken
	err = xml.Unmarshal([]byte(input), &esi)
	if err != nil {
		return esi, err
	}

	return esi, nil
}

func (token *esiIncludeToken) parseAB() abRatio {
	defaultValue := abRatio{
		A: 50,
		B: 50,
	}

	if !strings.Contains(token.ABRatio, ":") {
		return defaultValue
	}

	parts := strings.Split(token.ABRatio, ":")
	if len(parts) != 2 {
		return defaultValue
	}

	a, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return defaultValue
	}

	b, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return defaultValue
	}

	if a == 0 && b == 0 {
		return defaultValue
	}

	return abRatio{
		A: uint(a),
		B: uint(b),
	}
}

func (ratio abRatio) selectUrl(token *esiIncludeToken) string {
	if token.Alt == "" {
		return token.Src
	}

	sum := ratio.A + ratio.B

	if sum == 0 {
		return token.Src
	}

	randomValue := rand.IntN(int(sum))

	if randomValue < int(ratio.A) {
		return token.Src
	}
	return token.Alt
}

func fetchAB(token *esiIncludeToken, config EsiParserConfig) (string, bool, error) {
	logger := config.getLogger()
	selected := token.parseAB().selectUrl(token)
	logger.Debug("ab_ratio_select", "src", token.Src, "alt", token.Alt, "selected", selected)
	return singleFetchUrlWithContext(selected, config, config.Context)
}

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

	resultChan := make(chan esiResponse, 2)
	doneChan := make(chan struct{})

	runTask := func(url string) {
		data, isEsiResponse, err := singleFetchUrlWithContext(url, config, ctx)
		select {
		case resultChan <- esiResponse{Data: data, IsEsiResponse: isEsiResponse, Error: err}:
		case <-doneChan:
		}
	}

	go runTask(token.Src)
	go runTask(token.Alt)

	result := <-resultChan
	close(doneChan)
	cancel() // Cancel context immediately to stop the other HTTP request

	return result.Data, result.IsEsiResponse, result.Error
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

func (token *esiIncludeToken) toString(config EsiParserConfig) (string, bool) {
	logger := config.getLogger()
	var data string
	var err error
	var isEsiResponse bool

	logger.Debug("include_start", "src", token.Src, "fetch_mode", token.FetchMode, "max_depth", config.MaxDepth, "timeout", config.Timeout)

	if config.ParseOnly() {
		logger.Debug("max_depth_reached", "src", token.Src)
		err = errors.New("esi max depth")
	} else {
		switch token.FetchMode {
		case "ab":
			data, isEsiResponse, err = fetchAB(token, config)
		case "concurrent":
			data, isEsiResponse, err = fetchConcurrent(token, config)
		default:
			data, isEsiResponse, err = fetchFallback(token, config)
		}
	}

	if err != nil {
		if token.OnError == "continue" {
			return "", false
		}

		if token.Content != "" {
			return token.Content, false
		}

		return err.Error(), false
	}

	return data, isEsiResponse
}
