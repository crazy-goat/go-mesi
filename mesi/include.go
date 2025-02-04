package mesi

import (
	"encoding/xml"
	"fmt"
)

type esiIncludeToken struct {
	XMLName xml.Name `xml:"include"`
	Src     string   `xml:"src,attr"`
	Alt     string   `xml:"alt,attr"`
	OnError string   `xml:"onerror,attr"`
	Content string   `xml:",innerxml"`
}

func parseInclude(input string) esiIncludeToken {
	var esi esiIncludeToken
	err := xml.Unmarshal([]byte(input), &esi)
	if err != nil {
		fmt.Println("Błąd podczas parsowania XML:", err)
	}

	return esi
}

func (token *esiIncludeToken) toString() string {

	data, err := singleFetchUrl(token.Src)

	if err != nil && token.Alt != "" {
		data, err = singleFetchUrl(token.Alt)
	}

	if err != nil {
		if token.OnError == "continue" {
			return ""
		}
		return err.Error()
	}
	return data
}
