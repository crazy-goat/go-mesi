package mesi

import "strings"

func findUnquotedCloseBracket(s string) int {
	inQuote := false
	var quoteChar byte
	for i := 0; i < len(s); i++ {
		if inQuote {
			if s[i] == quoteChar {
				inQuote = false
			}
			continue
		}
		if s[i] == '"' || s[i] == '\'' {
			inQuote = true
			quoteChar = s[i]
			continue
		}
		if s[i] == '>' {
			return i
		}
	}
	return -1
}

func processInlineBlock(raw string) string {
	closeBracket := findUnquotedCloseBracket(raw)
	if closeBracket == -1 {
		return raw
	}

	body := raw[closeBracket+1:]

	closeTag := strings.LastIndex(body, "</esi:inline>")
	if closeTag == -1 {
		return body
	}

	return body[:closeTag]
}
