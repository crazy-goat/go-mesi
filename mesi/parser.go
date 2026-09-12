package mesi

import (
	"context"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Response struct {
	content string
	index   int
}

func assembleResults(results []Response) string {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].index < results[j].index
	})

	var result strings.Builder
	total := 0
	for _, r := range results {
		total += len(r.content)
	}
	result.Grow(total)

	for _, res := range results {
		result.WriteString(res.content)
	}

	return result.String()
}

// Deprecated: Parse is deprecated; use MESIParse with EsiParserConfig instead.
//
// Migration:
//
//	cfg := mesi.CreateDefaultConfig()
//	cfg.DefaultUrl = defaultUrl
//	cfg.MaxDepth = uint(maxDepth)
//	result := mesi.MESIParse(input, cfg)
func Parse(input string, maxDepth int, defaultUrl string) string {
	config := EsiParserConfig{
		Context:         context.Background(),
		DefaultUrl:      defaultUrl,
		MaxDepth:        uint(maxDepth),
		Timeout:         10 * time.Second,
		BlockPrivateIPs: true,
	}

	return MESIParse(input, config)
}

func MESIParse(input string, config EsiParserConfig) string {
	if config.Context == nil {
		config.Context = context.Background()
	}
	ctx, cancel := context.WithCancel(config.Context)
	defer cancel()

	config.Context = ctx

	logger := config.getLogger()
	start := time.Now()

	processed := unescape(input)
	tokens := esiTokenizer(processed)

	logger.Debug("parse_start", "input_size", len(input), "token_count", len(tokens))

	var semaphore chan struct{}
	if config.MaxConcurrentRequests < 0 {
		config.warn("max_concurrent_requests_invalid", "value", config.MaxConcurrentRequests)
		config.MaxConcurrentRequests = 0
	}
	if config.MaxConcurrentRequests > 0 {
		semaphore = make(chan struct{}, config.MaxConcurrentRequests)
		config = config.setSemaphore(semaphore)
	}

	type esiJob struct {
		id    int
		token esiToken
	}
	var esiJobs []esiJob
	var results []Response

	for index, token := range tokens {
		if token.esiTagType == ESI_VARS {
			vars := parseVarsBlock(token.esiTagContent)
			if config.Variables == nil {
				config.Variables = vars
			} else {
				for k, v := range vars {
					config.Variables[k] = v
				}
			}
			results = append(results, Response{"", index})
		} else if token.esiTagType == ESI_TRY {
			content := processTryBlock(token.esiTagContent, config, start)
			results = append(results, Response{content, index})
		} else if token.esiTagType == ESI_CHOOSE {
			content := processChooseBlock(token.esiTagContent, config)
			results = append(results, Response{content, index})
		} else if token.esiTagType == ESI_INLINE {
			content := processInlineBlock(token.esiTagContent)
			results = append(results, Response{content, index})
		} else if !token.isEsi() {
			content := evaluateExpression(token.staticContent, config)
			results = append(results, Response{content, index})
		} else {
			esiJobs = append(esiJobs, esiJob{index, token})
		}
	}

	if len(esiJobs) > 0 {
		maxWorkers := config.MaxWorkers
		if maxWorkers <= 0 {
			maxWorkers = runtime.NumCPU() * 4
		}

		ch := make(chan Response, len(esiJobs))

		workerCount := maxWorkers
		if workerCount > len(esiJobs) {
			workerCount = len(esiJobs)
		}

		var wg sync.WaitGroup
		jobs := make(chan esiJob, len(esiJobs))

		// NOTE: Traditional for loop is used instead of "for range workerCount"
		// (Go 1.22+ range-over-integer) for Traefik plugin compatibility.
		// Yaegi v0.16.1 panics on this syntax.
		// See: https://github.com/traefik/yaegi/issues/1701
		for i := 0; i < workerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					id := job.id
					token := job.token
					res := Response{"", id}

					if token.esiTagType == "include" {
						logger.Debug("token_processing", "token_type", token.esiTagType, "index", id)

						include, err := parseInclude(token.esiTagContent)
						if err != nil {
							logger.Debug("parse_error", "error", err.Error())
							ch <- res
							continue
						}
						include.Src = evaluateExpression(include.Src, config)
						include.Alt = evaluateExpression(include.Alt, config)
						newConfig := config.OverrideConfig(include).WithElapsedTime(time.Since(start))
						content, isEsiResponse, _ := include.toString(newConfig)

						if config.CanGoDeeper(time.Since(start)) && (isEsiResponse || !newConfig.ParseOnHeader) {
							content = MESIParse(content, newConfig.DecreaseMaxDepth().WithElapsedTime(time.Since(start)))
						}

						res.content = content
					} else {
						logger.Debug("token_processing", "token_type", token.esiTagType, "index", id)
					}

					ch <- res
				}
			}()
		}

		for _, job := range esiJobs {
			jobs <- job
		}
		close(jobs)

	ResultLoop:
		for i := 0; i < len(esiJobs); i++ {
			select {
			case <-ctx.Done():
				break ResultLoop
			case res := <-ch:
				results = append(results, res)
			}
		}

		wg.Wait()
	}

	return assembleResults(results)
}

