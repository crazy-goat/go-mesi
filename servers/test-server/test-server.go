package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
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

// slowPageHandler serves /slow/{millis}: an HTML page whose single
// <esi:include> targets the /hold/{millis} sleep endpoint above. It gives
// the timeout tests a page with a slow fragment (the root HtmlTemplate's
// include points at the fast /esi). The duration is validated exactly
// like holdHandler's, because it is interpolated into the include URL;
// strconv.Atoi + Itoi also normalises it, so only validated digits reach
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
	http.HandleFunc("/slow/{millis}", echoHeaders(slowPageHandler))
	http.HandleFunc("/track/{action}", trackHandler)

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
