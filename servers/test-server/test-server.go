package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const HtmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <title>Test ESI</title>
</head>
<body>
<h1>Welcome to ESI Test</h1>
<esi:include src="http://test-server/esi" />
<esi:remove>Failed to include ESI</esi:remove>
</body>
</html>`

const HtmlIncludeTemplate = "Hurray: Esi included!"

const PlainTextTemplate = `plain text with <esi:include src="http://test-server/esi" /> tags`

func echoHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sc := r.Header.Get("Surrogate-Capability"); sc != "" {
			w.Header().Set("Surrogate-Capability", sc)
		}
		next(w, r)
	}
}

// Peak-concurrency tracker for the php-ext max_concurrent_requests tests
// (#206), mirroring tests/server/main.go's /hold + /track endpoints (#192)
// and servers/apache/tests/server.py's originals (#170). holdHandler
// increments trackCurrent (guarded by trackMu) BEFORE sleeping, records
// trackPeak, sleeps, then decrements — so /track/max is the maximum number
// of /hold requests that had STARTED-but-not-finished at any one time: a
// deterministic observable instead of a wall-clock assertion. Only /hold
// touches these counters and each test resets them before its parse, so no
// other traffic (/esi, /plain, ...) can pollute the reading.
var (
	trackMu      sync.Mutex
	trackCurrent int
	trackPeak    int
)

// holdHandler serves /hold/<millis>/<label>: it registers the request in
// the peak-concurrency tracker, holds for <millis>, then returns a
// "<label> Held <millis>" fragment body. Distinct label values give every
// <esi:include> of a page its own URL so all of them reach this counter.
func holdHandler(w http.ResponseWriter, r *http.Request) {
	millis, err := strconv.Atoi(r.PathValue("millis"))
	if err != nil || millis < 0 || millis > 60000 {
		http.Error(w, "invalid hold duration", http.StatusBadRequest)
		return
	}
	label := r.PathValue("label")
	trackMu.Lock()
	trackCurrent++
	if trackCurrent > trackPeak {
		trackPeak = trackCurrent
	}
	trackMu.Unlock()
	defer func() {
		trackMu.Lock()
		trackCurrent--
		trackMu.Unlock()
	}()
	time.Sleep(time.Duration(millis) * time.Millisecond)
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(label + " Held " + strconv.Itoa(millis)))
}

// maxGeneratedBytes caps /bytes/{size} (#210), mirroring
// MAX_GENERATED_BYTES in servers/nginx/tests/server.py (#208): the cap
// keeps a stray URL from allocating an unbounded body in this shared
// test backend.
const maxGeneratedBytes = 268435456 // 256 MB

// maxGeneratedIncludes caps /holdpage/{n} (#215): the include count
// is interpolated into the page markup, so a stray URL cannot make
// this shared test backend generate an unbounded page.
const maxGeneratedIncludes = 1000

// bytesHandler serves /bytes/{size} (#210): a body of exactly <size>
// bytes prefixed with a "MesiBytesPayload <size>" marker line when the
// size leaves room for it (mirrors the /bytes endpoint of
// servers/nginx/tests/server.py #208 and servers/apache's
// tests/server.py #169), so test.sh can prove the whole fragment
// arrived (grep marker + wc -c) or was rejected (marker absent). The
// size is validated like holdHandler's bounds because it is echoed in
// the marker — only validated digits reach the payload.
func bytesHandler(w http.ResponseWriter, r *http.Request) {
	size, err := strconv.Atoi(r.PathValue("size"))
	if err != nil || size < 0 || size > maxGeneratedBytes {
		http.Error(w, "invalid byte count", http.StatusBadRequest)
		return
	}
	body := make([]byte, size)
	for i := range body {
		body[i] = 'x'
	}
	if marker := []byte("MesiBytesPayload " + strconv.Itoa(size) + "\n"); size >= len(marker) {
		copy(body, marker)
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(body)
}

// bytesPageHandler serves /bytespage/{size} (#210): an HTML page whose
// single <esi:include> targets the /bytes/{size} endpoint above — the
// page fixture for the traefik maxResponseSize tests (the root
// HtmlTemplate's include points at the fast /esi). The size is
// validated exactly like bytesHandler's, because it is interpolated
// into the include URL; strconv.Atoi + Itoa normalises it, so only
// validated digits reach the markup.
func bytesPageHandler(w http.ResponseWriter, r *http.Request) {
	size, err := strconv.Atoi(r.PathValue("size"))
	if err != nil || size < 0 || size > maxGeneratedBytes {
		http.Error(w, "invalid byte count", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <title>Bytes ESI</title>
</head>
<body>
<h1>BYTES-PAGE</h1>
<esi:include src="http://test-server/bytes/` + strconv.Itoa(size) + `" />
<esi:remove>Failed to include ESI</esi:remove>
<p>After bytes include</p>
</body>
</html>`))
}

