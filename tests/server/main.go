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

	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("ListenAndServe(): %v", err)
	}
}
