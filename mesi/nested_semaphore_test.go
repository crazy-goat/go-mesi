package mesi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// nestedSemaphoreTest exercises the page-wide MaxConcurrentRequests contract
// across nesting levels: a nested MESIParse made for a fetched fragment must
// inherit the parent render's admission semaphore instead of installing a
// fresh one (#453).
//
// The fixture is two top-level includes ("/chain/N") whose bodies contain two
// nested includes each ("/leaf/N"). Chain handlers return immediately; leaf
// handlers park on a barrier until cap+1 leaves have arrived. With a correct
// page-wide semaphore at most cap fetches can hold a slot, so the barrier is
// never reached concurrently and the parked leaves are released by the
// fallback timer. With a per-level semaphore (the regression) the nested
// parses admit their own leaves while the sibling chain fetch is in flight and
// cap+1 leaves run at once. countingTransport records the client-observed
// peak, which is the same interval in which a fetch holds a semaphore slot.
type nestedFetchMode struct {
	name    string
	attrs   string
	usesAlt bool
}

var nestedFetchModes = []nestedFetchMode{
	{name: "default"},
	{name: "concurrent", attrs: ` fetch-mode="concurrent"`, usesAlt: true},
	{name: "ab", attrs: ` fetch-mode="ab" ab-ratio="100:0"`, usesAlt: true},
}

func (m nestedFetchMode) includeTag(src, alt string) string {
	if !m.usesAlt {
		return `<esi:include src="` + src + `"/>`
	}
	return `<esi:include src="` + src + `" alt="` + alt + `"` + m.attrs + `/>`
}

func nestedIncludeBody(mode nestedFetchMode, serverURL string) string {
	var body strings.Builder
	for i := 0; i < 2; i++ {
		id := strconv.Itoa(i)
		body.WriteString("<!--esi ")
		body.WriteString(mode.includeTag(
			serverURL+"/leaf/"+id,
			serverURL+"/leaf-alt/"+id,
		))
		body.WriteString("-->")
	}
	return body.String()
}

func nestedTopLevelInput(mode nestedFetchMode, serverURL string) string {
	var input strings.Builder
	for i := 0; i < 2; i++ {
		id := strconv.Itoa(i)
		input.WriteString("<!--esi ")
		input.WriteString(mode.includeTag(
			serverURL+"/chain/"+id,
			serverURL+"/chain-alt/"+id,
		))
		input.WriteString("-->")
	}
	return input.String()
}

// nestedSemaphoreServer serves the two-level chain and parks every leaf until
// wantLeaves leaves are present at once or the fallback timer fires. The
// fallback only unblocks the correct (capped) case; it is not part of the
// assertion. It never makes a broken cap pass: when cap+1 leaves can run
// together the barrier opens immediately and countingTransport has already
// observed the violation.
func nestedSemaphoreServer(t *testing.T, mode nestedFetchMode, wantLeaves int64) (serverURL string, transport *countingTransport) {
	t.Helper()

	var serverURLValue string
	var arrived atomic.Int64
	released := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(released) }) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/leaf") {
			if arrived.Add(1) >= wantLeaves {
				release()
			}
			select {
			case <-released:
			case <-time.After(time.Second):
			}
			_, _ = w.Write([]byte("leaf"))
			return
		}
		_, _ = w.Write([]byte(nestedIncludeBody(mode, serverURLValue)))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)

	serverURLValue = server.URL
	return server.URL, &countingTransport{rt: http.DefaultTransport}
}

func nestedSemaphoreConfig(serverURL string, transport *countingTransport) EsiParserConfig {
	config := CreateDefaultConfig()
	config.DefaultUrl = serverURL + "/"
	config.MaxDepth = 3
	config.BlockPrivateIPs = false
	config.HTTPClient = &http.Client{Transport: transport}
	return config
}

func TestNestedMESIParseRespectsMaxConcurrentRequests(t *testing.T) {
	for _, cap := range []int{1, 2} {
		for _, mode := range nestedFetchModes {
			t.Run(fmt.Sprintf("cap_%d_%s", cap, mode.name), func(t *testing.T) {
				serverURL, transport := nestedSemaphoreServer(t, mode, int64(cap+1))

				config := nestedSemaphoreConfig(serverURL, transport)
				config.MaxConcurrentRequests = cap

				result := MESIParse(nestedTopLevelInput(mode, serverURL), config)

				if got := transport.max.Load(); got > int64(cap) {
					t.Errorf("peak concurrent fetches = %d, want <= %d (MaxConcurrentRequests must be page-wide across nesting levels)", got, cap)
				}
				if got := transport.max.Load(); got < 1 {
					t.Errorf("peak concurrent fetches = %d, want the measurement to reach at least 1", got)
				}
				if !strings.Contains(result, "leaf") {
					t.Errorf("nested includes were not rendered, got %q", result)
				}
			})
		}
	}
}

