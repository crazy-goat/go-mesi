package mesi

import (
	"encoding/xml"
	"time"
)

type esiIncludeToken struct {
	XMLName xml.Name `xml:"include"`
	Src     string   `xml:"src,attr"`
	Alt     string   `xml:"alt,attr"`
	Timeout string   `xml:"timeout,attr"`
	OnError string   `xml:"onerror,attr"`
	Content string   `xml:",innerxml"`
}

func parseInclude(input string) (token esiIncludeToken, err error) {
	var esi esiIncludeToken
	err = xml.Unmarshal([]byte(input), &esi)
	if err != nil {
		return esi, err
	}

	return esi, nil
}

func (token *esiIncludeToken) toString(config EsiParserConfig) string {
	start := time.Now()
	data, err := singleFetchUrl(token.Src, config)

	if err != nil && token.Alt != "" {
		data, err = singleFetchUrl(token.Alt, config.WithElapsedTime(time.Since(start)))
	}

	if err != nil {
		if token.OnError == "continue" {
			return ""
		}

		if token.Content != "" {
			return token.Content
		}

		return err.Error()
	}

	return data
}
