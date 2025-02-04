package mesi

import (
	"fmt"
	"strings"
)

const (
	ESI_INCLUDE string = "include"
	ESI_INLICNE        = "inline"
	ESI_CHOOSE         = "choose"
	ESI_TRY            = "try"
	ESI_REMOVE         = "remove"
	ESI_COMMENT        = "comment"
	ESI_VARS           = "vars"
)

type esiToken struct {
	staticContent string
	esiTagContent string
	esiTagType    string
}

func (token *esiToken) isEsi() bool {
	return token.esiTagType != "" && token.esiTagContent != ""
}

func (token *esiToken) isStaticText() bool {
	return token.staticContent != "" && token.esiTagType == ""
}

func (token *esiToken) isSupported() bool {
	return token.esiTagType == "include" && token.isEsi()
}

func (token *esiToken) printInfo() {
	if token.staticContent != "" {
		fmt.Print("s:", token.staticContent)
	} else {
		fmt.Print("e<", token.esiTagType, ">:", token.esiTagContent)
	}
}

func esiTokenizer(input string) []esiToken {
	var esiTokens []esiToken
	unsupportedTags := []string{ESI_INLICNE, ESI_CHOOSE, ESI_TRY, ESI_REMOVE, ESI_VARS, ESI_COMMENT, ESI_INCLUDE}
	pos := 0
	for {
		start := -1
		var tag string
		for _, t := range unsupportedTags {
			idx := strings.Index(input[pos:], "<esi:"+t)
			if idx != -1 && (start == -1 || idx < start) {
				start = idx
				tag = t
			}
		}

		if start == -1 {
			return append(esiTokens, esiToken{staticContent: input[pos:]})
		}
		start += pos

		if start > pos {
			esiTokens = append(esiTokens, esiToken{staticContent: input[pos:start]})
		}

		var endTag string
		if tag == "comment" || tag == "include" {
			endTag = ">"
		} else {
			endTag = "</esi:" + tag + ">"
		}

		end := strings.Index(input[start:], endTag)
		if tag == "include" && end >= 0 && input[end+start-1] != '/' {
			endTag = "</esi:" + tag + ">"
			end = strings.Index(input[start:], endTag)
		}

		if end == -1 {
			return append(esiTokens, esiToken{staticContent: input[pos:]})
		}
		end += start + len(endTag)

		esiTokens = append(esiTokens, esiToken{esiTagContent: input[start:end], esiTagType: tag})
		pos = end
	}
}
