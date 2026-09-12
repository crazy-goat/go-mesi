package mesi

import (
	"strings"
)

const (
	ESI_INCLUDE = "include"
	ESI_INLINE  = "inline"
	ESI_CHOOSE  = "choose"
	ESI_TRY     = "try"
	ESI_REMOVE  = "remove"
	ESI_COMMENT = "comment"
	ESI_VARS    = "vars"
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
	return token.staticContent != "" && !token.isEsi()
}

func (token *esiToken) isSupported() bool {
	return token.isEsi() && token.esiTagType == ESI_INCLUDE
}

// findMatchingEndTag finds the position (relative to s) of closeTag that
// matches the first openTag in s. Tracks nesting depth of <esi:*> tags so
// that nested blocks with the same tag type don't confuse matching.
func findMatchingEndTag(s string, openTag string, closeTag string) int {
	depth := 0
	pos := 0
	for {
		nextOpen := strings.Index(s[pos:], openTag)
		nextClose := strings.Index(s[pos:], closeTag)
		if nextClose == -1 {
			return -1
		}
		if nextOpen != -1 && nextOpen < nextClose {
			depth++
			// Advance past the opening tag's '>' to avoid re-matching
			tagEnd := strings.Index(s[pos+nextOpen:], ">")
			if tagEnd == -1 {
				return -1
			}
			pos += nextOpen + tagEnd + 1
			continue
		}
		depth--
		if depth == 0 {
			return pos + nextClose
		}
		pos += nextClose + len(closeTag)
	}
}

func esiTokenizer(input string) []esiToken {
	var esiTokens []esiToken
	unsupportedTags := []string{ESI_INLINE, ESI_CHOOSE, ESI_TRY, ESI_REMOVE, ESI_VARS, ESI_COMMENT, ESI_INCLUDE}
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

		var end int
		if tag == "try" || tag == "choose" || tag == "inline" || tag == "remove" || tag == "vars" {
			// For block tags that can contain nested ESI blocks,
			// find the matching closing tag using depth tracking.
			end = findMatchingEndTag(input[start:], "<esi:"+tag, endTag)
		} else {
			end = strings.Index(input[start:], endTag)
			if tag == "include" && end >= 0 && input[end+start-1] != '/' {
				endTag = "</esi:" + tag + ">"
				end = strings.Index(input[start:], endTag)
			}
		}

		if end == -1 {
			return append(esiTokens, esiToken{staticContent: input[start:]})
		}
		end += start + len(endTag)

		esiTokens = append(esiTokens, esiToken{esiTagContent: input[start:end], esiTagType: tag})
		pos = end
	}
}
