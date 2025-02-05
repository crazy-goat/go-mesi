package main

import (
	"log"
	"net/http"
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

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(HtmlTemplate))
	})

	http.HandleFunc("/esi", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(HtmlIncludeTemplate))
	})

	log.Fatal(http.ListenAndServe(":80", nil))
}
