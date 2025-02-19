package mesi

import (
	"context"
	"encoding/xml"
	"errors"
	"math/rand"
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

	randomValue := uint(rand.Intn(int(sum)))

	if randomValue < ratio.A {
		return token.Src
	}
	return token.Alt
}

func fetchAB(token *esiIncludeToken, config EsiParserConfig) (string, bool, error) {
	return singleFetchUrl(token.parseAB().selectUrl(token), config)
}

func fetchConcurrent(token *esiIncludeToken, config EsiParserConfig) (string, bool, error) {
	url1 := token.Src
	url2 := token.Alt

	if url2 == "" {
		url2 = token.Src
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultChan := make(chan esiResponse)

	runTask := func(url string) {
		select {
		case <-ctx.Done():
			return
		default:
			data, isEsiResponse, err := singleFetchUrl(url, config)
			select {
			case resultChan <- esiResponse{Data: data, IsEsiResponse: isEsiResponse, Error: err}:
			case <-ctx.Done():
				return
			}
		}
	}

	go runTask(url1)
	go runTask(url2)

	result := <-resultChan
	cancel()
	close(resultChan)

	return result.Data, result.IsEsiResponse, result.Error
}

func fetchFallback(token *esiIncludeToken, config EsiParserConfig) (string, bool, error) {
	start := time.Now()
	var data string
	var err error
	var isEsiResponse bool

	data, isEsiResponse, err = singleFetchUrl(token.Src, config)
	if err != nil && token.Alt != "" {
		return singleFetchUrl(token.Alt, config.WithElapsedTime(time.Since(start)))
	}

	return data, isEsiResponse, err
}

func (token *esiIncludeToken) toString(config EsiParserConfig) (string, bool) {
	var data string
	var err error
	var isEsiResponse bool

	if config.ParseOnly() {
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
