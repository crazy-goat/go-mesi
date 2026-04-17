package main

// #include <stdlib.h>
// #include <string.h>
import "C"
import (
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
	"unsafe"
)

// ParseDefault parses ESI tags using sensible defaults (maxDepth=5, defaultUrl=http://127.0.0.1/).
// Caller must free the returned string with FreeString.
//
// Example C usage:
//
//	char* result = ParseDefault(input);
//	// use result
//	FreeString(result);
//
//export ParseDefault
func ParseDefault(input *C.char) *C.char {
	goInput := C.GoString(input)
	config := mesi.EsiParserConfig{
		DefaultUrl: "http://127.0.0.1/",
		MaxDepth:   5,
		Timeout:    30 * time.Second,
	}
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
// Example C usage:
//
//	char* result = Parse(input, 5, "http://example.com/");
//	// use result
//	FreeString(result);
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
