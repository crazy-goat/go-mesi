package mesi

import (
	"regexp"
	"strings"
)

var varPattern = regexp.MustCompile(`\$\(([^)]+)\)`)

func evaluateExpression(expr string, config EsiParserConfig) string {
	return varPattern.ReplaceAllStringFunc(expr, func(match string) string {
		inner := match[2 : len(match)-1]

		if config.Variables != nil {
			if val, ok := config.Variables[inner]; ok {
				return val
			}
		}

		if strings.HasPrefix(inner, "HTTP_HEADER{") && strings.HasSuffix(inner, "}") {
			key := inner[12 : len(inner)-1]
			if config.RequestHeaders != nil {
				if val := config.RequestHeaders.Get(key); val != "" {
					return val
				}
			}
			if config.Variables != nil {
				if val, ok := config.Variables[match]; ok {
					return val
				}
			}
			return ""
		}

		if strings.HasPrefix(inner, "HTTP_COOKIE{") && strings.HasSuffix(inner, "}") {
			key := inner[12 : len(inner)-1]
			if config.RequestCookies != nil {
				if val, ok := config.RequestCookies[key]; ok {
					return val
				}
			}
			return ""
		}

		if strings.HasPrefix(inner, "QUERY_STRING{") && strings.HasSuffix(inner, "}") {
			key := inner[13 : len(inner)-1]
			if config.RequestQuery != nil {
				if val, ok := config.RequestQuery[key]; ok {
					return val
				}
			}
			return ""
		}

		return ""
	})
}

var varDefPattern = regexp.MustCompile(`<esi:variable\s+name="([^"]*)"\s+value="([^"]*)"\s*/>`)

func parseVarsBlock(rawContent string) map[string]string {
	vars := make(map[string]string)

	start := strings.Index(rawContent, ">")
	if start == -1 {
		return vars
	}
	end := strings.LastIndex(rawContent, "</")
	if end == -1 || end <= start {
		return vars
	}
	inner := rawContent[start+1 : end]

	matches := varDefPattern.FindAllStringSubmatch(inner, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			name := m[1]
			value := m[2]
			if name != "" {
				vars[name] = value
			}
		}
	}

	return vars
}
