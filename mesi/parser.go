package mesi

import (
	"strings"
)

func removeUnsupportedESITags(input string) string {
	var result strings.Builder
	unsupportedTags := []string{"inline", "choose", "try", "remove", "vars", "comment"}
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
			result.WriteString(input[pos:])
			return result.String()
		}
		start += pos

		if start > pos {
			result.WriteString(input[pos:start])
		}

		var endTag string
		if tag == "comment" {
			endTag = ">"
		} else {
			endTag = "</esi:" + tag + ">"
		}

		end := strings.Index(input[start:], endTag)
		if end == -1 {
			result.WriteString(input[pos:])
			return result.String()
		}
		end += start + len(endTag)
		pos = end
	}
}

type includeData struct {
	src     string
	alt     string
	onerror string
	content string
}

func parseInclude(input string) includeData {
	include := includeData{src: "", alt: "", onerror: "", content: ""}

	start := strings.Index(input, "<esi:include")
	if start == -1 {
		return include
	}

	end := strings.Index(input[start:], ">")
	if end == -1 {
		return include
	}
	end += start

	attributes := input[start+13 : end]
	for _, attr := range strings.Fields(attributes) {
		kv := strings.SplitN(attr, "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			value := strings.Trim(kv[1], "\"")
			switch key {
			case "src":
				include.src = value
			case "alt":
				include.alt = value
			case "onerror":
				include.onerror = value
			}
		}
	}

	contentStart := end + 1
	contentEnd := strings.Index(input, "</esi:include>")
	if contentEnd != -1 {
		include.content = input[contentStart:contentEnd]
	}

	return include
}

type esiElement struct {
	isInclude bool
	text      string
	include   includeData
}

func parseMicroEsi2(input string) string {
	var elements []esiElement
	pos := 0
	openTag := "<esi:include "

	for {
		start := strings.Index(input[pos:], openTag)
		if start == -1 {
			elements = append(elements, esiElement{isInclude: false, text: input[pos:]})
			break
		}
		start += pos

		if start > pos {
			elements = append(elements, esiElement{isInclude: false, text: input[pos:start]})
		}

		end := strings.Index(input[start:], ">")
		fullTag := ""

		if end == -1 {
			elements = append(elements, esiElement{isInclude: false, text: input[pos:]})
			break
		} else if end > 0 && input[start+end-1] == '/' {
			end += start + 1
			fullTag = toLongEsiInclude(input[start:end])
		} else {
			endTag := "</esi:include>"
			end = strings.Index(input[start+len(openTag):], endTag)
			if end == -1 {
				elements = append(elements, esiElement{isInclude: false, text: input[pos:]})
				break
			}
			end += start + len(openTag) + len(endTag)
			fullTag = input[start:end]
		}

		includeData := parseInclude(fullTag)
		elements = append(elements, esiElement{isInclude: true, include: includeData})

		pos = end
	}

	return processEsiElements(elements)
}

func processEsiElements(elements []esiElement) string {
	var result strings.Builder
	for _, element := range elements {
		if element.isInclude {
			if element.include.src != "" {
				result.WriteString("included " + element.include.src)
			} else if element.include.content != "" {
				result.WriteString("error include src, content: " + element.include.content)
			} else {
				result.WriteString("error include src")
			}
		} else {
			result.WriteString(element.text)
		}
	}
	return result.String()
}

func toLongEsiInclude(shortTag string) string {
	if !strings.HasPrefix(shortTag, "<esi:include") || !strings.HasSuffix(shortTag, "/>") {
		return shortTag
	}
	longTag := strings.TrimSuffix(shortTag, "/>") + "></esi:include>"
	return longTag
}

func Parse(input string) string {
	processed := unescape(input)
	processed = removeUnsupportedESITags(processed)
	finalOutput := parseMicroEsi2(processed)
	return finalOutput
}
