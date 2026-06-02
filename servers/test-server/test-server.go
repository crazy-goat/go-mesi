package main

import (
	"log"
	"net/http"
	"os"
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

func main() {
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

	port := os.Getenv("MESI_TEST_SERVER_PORT")
	if port == "" {
		port = "80"
	}
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