// extractTryBlocks extracts the attempt and except body content from a raw
// <esi:try> tag content (e.g. "<esi:try><esi:attempt>...</esi:attempt>...").
// Properly handles nested <esi:try> blocks by tracking nesting depth.
// Returns the attempt body and except body (exceptBody is empty if absent).
func extractTryBlocks(raw string) (attemptBody, exceptBody string) {
	// Skip past the opening <esi:try ...>
	closeBracket := strings.Index(raw, ">")
	if closeBracket == -1 {
		return "", ""
	}
	pos := closeBracket + 1

	// Find first <esi:attempt> that is a direct child of this try block.
	// Skip over any nested <esi:try>...</esi:try> blocks.
	for {
		nextTry := strings.Index(raw[pos:], "<esi:try")
		nextAttempt := strings.Index(raw[pos:], "<esi:attempt")
		if nextAttempt == -1 {
			return "", ""
		}
		if nextTry != -1 && nextTry < nextAttempt {
			nestedEnd := findTryClose(raw[pos+nextTry:])
			if nestedEnd == -1 {
				return "", ""
			}
			pos += nextTry + nestedEnd
			continue
		}
		pos += nextAttempt
		break
	}

	// Find end of <esi:attempt ...>
	tagEnd := strings.Index(raw[pos:], ">")
	if tagEnd == -1 {
		return "", ""
	}
	pos += tagEnd + 1

	// Find matching </esi:attempt>
	attemptLen := findAttemptClose(raw[pos:])
	if attemptLen == -1 {
		attemptBody = raw[pos:]
		return attemptBody, ""
	}
	attemptBody = raw[pos : pos+attemptLen]
	pos += attemptLen + len("</esi:attempt>")

	// Check for <esi:except>
	exceptStart := strings.Index(raw[pos:], "<esi:except")
	if exceptStart == -1 {
		return attemptBody, ""
	}
	pos += exceptStart
	tagEnd = strings.Index(raw[pos:], ">")
	if tagEnd == -1 {
		return attemptBody, ""
	}
	pos += tagEnd + 1

	exceptLen := findExceptClose(raw[pos:])
	if exceptLen == -1 {
		return attemptBody, ""
	}
	exceptBody = raw[pos : pos+exceptLen]

	return attemptBody, exceptBody
}