// slowPageHandler serves /slow/{millis}: an HTML page whose single
// <esi:include> targets the /hold/{millis} sleep endpoint above. It gives
// the timeout tests a page with a slow fragment (the root HtmlTemplate's
// include points at the fast /esi). The duration is validated exactly
// like holdHandler's, because it is interpolated into the include URL;
// strconv.Atoi + Itoa also normalises it, so only validated digits reach
// the markup.
func slowPageHandler(w http.ResponseWriter, r *http.Request) {
	millis, err := strconv.Atoi(r.PathValue("millis"))
	if err != nil || millis < 0 || millis > 60000 {
		http.Error(w, "invalid hold duration", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <title>Slow ESI</title>
</head>
<body>
<h1>SLOW-PAGE</h1>
<esi:include src="http://test-server/hold/` + strconv.Itoa(millis) + `/SLOW-FRAG" />
<esi:remove>Failed to include ESI</esi:remove>
</body>
</html>`))
}

// holdPageHandler serves /holdpage/{n}/{millis} (#215): an HTML page
// whose n <esi:include> tags target /hold/{millis}/HOLDFRAG-{i} — the
// page fixture for the maxConcurrentRequests funnel tests (the root
// HtmlTemplate's include points at the fast /esi). Each include gets a
// DISTINCT label/URL so every one of them reaches the peak-concurrency
// counter (the same reason nginx's #214 fixtures own disjoint label
// ranges). Both params are validated exactly like bytesPageHandler's /
// slowPageHandler's, because both are interpolated into the include
// URLs; strconv.Atoi + Itoa normalises them, so only validated digits
// reach the markup.
//
// deepPageHandler serves /deeppage/{depth}/{cap}: an ESI page with a
// deterministic nested include chain. The depth and cap are validated
// before interpolation, so malformed values cannot create invalid include
// URLs or unbounded markup. The fixture mirrors four-level nesting coverage
// in nginx #219 and Apache #171, making MaxWorkers' drain correctness
// observable without timing assumptions.
func deepPageHandler(w http.ResponseWriter, r *http.Request) {
	depth, err := strconv.Atoi(r.PathValue("depth"))
	if err != nil || depth < 1 || depth > 8 {
		http.Error(w, "invalid nesting depth", http.StatusBadRequest)
		return
	}
	capValue, err := strconv.Atoi(r.PathValue("cap"))
	if err != nil || capValue < 1 || capValue > 999999999 {
		http.Error(w, "invalid worker cap", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html><html><body><h1>DEEP-PAGE</h1><esi:include src="http://test-server/deep/` + strconv.Itoa(depth) + `/` + strconv.Itoa(capValue) + `" /><p>AFTER-DEEP</p></body></html>`))
}

