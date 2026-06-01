package mesi

import (
	// NOTE: math/rand is used instead of math/rand/v2 for Traefik plugin
	// compatibility. Yaegi (Traefik's Go interpreter) does not support
	// packages introduced in Go 1.22+ like math/rand/v2.
	// See: https://github.com/traefik/yaegi/issues/1674
	"math/rand"
	"strconv"
	"strings"
)

type abRatio struct {
	A uint
	B uint
}

func (token *esiIncludeToken) parseAB() abRatio {
	defaultValue := abRatio{
		A: 50,
		B: 50,
	}

	if !strings.Contains(token.ABRatio, ":") {
		return defaultValue
	}

	parts := strings.Split(token.ABRatio, ":")
	if len(parts) != 2 {
		return defaultValue
	}

	a, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return defaultValue
	}

	b, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return defaultValue
	}

	if a == 0 && b == 0 {
		return defaultValue
	}

	return abRatio{
		A: uint(a),
		B: uint(b),
	}
}

func (ratio abRatio) selectUrl(token *esiIncludeToken, rng func(int) int) string {
	if token.Alt == "" {
		return token.Src
	}

	sum := ratio.A + ratio.B

	if sum == 0 {
		return token.Src
	}

	if rng == nil {
		rng = rand.Intn
	}

	randomValue := rng(int(sum))

	if randomValue < int(ratio.A) {
		return token.Src
	}
	return token.Alt
}

func fetchAB(token *esiIncludeToken, config EsiParserConfig) (string, bool, error) {
	logger := config.getLogger()
	selected := token.parseAB().selectUrl(token, nil)
	logger.Debug("ab_ratio_select", "src", token.Src, "alt", token.Alt, "selected", selected)
	return singleFetchUrlWithContext(selected, config, config.Context)
}
