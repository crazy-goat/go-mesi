package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/crazy-goat/go-mesi/servers/roadrunner"
)

func main() {
	listen := flag.String("listen", ":8080", "Listen address")
	allowedHosts := flag.String("allowed-hosts", "", "Comma-separated allowed hosts for <esi:include> (empty/unset = all hosts allowed)")
	blockPrivateIPs := flag.Bool("block-private-ips", true, "Block ESI includes to private/reserved IPs at dial time")
	allowPrivateIPsForAllowedHosts := flag.Bool("allow-private-ips-for-allowed-hosts", false, "Bypass the dial-time private-IP block for hosts listed in -allowed-hosts")
	flag.Parse()

	config := roadrunner.CreateConfig()
	if *allowedHosts != "" {
		config.AllowedHosts = strings.Split(*allowedHosts, ",")
	}
	config.BlockPrivateIPs = blockPrivateIPs
	config.AllowPrivateIPsForAllowedHosts = *allowPrivateIPsForAllowedHosts

	plugin := roadrunner.NewWithConfig(config)
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
	// /fragment is the loopback include target; /allowed serves a page that
	// includes it via an absolute URL on the same listener. The hostname is
	// "127.0.0.1" so allowed_hosts functional cases need no DNS.
	mux.HandleFunc("/fragment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("FRAGMENT_OK"))
	})
	mux.HandleFunc("/allowed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><esi:include src="http://127.0.0.1:9090/fragment" /></body></html>`))
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