func TestNestedMESIParseUnlimitedWhenMaxConcurrentRequestsZero(t *testing.T) {
	for _, mode := range nestedFetchModes {
		for _, explicit := range []bool{false, true} {
			name := fmt.Sprintf("%s_unset", mode.name)
			if explicit {
				name = fmt.Sprintf("%s_explicit_zero", mode.name)
			}
			t.Run(name, func(t *testing.T) {
				const wantLeaves = int64(4)
				serverURL, transport := nestedSemaphoreServer(t, mode, wantLeaves)

				config := nestedSemaphoreConfig(serverURL, transport)
				if explicit {
					config.MaxConcurrentRequests = 0
				}

				done := make(chan string, 1)
				go func() {
					done <- MESIParse(nestedTopLevelInput(mode, serverURL), config)
				}()

				select {
				case result := <-done:
					if got := transport.max.Load(); got < wantLeaves {
						t.Errorf("peak concurrent fetches = %d, want at least %d with MaxConcurrentRequests=0 (unlimited)", got, wantLeaves)
					}
					if !strings.Contains(result, "leaf") {
						t.Errorf("nested includes were not rendered, got %q", result)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("nested render did not finish; MaxConcurrentRequests=0 is not behaving as unlimited")
				}
			})
		}
	}
}

// TestMESIParseKeepsPreinstalledSemaphore pins the nil guard: a caller that
// already owns a semaphore must have it used rather than replaced. The only
// slot is occupied, so the include has to wait for it and fail on the fetch
// budget. A fresh semaphore would let the request through.
func TestMESIParseKeepsPreinstalledSemaphore(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.MaxConcurrentRequests = 1
	config.Timeout = 200 * time.Millisecond
	config.IncludeErrorMarker = "ESI_ERROR"

	semaphore := make(chan struct{}, 1)
	semaphore <- struct{}{}
	config = config.setSemaphore(semaphore)

	result := MESIParse(`<!--esi <esi:include src="`+server.URL+`/page"/>-->`, config)

	if got := strings.TrimSpace(result); got != "ESI_ERROR" {
		t.Errorf("result = %q, want %q: a pre-installed full semaphore must be honoured, not replaced", got, "ESI_ERROR")
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("upstream requests = %d, want 0 while the caller's only semaphore slot is occupied", got)
	}
	if config.getSemaphore() != semaphore {
		t.Errorf("caller's semaphore was replaced")
	}
	if len(semaphore) != 1 {
		t.Errorf("caller's semaphore length = %d, want 1 (unchanged)", len(semaphore))
	}
}

// TestNestedMESIParseLeavesParentSemaphoreUnchanged verifies that a nested
// render neither replaces nor leaks slots from the semaphore it inherited: the
// caller's channel stays the same, drains back to zero and still caps a later
// render.
func TestNestedMESIParseLeavesParentSemaphoreUnchanged(t *testing.T) {
	mode := nestedFetchModes[0]
	serverURL, transport := nestedSemaphoreServer(t, mode, 1)

	config := nestedSemaphoreConfig(serverURL, transport)
	config.MaxConcurrentRequests = 2
	semaphore := make(chan struct{}, 2)
	config = config.setSemaphore(semaphore)

	input := nestedTopLevelInput(mode, serverURL)
	if result := MESIParse(input, config); !strings.Contains(result, "leaf") {
		t.Fatalf("nested includes were not rendered, got %q", result)
	}

	if config.getSemaphore() != semaphore {
		t.Errorf("caller's semaphore was replaced by the nested parse")
	}
	if got := cap(semaphore); got != 2 {
		t.Errorf("semaphore capacity = %d, want 2", got)
	}
	if got := len(semaphore); got != 0 {
		t.Errorf("semaphore length after render = %d, want 0 (every slot released)", got)
	}

	// The same channel must still be usable for a second render.
	if result := MESIParse(input, config); !strings.Contains(result, "leaf") {
		t.Errorf("second render with the same semaphore failed, got %q", result)
	}
	if got := len(semaphore); got != 0 {
		t.Errorf("semaphore length after second render = %d, want 0", got)
	}
}