// deepHandler serves /deep/{level}/{cap}. Each fragment is itself HTML and
// at levels above one includes the next level plus two sibling marker
// fragments. The three jobs per level exercise the worker cap during every
// recursive drain while uniquely-labelled markers prove all jobs completed.
func deepHandler(w http.ResponseWriter, r *http.Request) {
	level, err := strconv.Atoi(r.PathValue("level"))
	if err != nil || level < 1 || level > 8 {
		http.Error(w, "invalid nesting level", http.StatusBadRequest)
		return
	}
	capValue, err := strconv.Atoi(r.PathValue("cap"))
	if err != nil || capValue < 1 || capValue > 999999999 {
		http.Error(w, "invalid worker cap", http.StatusBadRequest)
		return
	}
	levelText := strconv.Itoa(level)
	body := `<section>LEVEL-` + levelText + `-START</section>`
	if level > 1 {
		childLevel := strconv.Itoa(level - 1)
		body += `<esi:include src="http://test-server/deep/` + childLevel + `/` + strconv.Itoa(capValue) + `" />`
		body += `<esi:include src="http://test-server/marker/` + levelText + `/A" />`
		body += `<esi:include src="http://test-server/marker/` + levelText + `/B" />`
	}
	body += `<section>LEVEL-` + levelText + `-END</section>`
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(body))
}

// deepMarkerHandler serves stable HTML markers as distinct sibling jobs so
// each recursive level has three ESI jobs and can exercise MaxWorkers=2.
func deepMarkerHandler(w http.ResponseWriter, r *http.Request) {
	level, err := strconv.Atoi(r.PathValue("level"))
	if err != nil || level < 2 || level > 8 {
		http.Error(w, "invalid marker level", http.StatusBadRequest)
		return
	}
	marker := r.PathValue("marker")
	if marker != "A" && marker != "B" {
		http.Error(w, "invalid marker", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<section>LEVEL-` + strconv.Itoa(level) + `-MARKER-` + marker + `</section>`))
}

func holdPageHandler(w http.ResponseWriter, r *http.Request) {
	count, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || count < 0 || count > maxGeneratedIncludes {
		http.Error(w, "invalid include count", http.StatusBadRequest)
		return
	}
	millis, err := strconv.Atoi(r.PathValue("millis"))
	if err != nil || millis < 0 || millis > 60000 {
		http.Error(w, "invalid hold duration", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <title>Hold ESI</title>
</head>
<body>
<h1>HOLD-PAGE</h1>`)
	for i := 0; i < count; i++ {
		b.WriteString(`<esi:include src="http://test-server/hold/` + strconv.Itoa(millis) + `/HOLDFRAG-` + strconv.Itoa(i) + `" />`)
	}
	b.WriteString(`
<esi:remove>Failed to include ESI</esi:remove>
<p>After hold include</p>
</body>
</html>`)
	w.Write([]byte(b.String()))
}

// trackHandler serves /track/reset (zero both counters) and /track/max
// (the recorded peak) as text/plain control endpoints.
func trackHandler(w http.ResponseWriter, r *http.Request) {
	switch r.PathValue("action") {
	case "reset":
		trackMu.Lock()
		trackCurrent = 0
		trackPeak = 0
		trackMu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("reset"))
	case "max":
		trackMu.Lock()
		peak := strconv.Itoa(trackPeak)
		trackMu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(peak))
	default:
		http.NotFound(w, r)
	}
}

func main() {
	port := os.Getenv("MESI_TEST_SERVER_PORT")
	if port == "" {
		port = "80"
	}

	http.HandleFunc("/", echoHeaders(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(HtmlTemplate))
	}))

	http.HandleFunc("/esi", echoHeaders(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(HtmlIncludeTemplate))
	}))

	http.HandleFunc("/plain", echoHeaders(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(PlainTextTemplate))
	}))

	http.HandleFunc("/hold/{millis}/{label}", holdHandler)
	http.HandleFunc("/bytes/{size}", bytesHandler)
	http.HandleFunc("/bytespage/{size}", echoHeaders(bytesPageHandler))
	http.HandleFunc("/slow/{millis}", echoHeaders(slowPageHandler))
	http.HandleFunc("/holdpage/{n}/{millis}", echoHeaders(holdPageHandler))
	http.HandleFunc("/deeppage/{depth}/{cap}", echoHeaders(deepPageHandler))
	http.HandleFunc("/deep/{level}/{cap}", deepHandler)
	http.HandleFunc("/marker/{level}/{marker}", deepMarkerHandler)
	http.HandleFunc("/track/{action}", trackHandler)

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
