package mesi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestNestedMESIParseDoesNotShareVariablesMap pins #645: EsiParserConfig is
// copied by value, but Variables is a map, so sibling nested parses for
// include bodies used to merge their <esi:vars> definitions into one shared
// map. Concurrent fragments then read and wrote it from different goroutines,
// which `go test -race` reports and which can end a render in "fatal error:
// concurrent map writes".
//
// The contract asserted here:
//   - a nested parse inherits the variables the parent render defined so far;
//   - the render never mutates a caller-supplied Variables map (the
//     deterministic proof that sibling fragments did not share it), and
//   - every fragment renders its own variable plus the inherited one.
func TestNestedMESIParseDoesNotShareVariablesMap(t *testing.T) {
	const fragments = 8

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/fragment/")
		_, _ = fmt.Fprintf(w,
			`<esi:vars><esi:variable name="FRAGMENT_%[1]s" value="%[1]s"/></esi:vars>fragment:%[1]s:$(ROOT)`,
			name)
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxDepth = 2
	config.BlockPrivateIPs = false
	config.DefaultUrl = server.URL + "/"
	config.MaxWorkers = fragments
	// A caller-supplied map: before the fix every nested parse merged into
	// this map, so the render's fragments raced on it and left their
	// definitions behind.
	config.Variables = map[string]string{"ROOT": "root"}

	var input strings.Builder
	for i := 0; i < fragments; i++ {
		input.WriteString(`<!--esi <esi:include src="/fragment/`)
		input.WriteString(strconv.Itoa(i))
		input.WriteString(`"/>-->`)
	}

	result := MESIParse(input.String(), config)

	if got := strings.Count(result, ":root"); got != fragments {
		t.Fatalf("rendered %d inherited root variables, want %d: %q", got, fragments, result)
	}
	for i := 0; i < fragments; i++ {
		if want := "fragment:" + strconv.Itoa(i) + ":root"; !strings.Contains(result, want) {
			t.Errorf("result does not contain %q: %q", want, result)
		}
	}

	// A caller-supplied map must keep exactly its original entries: the
	// render works on its own copy instead of merging fragment definitions
	// back into the caller's map.
	if len(config.Variables) != 1 {
		t.Errorf("caller Variables = %v, want only the original ROOT entry", config.Variables)
	}
	if got := config.Variables["ROOT"]; got != "root" {
		t.Errorf("caller Variables[ROOT] = %q, want %q", got, "root")
	}
}