// findAttemptClose finds the position (relative to s) of the </esi:attempt>
// that matches the first <esi:attempt> in the content. s starts after the
// opening <esi:attempt ...> tag. Handles nested <esi:try> and <esi:attempt>
// blocks by tracking nesting depth.
func findAttemptClose(s string) int {
	depth := 0
	pos := 0
	for {
		nextClose := strings.Index(s[pos:], "</esi:attempt>")
		nextCloseTry := strings.Index(s[pos:], "</esi:try>")
		firstClose := -1
		if nextClose != -1 && (nextCloseTry == -1 || nextClose < nextCloseTry) {
			firstClose = nextClose
		} else if nextCloseTry != -1 {
			firstClose = nextCloseTry
		} else if nextClose != -1 {
			firstClose = nextClose
		}
		if firstClose == -1 {
			return -1
		}

		nextOpenAttempt := strings.Index(s[pos:], "<esi:attempt")
		nextOpenTry := strings.Index(s[pos:], "<esi:try")
		firstOpen := -1
		if nextOpenAttempt != -1 && (nextOpenTry == -1 || nextOpenAttempt < nextOpenTry) {
			firstOpen = nextOpenAttempt
		} else if nextOpenTry != -1 {
			firstOpen = nextOpenTry
		} else if nextOpenAttempt != -1 {
			firstOpen = nextOpenAttempt
		}

		if firstOpen != -1 && firstOpen < firstClose {
			depth++
			nextEnd := strings.Index(s[pos+firstOpen:], ">")
			if nextEnd == -1 {
				return -1
			}
			pos += firstOpen + nextEnd + 1
			continue
		}

		if depth == 0 && firstClose == nextClose {
			return pos + nextClose
		}
		depth--
		var skipLen int
		if firstClose == nextClose {
			skipLen = len("</esi:attempt>")
		} else {
			skipLen = len("</esi:try>")
		}
		pos += firstClose + skipLen
	}
}

// findExceptClose finds the position (relative to s) of the </esi:except>
// that matches the first <esi:except> in the content. s starts after the
// opening <esi:except ...> tag. Handles nested blocks.
func findExceptClose(s string) int {
	depth := 0
	pos := 0
	for {
		nextClose := strings.Index(s[pos:], "</esi:except>")
		nextCloseTry := strings.Index(s[pos:], "</esi:try>")
		firstClose := -1
		if nextClose != -1 && (nextCloseTry == -1 || nextClose < nextCloseTry) {
			firstClose = nextClose
		} else if nextCloseTry != -1 {
			firstClose = nextCloseTry
		} else if nextClose != -1 {
			firstClose = nextClose
		}
		if firstClose == -1 {
			return -1
		}

		nextOpenExcept := strings.Index(s[pos:], "<esi:except")
		nextOpenTry := strings.Index(s[pos:], "<esi:try")
		firstOpen := -1
		if nextOpenExcept != -1 && (nextOpenTry == -1 || nextOpenExcept < nextOpenTry) {
			firstOpen = nextOpenExcept
		} else if nextOpenTry != -1 {
			firstOpen = nextOpenTry
		} else if nextOpenExcept != -1 {
			firstOpen = nextOpenExcept
		}

		if firstOpen != -1 && firstOpen < firstClose {
			depth++
			nextEnd := strings.Index(s[pos+firstOpen:], ">")
			if nextEnd == -1 {
				return -1
			}
			pos += firstOpen + nextEnd + 1
			continue
		}

		if depth == 0 && firstClose == nextClose {
			return pos + nextClose
		}
		depth--
		var skipLen int
		if firstClose == nextClose {
			skipLen = len("</esi:except>")
		} else {
			skipLen = len("</esi:try>")
		}
		pos += firstClose + skipLen
	}
}

// findTryClose finds the position (relative to s) of the </esi:try>
// that matches the first <esi:try> in s. s starts at the opening
// <esi:try tag. Handles nested <esi:try> blocks.
func findTryClose(s string) int {
	depth := 0
	pos := 0
	for {
		nextClose := strings.Index(s[pos:], "</esi:try>")
		if nextClose == -1 {
			return -1
		}

		nextOpen := strings.Index(s[pos:], "<esi:try")
		if nextOpen != -1 && nextOpen < nextClose {
			depth++
			nextEnd := strings.Index(s[pos+nextOpen:], ">")
			if nextEnd == -1 {
				return -1
			}
			pos += nextOpen + nextEnd + 1
			continue
		}

		if depth == 0 {
			return pos + nextClose
		}
		depth--
		pos += nextClose + len("</esi:try>")
	}
}

// evaluateTest evaluates a boolean test expression from an <esi:when test="...">
// attribute. Supports boolean literals: "true", "false", "", "0", "1".
// $(...) variables in the expression are resolved before evaluation.
// Full expression syntax (comparisons, boolean operators) can be added later.
func evaluateTest(expr string, config EsiParserConfig) bool {
	expr = evaluateExpression(strings.TrimSpace(expr), config)
	switch expr {
	case "true", "1":
		return true
	case "false", "0", "":
		return false
	}
	return false
}

