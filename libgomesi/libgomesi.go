package main

// #include <stdlib.h>
// #include <string.h>
import "C"
import (
	"net/http"
	"strings"
	"time"
	"unsafe"

	"github.com/crazy-goat/go-mesi/mesi"
)

var (
	sharedTransport *http.Transport
	sharedClient    *http.Client
)

// InitHTTPClient creates a shared HTTP client with SSRF protection.
// Call once per worker process (e.g. in module init) before Parse.
// The blockPrivateIPs parameter controls dial-time private IP blocking.
// Subsequent Parse calls reuse this client for connection pooling.
//
//export InitHTTPClient
func InitHTTPClient(blockPrivateIPs C.int) {
	config := mesi.EsiParserConfig{
		BlockPrivateIPs: blockPrivateIPs != 0,
	}
	sharedTransport = mesi.NewSSRFSafeTransport(config)
	sharedClient = &http.Client{
		Transport: sharedTransport,
		Timeout:   30 * time.Second,
	}
}

// FreeHTTPClient closes idle connections on the shared HTTP client.
// Call in process exit handler to prevent resource leaks.
// Idempotent — safe to call multiple times.
//
//export FreeHTTPClient
func FreeHTTPClient() {
	if sharedTransport != nil {
		sharedTransport.CloseIdleConnections()
		sharedTransport = nil
	}
	sharedClient = nil
}

func applySharedClient(config *mesi.EsiParserConfig) {
	if sharedClient != nil {
		client := *sharedClient
		client.Timeout = config.Timeout
		config.HTTPClient = &client
	}
}

// ParseDefault parses ESI tags using sensible defaults (maxDepth=5, defaultUrl=http://127.0.0.1/).
// Caller must free the returned string with FreeString.
//
//export ParseDefault
func ParseDefault(input *C.char) *C.char {
	goInput := C.GoString(input)
	config := mesi.EsiParserConfig{
		DefaultUrl: "http://127.0.0.1/",
		MaxDepth:   5,
		Timeout:    30 * time.Second,
	}
	applySharedClient(&config)
	result := mesi.MESIParse(goInput, config)
	return C.CString(result)
}

// Parse parses ESI tags with explicit configuration.
// Parameters:
//   - input: ESI markup string to parse
//   - maxDepth: maximum nesting depth for includes (recommended: 5)
//   - defaultUrl: base URL for relative include paths
//
// Returns parsed HTML with ESI tags replaced by their content.
// Caller must free the returned string with FreeString.
//
//export Parse
func Parse(input *C.char, maxDepth C.int, defaultUrl *C.char) *C.char {
	goInput := C.GoString(input)
	goMaxDepth := int(maxDepth)
	goDefaultUrl := C.GoString(defaultUrl)
	config := mesi.EsiParserConfig{
		DefaultUrl: goDefaultUrl,
		MaxDepth:   uint(goMaxDepth),
		Timeout:    30 * time.Second,
	}
	applySharedClient(&config)
	result := mesi.MESIParse(goInput, config)
	return C.CString(result)
}

// ParseWithConfig parses ESI tags with full configuration.
// Parameters:
//   - input: ESI markup string to parse
//   - maxDepth: maximum nesting depth for includes (recommended: 5)
//   - defaultUrl: base URL for relative include paths
//   - allowedHosts: space-separated list of allowed hostnames (or empty for no restriction)
//   - blockPrivateIPs: set to 1 to block private/reserved IP addresses
//
// Returns parsed HTML with ESI tags replaced by their content.
// Caller must free the returned string with FreeString.
//
//export ParseWithConfig
func ParseWithConfig(input *C.char, maxDepth C.int, defaultUrl *C.char, allowedHosts *C.char, blockPrivateIPs C.int) *C.char {
	goInput := C.GoString(input)
	goMaxDepth := int(maxDepth)
	goDefaultUrl := C.GoString(defaultUrl)

	hostsStr := C.GoString(allowedHosts)
	var hosts []string
	for _, h := range strings.Fields(hostsStr) {
		hosts = append(hosts, h)
	}

	config := mesi.EsiParserConfig{
		DefaultUrl:      goDefaultUrl,
		MaxDepth:        uint(goMaxDepth),
		Timeout:         30 * time.Second,
		AllowedHosts:    hosts,
		BlockPrivateIPs: blockPrivateIPs != 0,
	}
	applySharedClient(&config)
	result := mesi.MESIParse(goInput, config)
	return C.CString(result)
}

// FreeString frees memory allocated by Parse and ParseDefault.
// Call this for every string returned by the Parse functions.
//
//export FreeString
func FreeString(str *C.char) {
	C.free(unsafe.Pointer(str))
}

func main() {}
