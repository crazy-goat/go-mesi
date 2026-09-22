package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

func hello(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("Hello World"))
}

func statusCode(w http.ResponseWriter, r *http.Request) {
	code, _ := strconv.Atoi(r.PathValue("id"))
	w.WriteHeader(code)
	w.Write([]byte(http.StatusText(code)))
}

func sleep(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	timeout, _ := strconv.Atoi(r.PathValue("timeout"))
	index := r.PathValue("index")
	time.Sleep(time.Duration(timeout) * time.Second)
	w.Write([]byte(index + " Waited " + strconv.Itoa(timeout)))
}

func returnEsi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Header().Add("Edge-control", "dca=esi")
	slow := r.URL.Query().Get("slow")
	if slow != "" {
		w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:18080/sleep/" + slow + "/1\" />]"))
		return
	}
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:18080/hello\" />]"))
}

func returnEsiNoHeader(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:18080/hello\" />]"))
}

func recursive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Header().Add("Edge-control", "dca=esi")
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:18080/recursive\" />]"))
}

func returnString(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(r.PathValue("data")))
}

// bytesHandler serves an exactly-N-byte body, mirroring the
// `/bytes/<size>` generator in servers/apache/tests/server.py (#169):
// a marker string repeated and truncated to the requested size. Used by
// cli/test.sh to prove -max-response-size caps a single include fetch.
func bytesHandler(w http.ResponseWriter, r *http.Request) {
	size, _ := strconv.Atoi(r.PathValue("size"))
	if size < 0 {
		size = 0
	}
	w.Header().Set("Content-Type", "text/html")
	const marker = "MesiBytesPayload"
	body := make([]byte, 0, size)
	for len(body) < size {
		body = append(body, marker...)
	}
	w.Write(body[:size])
}

var (
	countersMu sync.Mutex
	counters   = map[string]int{}
)

func countHandler(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	countersMu.Lock()
	counters[name]++
	n := counters[name]
	countersMu.Unlock()
	w.Write([]byte(strconv.Itoa(n)))
}

// Peak-concurrency tracker for the CLI -max-concurrent-requests tests
// (#192), mirroring servers/apache/tests/server.py's /hold + /track
// endpoints (#170). holdHandler increments trackCurrent (guarded by
// trackMu) BEFORE sleeping, records trackPeak, sleeps, then decrements —
// so /track/max is the maximum number of /hold requests that had
// STARTED-but-not-finished at any one time: a deterministic observable
// instead of a wall-clock assertion. Only /hold touches these counters
// and each test uses a fresh set of distinct labels (the CLI's cache is
// off by default, so repeats would not be deduped anyway), so no other
// traffic can pollute the reading.
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
	srv := &http.Server{Addr: ":18080"}

	http.HandleFunc("/hello", hello)
	http.HandleFunc("/status/code/{id}", statusCode)
	http.HandleFunc("/sleep/{timeout}/{index}", sleep)
	http.HandleFunc("/returnEsi", returnEsi)
	http.HandleFunc("/returnNonEsiHeader", returnEsiNoHeader)
	http.HandleFunc("/recursive", recursive)
	http.HandleFunc("/returnString/{data}", returnString)
	http.HandleFunc("/bytes/{size}", bytesHandler)
	http.HandleFunc("/count/{name}", countHandler)
	http.HandleFunc("/hold/{millis}/{label}", holdHandler)
	http.HandleFunc("/track/{action}", trackHandler)

	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("ListenAndServe(): %v", err)
	}
}