// esiWhenBlock represents a single <esi:when test="...">...</esi:when> block.
type esiWhenBlock struct {
	Test string
	Body string
}

// extractChooseBlocks parses the raw content of an <esi:choose> token and
// extracts the list of <esi:when> blocks and optional <esi:otherwise> body.
// Properly handles nested <esi:choose> blocks by tracking nesting depth.
func extractChooseBlocks(raw string) (whens []esiWhenBlock, otherwise string) {
	closeBracket := strings.Index(raw, ">")
	if closeBracket == -1 {
		return nil, ""
	}
	pos := closeBracket + 1

	for {
		// Find next potential child tag
		nextWhen := findChooseChildTag(raw[pos:], "<esi:when")
		nextOtherwise := findChooseChildTag(raw[pos:], "<esi:otherwise")
		nextClose := findChooseChildTag(raw[pos:], "</esi:choose>")

		// If no more children, stop
		if nextWhen == -1 && nextOtherwise == -1 {
			break
		}

		// Check if we hit </esi:choose> first
		if nextClose != -1 && (nextWhen == -1 || nextClose < nextWhen) && (nextOtherwise == -1 || nextClose < nextOtherwise) {
			break
		}

		if nextWhen != -1 && (nextOtherwise == -1 || nextWhen < nextOtherwise) {
			// Process <esi:when test="...">
			tagStart := pos + nextWhen

			// Extract test attribute
			testStart := strings.Index(raw[tagStart:], "test=\"")
			if testStart == -1 {
				// Malformed, skip
				quoteEnd := strings.Index(raw[tagStart:], ">")
				if quoteEnd == -1 {
					break
				}
				pos = tagStart + quoteEnd + 1
				continue
			}
			testStart += tagStart + len("test=\"")
			testEnd := strings.Index(raw[testStart:], "\"")
			if testEnd == -1 {
				break
			}
			testExpr := raw[testStart : testStart+testEnd]

			// Find end of opening <esi:when ...> tag
			bodyStart := testStart + testEnd
			tagEnd := strings.Index(raw[bodyStart:], ">")
			if tagEnd == -1 {
				break
			}
			bodyStart += tagEnd + 1

			// Find matching </esi:when> (depth-aware for nested <esi:choose>)
			whenBodyLen := findWhenClose(raw[bodyStart:])
			if whenBodyLen == -1 {
				// No matching close tag, consume rest and stop
				whenBody := raw[bodyStart:]
				whens = append(whens, esiWhenBlock{Test: testExpr, Body: whenBody})
				break
			}
			whenBody := raw[bodyStart : bodyStart+whenBodyLen]

			whens = append(whens, esiWhenBlock{Test: testExpr, Body: whenBody})
			pos = bodyStart + whenBodyLen + len("</esi:when>")
		} else if nextOtherwise != -1 {
			// Process <esi:otherwise>
			tagStart := pos + nextOtherwise

			// Find end of opening <esi:otherwise ...> tag
			tagEnd := strings.Index(raw[tagStart:], ">")
			if tagEnd == -1 {
				break
			}
			bodyStart := tagStart + tagEnd + 1

			// Find matching </esi:otherwise> (depth-aware)
			otherwiseLen := findOtherwiseClose(raw[bodyStart:])
			if otherwiseLen == -1 {
				// No matching close tag, consume rest and stop
				otherwise = raw[bodyStart:]
				break
			}
			otherwise = raw[bodyStart : bodyStart+otherwiseLen]
			pos = bodyStart + otherwiseLen + len("</esi:otherwise>")
		} else {
			break
		}
	}

	return whens, otherwise
}

// findChooseChildTag finds the first occurrence of tag in s that is a direct
// child (skipping nested <esi:choose> blocks).
func findChooseChildTag(s string, tag string) int {
	pos := 0
	for {
		nextNested := strings.Index(s[pos:], "<esi:choose")
		nextTarget := strings.Index(s[pos:], tag)
		if nextTarget == -1 {
			return -1
		}
		if nextNested != -1 && nextNested < nextTarget {
			// Skip this nested <esi:choose> block
			nestedClose := findChooseClose(s[pos+nextNested:])
			if nestedClose == -1 {
				return -1
			}
			pos += nextNested + nestedClose
			continue
		}
		return pos + nextTarget
	}
}

