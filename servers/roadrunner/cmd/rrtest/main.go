package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crazy-goat/go-mesi/servers/roadrunner"
)

func main() {
	listen := flag.String("listen", ":8080", "Listen address")
	allowedHosts := flag.String("allowed-hosts", "", "Comma-separated allowed hosts for <esi:include> (empty/unset = all hosts allowed)")
	blockPrivateIPs := flag.Bool("block-private-ips", true, "Block ESI includes to private/reserved IPs at dial time")
	allowPrivateIPsForAllowedHosts := flag.Bool("allow-private-ips-for-allowed-hosts", false, "Bypass the dial-time private-IP block for hosts listed in -allowed-hosts")
	maxDepth := flag.Int("max-depth", 5, "Maximum ESI nesting depth (0 = passthrough: no include fetched, tags stripped; unset = plugin default 5)")
	timeout := flag.String("timeout", "", "Per-include ESI fetch budget as a Go duration (unset = plugin default 10s)")
	maxResponseSize := flag.Int64("max-response-size", 0, "Maximum bytes per ESI include response (0 = unlimited)")
	maxConcurrentRequests := flag.Int("max-concurrent-requests", 0, "Maximum concurrent ESI include fetches (0 = unlimited)")
	maxWorkers := flag.Int("max-workers", 0, "Maximum token-processing workers per ESI parse (0 = library default)")
	flag.Parse()

	config := roadrunner.CreateConfig()
	if *allowedHosts != "" {
		config.AllowedHosts = strings.Split(*allowedHosts, ",")
	}
	if *timeout != "" {
		config.Timeout = *timeout
	}
	config.MaxResponseSize = *maxResponseSize
	config.MaxConcurrentRequests = *maxConcurrentRequests
	config.MaxWorkers = *maxWorkers
	config.BlockPrivateIPs = blockPrivateIPs
	config.AllowPrivateIPsForAllowedHosts = *allowPrivateIPsForAllowedHosts
	// Only override CreateConfig()'s default (5) when -max-depth is
	// explicitly passed; when omitted, reset to nil so the plugin's
	// Init() takes its genuine unset branch (nil → 5). `-max-depth 0`
	// stays the documented passthrough (Init keeps an explicit 0
	// verbatim).
	maxDepthSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "max-depth" {
			maxDepthSet = true
		}
	})
	if maxDepthSet {
		depth := *maxDepth
		config.MaxDepth = &depth
	} else {
		config.MaxDepth = nil
	}

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
	mux.HandleFunc("/bytes/{size}", func(w http.ResponseWriter, r *http.Request) {
		size, err := strconv.Atoi(r.PathValue("size"))
		if err != nil || size < 0 || size > 1048576 {
			http.Error(w, "invalid byte count", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", size)))
	})
	mux.HandleFunc("/bytespage/{size}", func(w http.ResponseWriter, r *http.Request) {
		size, err := strconv.Atoi(r.PathValue("size"))
		if err != nil || size < 0 || size > 1048576 {
			http.Error(w, "invalid byte count", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>BYTES-PAGE<esi:include src="http://127.0.0.1:9090/bytes/` + strconv.Itoa(size) + `" /></body></html>`))
	})
	// /hold and /track provide a deterministic peak-concurrency gauge for the
	// RoadRunner functional suite, analogous to servers/test-server's tracker.
	var tracker struct {
		sync.Mutex
		current int
		peak    int
	}
	mux.HandleFunc("/hold/{millis}/{label}", func(w http.ResponseWriter, r *http.Request) {
		millis, err := strconv.Atoi(r.PathValue("millis"))
		if err != nil || millis < 0 || millis > 10000 {
			http.Error(w, "invalid hold duration", http.StatusBadRequest)
			return
		}
		tracker.Lock()
		tracker.current++
		if tracker.current > tracker.peak {
			tracker.peak = tracker.current
		}
		tracker.Unlock()
		defer func() {
			tracker.Lock()
			tracker.current--
			tracker.Unlock()
		}()
		time.Sleep(time.Duration(millis) * time.Millisecond)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("HELD-FRAGMENT-" + r.PathValue("label")))
	})
	mux.HandleFunc("/track/{action}", func(w http.ResponseWriter, r *http.Request) {
		tracker.Lock()
		defer tracker.Unlock()
		switch r.PathValue("action") {
		case "reset":
			tracker.current = 0
			tracker.peak = 0
			_, _ = w.Write([]byte("OK"))
		case "max":
			_, _ = w.Write([]byte(strconv.Itoa(tracker.peak)))
		default:
			http.Error(w, "unknown tracking action", http.StatusNotFound)
		}
	})
	mux.HandleFunc("/holdpage/{n}/{millis}", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil || n < 0 || n > 100 {
			http.Error(w, "invalid include count", http.StatusBadRequest)
			return
		}
		millis, err := strconv.Atoi(r.PathValue("millis"))
		if err != nil || millis < 0 || millis > 10000 {
			http.Error(w, "invalid hold duration", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		var b strings.Builder
		b.WriteString("<html><body>HOLD-PAGE")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, `<esi:include src="http://127.0.0.1:9090/hold/%d/%d" />`, millis, i)
		}
		b.WriteString("HOLD-PAGE-END</body></html>")
		_, _ = w.Write([]byte(b.String()))
	})
	// /deep/{level} returns three jobs at every non-leaf level: one child
	// page plus two marker fragments. That makes a cap of 2 constrain each
	// parse pool while deterministic markers prove nested drains complete.
	mux.HandleFunc("/deep/{level}", func(w http.ResponseWriter, r *http.Request) {
		level, err := strconv.Atoi(r.PathValue("level"))
		if err != nil || level < 1 || level > 5 {
			http.Error(w, "invalid deep level", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		var body strings.Builder
		fmt.Fprintf(&body, "LEVEL-%d-START", level)
		if level > 1 {
			fmt.Fprintf(&body, `<esi:include src="http://127.0.0.1:9090/deep/%d" />`, level-1)
			for _, marker := range []string{"A", "B"} {
				fmt.Fprintf(&body, `<esi:include src="http://127.0.0.1:9090/deep-marker/%d-%s" />`, level, marker)
			}
		}
		fmt.Fprintf(&body, "LEVEL-%d-END", level)
		_, _ = w.Write([]byte(body.String()))
	})
	mux.HandleFunc("/deep-marker/{marker}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "LEVEL-MARKER-%s", r.PathValue("marker"))
	})
	mux.HandleFunc("/deep-page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><esi:include src="http://127.0.0.1:9090/deep/4" />AFTER-DEEP</body></html>`))
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
	// /nested-depth is a two-level nested include page for the max_depth
	// functional cases. Each level carries a per-level marker (same scheme
	// as servers/nginx/tests/nested_depth_{outer,inner}.txt) so a shallow
	// depth provably stops after the first level — a marker-less fixture
	// cannot distinguish "not fetched" from "fetched but empty" (#428).
	// Fragments are served as text/plain on purpose: the loopback fetch
	// re-enters THIS server's middleware, and a text/html fragment would be
	// processed again as a fresh top-level response at full depth,
	// defeating the caller's depth budget (nginx avoids the same trap by
	// serving its .txt fixtures from the backend, outside the filter).
	// The hostname is "127.0.0.1" so the cases need no DNS and run with
	// -block-private-ips=false like the allowed_hosts cases; the core still
	// recurses into plain-text fragments (ParseOnHeader is off).
	mux.HandleFunc("/nested-depth/inner", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("INNER-DEPTH-BODY"))
	})
	mux.HandleFunc("/nested-depth/outer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(`OUTER-DEPTH-BODY<esi:include src="http://127.0.0.1:9090/nested-depth/inner" />`))
	})
	mux.HandleFunc("/nested-depth", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><esi:include src="http://127.0.0.1:9090/nested-depth/outer" /></body></html>`))
	})
	mux.HandleFunc("/slow-fragment", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("SLOW_FRAGMENT"))
	})
	mux.HandleFunc("/timeout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><esi:include src="http://127.0.0.1:9090/slow-fragment" /></body></html>`))
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
