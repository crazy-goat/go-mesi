package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
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
	timeout, _ := strconv.Atoi(r.PathValue("timeout"))
	index := r.PathValue("index")
	time.Sleep(time.Duration(timeout) * time.Second)
	w.Write([]byte(index + " Waited " + strconv.Itoa(timeout)))
}

func returnEsi(w http.ResponseWriter, _ *http.Request) {
	w.Header().Add("Edge-control", "dca=esi")
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:8080/hello\" />]"))
}

func returnEsiNoHeader(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:8080/hello\" />]"))
}

func recursive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Add("Edge-control", "dca=esi")
	w.Write([]byte("included: [<esi:include src=\"http://127.0.0.1:8080/recursive\" />]"))
}

func main() {
	srv := &http.Server{Addr: ":8080"}

	http.HandleFunc("/hello", hello)
	http.HandleFunc("/status/code/{id}", statusCode)
	http.HandleFunc("/sleep/{timeout}/{index}", sleep)
	http.HandleFunc("/returnEsi", returnEsi)
	http.HandleFunc("/returnNonEsiHeader", returnEsiNoHeader)
	http.HandleFunc("/recursive", recursive)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("ListenAndServe(): %v", err)
	}
}
