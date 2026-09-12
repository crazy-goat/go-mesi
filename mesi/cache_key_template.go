package mesi

import (
	"net/http"
	"strings"
)

// BuildCacheKey evaluates a cache key template string, substituting placeholders
// with values from the URL and HTTP request.
//
// Supported placeholders:
//   - ${url}            — the include URL
//   - ${header:Name}    — request header value (supports canonical, lowercase, uppercase forms)
//   - ${cookie:Name}    — cookie value (supports canonical, lowercase, uppercase forms)
//
// Unknown placeholders are left as literals.
func BuildCacheKey(url string, template string, r *http.Request) string {
	result := template
	result = strings.ReplaceAll(result, "${url}", url)

	// ${header:Name} substitution
	for key, vals := range r.Header {
		val := vals[0]
		result = strings.ReplaceAll(result, "${header:"+key+"}", val)
		result = strings.ReplaceAll(result, "${header:"+strings.ToLower(key)+"}", val)
		result = strings.ReplaceAll(result, "${header:"+strings.ToUpper(key)+"}", val)
	}

	// ${cookie:Name} substitution
	for _, c := range r.Cookies() {
		result = strings.ReplaceAll(result, "${cookie:"+c.Name+"}", c.Value)
		result = strings.ReplaceAll(result, "${cookie:"+strings.ToLower(c.Name)+"}", c.Value)
		result = strings.ReplaceAll(result, "${cookie:"+strings.ToUpper(c.Name)+"}", c.Value)
	}

	return result
}