// findChooseClose finds the position (relative to s) of the </esi:choose>
// that matches the first <esi:choose> in s. s starts at the opening
// <esi:choose tag.
func findChooseClose(s string) int {
	depth := 0
	pos := 0
	for {
		nextClose := strings.Index(s[pos:], "</esi:choose>")
		if nextClose == -1 {
			return -1
		}
		nextOpen := strings.Index(s[pos:], "<esi:choose")
		if nextOpen != -1 && nextOpen < nextClose {
			depth++
			nextEnd := strings.Index(s[pos+nextOpen:], ">")
			if nextEnd == -1 {
				return -1
			}
			pos += nextOpen + nextEnd + 1
			continue
		}
		if depth == 0 {
			return pos + nextClose + len("</esi:choose>")
		}
		depth--
		pos += nextClose + len("</esi:choose>")
	}
}

// findWhenClose finds the position (relative to s) of the </esi:when>
// that matches the first <esi:when> in s. s starts after the opening
// <esi:when ...> tag. Handles nested <esi:choose> blocks.
func findWhenClose(s string) int {
	depth := 0
	pos := 0
	for {
		nextClose := strings.Index(s[pos:], "</esi:when>")
		nextCloseChoose := strings.Index(s[pos:], "</esi:choose>")
		firstClose := -1
		if nextClose != -1 && (nextCloseChoose == -1 || nextClose < nextCloseChoose) {
			firstClose = nextClose
		} else if nextCloseChoose != -1 {
			firstClose = nextCloseChoose
		} else if nextClose != -1 {
			firstClose = nextClose
		}
		if firstClose == -1 {
			return -1
		}
		nextOpenWhen := strings.Index(s[pos:], "<esi:when")
		nextOpenChoose := strings.Index(s[pos:], "<esi:choose")
		firstOpen := -1
		if nextOpenWhen != -1 && (nextOpenChoose == -1 || nextOpenWhen < nextOpenChoose) {
			firstOpen = nextOpenWhen
		} else if nextOpenChoose != -1 {
			firstOpen = nextOpenChoose
		} else if nextOpenWhen != -1 {
			firstOpen = nextOpenWhen
		}
		if firstOpen != -1 && firstOpen < firstClose {
			depth++
			nextEnd := strings.Index(s[pos+firstOpen:], ">")
			if nextEnd == -1 {
				return -1
			}
			pos += firstOpen + nextEnd + 1
			continue
		}
		if depth == 0 && firstClose == nextClose {
			return pos + nextClose
		}
		depth--
		var skipLen int
		if firstClose == nextClose {
			skipLen = len("</esi:when>")
		} else {
			skipLen = len("</esi:choose>")
		}
		pos += firstClose + skipLen
	}
}

// findOtherwiseClose finds the position (relative to s) of the </esi:otherwise>
// that matches the first <esi:otherwise> in s. s starts after the opening
// <esi:otherwise ...> tag. Handles nested blocks.
func findOtherwiseClose(s string) int {
	depth := 0
	pos := 0
	for {
		nextClose := strings.Index(s[pos:], "</esi:otherwise>")
		nextCloseChoose := strings.Index(s[pos:], "</esi:choose>")
		firstClose := -1
		if nextClose != -1 && (nextCloseChoose == -1 || nextClose < nextCloseChoose) {
			firstClose = nextClose
		} else if nextCloseChoose != -1 {
			firstClose = nextCloseChoose
		} else if nextClose != -1 {
			firstClose = nextClose
		}
		if firstClose == -1 {
			return -1
		}
		nextOpenOtherwise := strings.Index(s[pos:], "<esi:otherwise")
		nextOpenChoose := strings.Index(s[pos:], "<esi:choose")
		firstOpen := -1
		if nextOpenOtherwise != -1 && (nextOpenChoose == -1 || nextOpenOtherwise < nextOpenChoose) {
			firstOpen = nextOpenOtherwise
		} else if nextOpenChoose != -1 {
			firstOpen = nextOpenChoose
		} else if nextOpenOtherwise != -1 {
			firstOpen = nextOpenOtherwise
		}
		if firstOpen != -1 && firstOpen < firstClose {
			depth++
			nextEnd := strings.Index(s[pos+firstOpen:], ">")
			if nextEnd == -1 {
				return -1
			}
			pos += firstOpen + nextEnd + 1
			continue
		}
		if depth == 0 && firstClose == nextClose {
			return pos + nextClose
		}
		depth--
		var skipLen int
		if firstClose == nextClose {
			skipLen = len("</esi:otherwise>")
		} else {
			skipLen = len("</esi:choose>")
		}
		pos += firstClose + skipLen
	}
}

