package main

// #include <stdlib.h>
// #include <string.h>
import "C"
import (
	"github.com/crazy-goat/go-mesi/mesi"
	"unsafe"
)

func defaultValues() (int, string) {
	return 5, "http://127.0.0.1/"
}

//export ParseDefault
func ParseDefault(input *C.char) *C.char {
	goInput := C.GoString(input)
	maxDepth, defaultUrl := defaultValues()
	result := mesi.Parse(goInput, maxDepth, defaultUrl)
	return C.CString(result)
}

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
