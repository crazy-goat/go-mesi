package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
)

var (
	listen        = flag.String("listen", ":8080", "Listen address (default :8080)")
	backend       = flag.String("backend", "", "Upstream backend URL (required)")
	maxDepth      = flag.Uint("max-depth", 5, "Maximum recursion depth")
	timeout       = flag.Float64("timeout", 10.0, "Request timeout in seconds")
	parseOnHeader = flag.Bool("parse-on-header", false, "Only parse when Edge-control: dca=esi header is present")
	blockPrivate  = flag.Bool("block-private-ips", true, "Block private IP addresses")
	debug         = flag.Bool("debug", false, "Enable debug logging")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: mesi-proxy [options]\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *backend == "" {
		fmt.Fprintf(os.Stderr, "Error: --backend is required\n")
		os.Exit(1)
	}

	config := mesi.CreateDefaultConfig()
	config.MaxDepth = *maxDepth
	config.Timeout = time.Duration(*timeout * float64(time.Second))
	config.ParseOnHeader = *parseOnHeader
	config.BlockPrivateIPs = *blockPrivate
	config.Debug = *debug

	proxy, err := NewProxy(*backend, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating proxy: %v\n", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:         *listen,
		Handler:      proxy,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Starting ESI proxy server on %s", *listen)
		log.Printf("Backend: %s", *backend)
		log.Printf("Max depth: %d, Timeout: %.1fs, Parse on header: %v", *maxDepth, *timeout, *parseOnHeader)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}
