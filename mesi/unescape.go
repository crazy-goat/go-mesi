package mesi

import "strings"

func unescape(input string) string {
	const open = "<!--esi"
	const close = "-->"

	var result strings.Builder
	pos := 0
	for {
		start := strings.Index(input[pos:], open)
		if start == -1 {
			result.WriteString(input[pos:])
			return result.String()
		}
		start += pos

		if start > pos {
			result.WriteString(input[pos:start])
		}

		bodyStart := start + len(open)
		end := strings.Index(input[bodyStart:], close)
		if end == -1 {
			// unclosed — preserve original literal in output
			result.WriteString(input[start:])
			return result.String()
		}
		result.WriteString(input[bodyStart : bodyStart+end])
		pos = bodyStart + end + len(close)
	}
}
