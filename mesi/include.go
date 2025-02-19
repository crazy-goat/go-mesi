package mesi

import (
	"encoding/xml"
)

type esiIncludeToken struct {
	XMLName xml.Name `xml:"include"`
	Src     string   `xml:"src,attr"`
	Alt     string   `xml:"alt,attr"`
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

	data, err := singleFetchUrl(token.Src, config.defaultUrl)

	if err != nil && token.Alt != "" {
		data, err = singleFetchUrl(token.Alt, config.defaultUrl)
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
