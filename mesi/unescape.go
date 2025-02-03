package mesi

import "strings"

func unescape(input string) string {
	var result strings.Builder
	pos := 0
	for {
		start := strings.Index(input[pos:], "<!--esi")
		if start == -1 {
			result.WriteString(input[pos:])
			return result.String()
		}
		start += pos

		if start > pos {
			result.WriteString(input[pos:start])
		}

		end := strings.Index(input[start:], "-->")
		if end == -1 {
			result.WriteString(input[pos:])
			return result.String()
		}
		end += start + 3

		result.WriteString(input[start+8 : end-3])
		pos = end
	}
}
