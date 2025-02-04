package mesi

import (
	"strings"
)

type esiElement struct {
	isInclude bool
	text      string
	include   esiIncludeToken
}

func toLongEsiInclude(shortTag string) string {
	if !strings.HasPrefix(shortTag, "<esi:include") || !strings.HasSuffix(shortTag, "/>") {
		return shortTag
	}
	longTag := strings.TrimSuffix(shortTag, "/>") + "></esi:include>"
	return longTag
}

func Parse(input string) string {
	var result strings.Builder
	processed := unescape(input)
	tokens := esiTokenizer(processed)
	for _, token := range tokens {
		if token.isEsi() {
			switch {
			case token.esiTagType == "include":
				include := parseInclude(token.esiTagContent)
				result.WriteString(include.toString())
			}
		}
		result.WriteString(token.staticContent)
		continue
	}
	//finalOutput := parseMicroEsi2(processed)
	return result.String()
}
