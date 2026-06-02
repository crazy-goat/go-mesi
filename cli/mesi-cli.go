package main

import (
	"flag"
	"fmt"
	"github.com/crazy-goat/go-mesi/mesi"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func isURL(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

func main() {
	defaultUrl := flag.String("default-url", "http://127.0.0.1/", "Default URL to parse")
	maxDepth := flag.Uint("max-depth", 5, "Maximum depth of parsing")
	timeout := flag.Float64("timeout", 10.0, "Request timeout duration in seconds")
	parseOnHeader := flag.Bool("parse-on-header", false, "Enable parsing on header")
	debug := flag.Bool("debug", false, "Enable debug logging")
	cacheBackend := flag.String("cacheBackend", "",
		"Cache backend for ESI includes: memory (default: off)")
	cacheSize := flag.Int("cacheSize", 10000,
		"Max cache entries for memory backend")
	cacheTTL := flag.Duration("cacheTTL", 0,
		"Cache TTL (e.g. 30s, 5m); 0 = no expiry")
	allowPrivateIPs := flag.Bool("allow-private-ips", false,
		"Allow ESI includes to private/reserved IP ranges (for local testing)")
	maxWorkers := flag.Int("max-workers", 0,
		"Max concurrent ESI include goroutines (0 = NumCPU*4)")

	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Error: Missing file|url path argument.")
		fmt.Println("Usage: go run main.go [flags] <file_path|url>")
		flag.PrintDefaults()
		return
	}

	config := mesi.CreateDefaultConfig()
	config.DefaultUrl = *defaultUrl
	config.MaxDepth = *maxDepth
	config.Timeout = time.Duration(*timeout * float64(time.Second))
	config.ParseOnHeader = *parseOnHeader
	config.Debug = *debug
	config.BlockPrivateIPs = !*allowPrivateIPs
	config.MaxWorkers = *maxWorkers

	switch *cacheBackend {
	case "":
	case "memory":
		config.Cache = mesi.NewMemoryCache(*cacheSize, *cacheTTL)
		config.CacheTTL = *cacheTTL
	default:
		log.Fatalf("unknown cache backend: %s", *cacheBackend)
	}

	pathOrUrl := args[0]
	var data string
	if isURL(pathOrUrl) {
		parsedURL, err := url.Parse(pathOrUrl)
		if err != nil {
			fmt.Println("Error parsing URL:", err)
			return
		}

		config.DefaultUrl = parsedURL.String()

		client := http.Client{
			Timeout: config.Timeout,
		}
		req, err := http.NewRequest("GET", pathOrUrl, nil)
		if err != nil {
			fmt.Println("Error creating request:", err)
			os.Exit(1)
		}
		req.Header.Set("Surrogate-Capability", "ESI/1.0")

		content, err := client.Do(req)
		if err != nil {
			fmt.Println("Error fetching url:", err)
			return
		}

		if !(mesi.IsEsiResponse(content) || config.ParseOnHeader == false) {
			fmt.Println("Error response missing Edge-control header:")
			return
		}

		body, err := io.ReadAll(content.Body)
		if err != nil {
			fmt.Println("Error reading response:", err)
			return
		}

		if content.StatusCode >= 400 {
			fmt.Println("Invalid status code:", content.StatusCode)
			return
		}

		data = string(body)
	} else {
		fileContent, err := os.ReadFile(pathOrUrl)
		if err != nil {
			fmt.Println("Error reading file:", err)
			return
		}

		data = string(fileContent)
	}

	fmt.Println(mesi.MESIParse(data, config))
}
