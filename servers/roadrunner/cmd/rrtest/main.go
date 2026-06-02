package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/crazy-goat/go-mesi/servers/roadrunner"
)

func main() {
	listen := flag.String("listen", ":8080", "Listen address")
	flag.Parse()

	plugin := &roadrunner.Plugin{}
	if err := plugin.Init(); err != nil {
		log.Fatalf("Failed to initialize plugin: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <title>Test ESI</title>
</head>
<body>
<!--esi <h1>Welcome to ESI Test</h1> -->
<esi:remove><h1>Failed to include ESI</h1></esi:remove>
</body>
</html>`))
	})
	mux.HandleFunc("/plain", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(`plain text with <esi:include src="http://example.com/test" /> tags`))
	})

	handler := plugin.Middleware(mux)

	server := &http.Server{
		Addr:    *listen,
		Handler: handler,
	}

	go func() {
		log.Printf("Starting RR test server on %s", *listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	server.Close()
	log.Println("Server stopped")
}
