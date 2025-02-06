package main

// #include <stdlib.h>
// #include <string.h>
import "C"
import (
	"github.com/crazy-goat/go-mesi/mesi"
	"unsafe"
)

//export Parse
func Parse(input *C.char, maxDepth C.int, defaultUrl *C.char) *C.char {
	goInput := C.GoString(input)
	goMaxDepth := int(maxDepth)
	goDefaultUrl := C.GoString(defaultUrl)
	result := mesi.Parse(goInput, goMaxDepth, goDefaultUrl)
	cResult := C.CString(result)
	return cResult
}

//export FreeString
func FreeString(str *C.char) {
	C.free(unsafe.Pointer(str))
}

func main() {}
