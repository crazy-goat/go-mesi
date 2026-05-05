package mesi

import (
	"encoding/xml"
	"errors"
)

type esiResponse struct {
	Data          string
	IsEsiResponse bool
	Error         error
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
		logger.Debug("include_failed", "src", token.Src, "error", err.Error())

		if token.OnError == "continue" {
			return "", false
		}

		if token.Content != "" {
			return token.Content, false
		}

		return config.IncludeErrorMarker, false
	}

	return data, isEsiResponse
}
