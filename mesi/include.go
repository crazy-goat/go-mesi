package mesi

import (
	"encoding/xml"
	"errors"
	"time"
)

type esiIncludeToken struct {
	XMLName  xml.Name `xml:"include"`
	Src      string   `xml:"src,attr"`
	Alt      string   `xml:"alt,attr"`
	Timeout  string   `xml:"timeout,attr"`
	MaxDepth string   `xml:"max-depth,attr"`
	OnError  string   `xml:"onerror,attr"`
	Content  string   `xml:",innerxml"`
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
	start := time.Now()
	var data string
	var err error
	var isEsiResponse bool

	if config.ParseOnly() {
		err = errors.New("esi max depth")
	} else {
		data, isEsiResponse, err = singleFetchUrl(token.Src, config)
		if err != nil && token.Alt != "" {
			data, isEsiResponse, err = singleFetchUrl(token.Alt, config.WithElapsedTime(time.Since(start)))
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