// processChooseBlock processes an <esi:choose> block. It evaluates each
// <esi:when test="..."> condition in order and renders the body of the first
// matching branch. If no <esi:when> matches and an <esi:otherwise> exists,
// it renders the otherwise body. The selected body is recursively parsed
// for further ESI processing (nested includes, vars, etc.).
func processChooseBlock(raw string, config EsiParserConfig) string {
	logger := config.getLogger()
	whens, otherwise := extractChooseBlocks(raw)

	logger.Debug("choose_start", "when_count", len(whens), "has_otherwise", otherwise != "")

	for _, w := range whens {
		if evaluateTest(w.Test, config) {
			logger.Debug("choose_when_matched", "test", w.Test)
			return MESIParse(w.Body, config)
		}
	}

	if otherwise != "" {
		logger.Debug("choose_no_match_rendering_otherwise")
		return MESIParse(otherwise, config)
	}

	logger.Debug("choose_no_match_no_otherwise")
	return ""
}

// processTryBlock handles a <esi:try> block. It processes the attempt body
// with error tracking. If any include inside attempt fails with an unhandled
// error (no onerror="continue" and no fallback body), the except body is
// rendered instead. If no except body exists, empty output is returned.
func processTryBlock(raw string, config EsiParserConfig, start time.Time) string {
	logger := config.getLogger()
	attemptBody, exceptBody := extractTryBlocks(raw)

	if attemptBody == "" {
		logger.Debug("try_empty_attempt", "raw_length", len(raw))
		return ""
	}

	result, hasError := processAttemptContent(attemptBody, config, start)

	if hasError {
		logger.Debug("try_attempt_failed_rendering_except")
		if exceptBody != "" {
			return MESIParse(exceptBody, config)
		}
		return ""
	}

	return result
}

// processAttemptContent processes content inside an <esi:attempt> block.
// It tokenizes and processes includes inline (not via worker pool) to detect
// unhandled include errors. Returns the rendered output and a flag indicating
// whether an unhandled error occurred.
func processAttemptContent(content string, config EsiParserConfig, start time.Time) (string, bool) {
	tokens := esiTokenizer(content)

	var hasError bool
	var results []Response

	for index, token := range tokens {
		if token.esiTagType == ESI_INCLUDE {
			include, err := parseInclude(token.esiTagContent)
			if err != nil {
				hasError = true
				break
			}
			include.Src = evaluateExpression(include.Src, config)
			include.Alt = evaluateExpression(include.Alt, config)
			newConfig := config.OverrideConfig(include).WithElapsedTime(time.Since(start))
			data, isEsiResponse, includeErr := include.toString(newConfig)

			if includeErr != nil {
				hasError = true
				break
			}

			if config.CanGoDeeper(time.Since(start)) && (isEsiResponse || !newConfig.ParseOnHeader) {
				data = MESIParse(data, newConfig.DecreaseMaxDepth().WithElapsedTime(time.Since(start)))
			}

			results = append(results, Response{data, index})
		} else if token.esiTagType == ESI_TRY {
			data := processTryBlock(token.esiTagContent, config, start)
			results = append(results, Response{data, index})
		} else if token.esiTagType == ESI_CHOOSE {
			data := processChooseBlock(token.esiTagContent, config)
			results = append(results, Response{data, index})
		} else if token.esiTagType == ESI_INLINE {
			data := processInlineBlock(token.esiTagContent)
			results = append(results, Response{data, index})
		} else if !token.isEsi() {
			content := evaluateExpression(token.staticContent, config)
			results = append(results, Response{content, index})
		} else {
			results = append(results, Response{"", index})
		}
	}

	return assembleResults(results), hasError
}
