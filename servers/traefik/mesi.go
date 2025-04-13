package traefik

import (
	"bytes"
	"context"
	"fmt"
	"github.com/crazy-goat/go-mesi/mesi"
	"net/http"
	"strconv"
	"strings"
)

type Config struct {
	MaxDepth int `json:"maxDepth" yaml:"maxDepth"`
}

func CreateConfig() *Config {
	return &Config{
		MaxDepth: 5,
	}
}

type ResponsePlugin struct {
	next   http.Handler
	name   string
	config *Config
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if config.MaxDepth == 0 {
		config.MaxDepth = 5
	}

	return &ResponsePlugin{
		next:   next,
		name:   name,
		config: config,
	}, nil
}

func (p *ResponsePlugin) ServeHTTP(rw http.ResponseWriter, req *http.Request) {

	// Create a custom response writer to capture the response
	customWriter := &responseWriter{
		ResponseWriter: rw,
		body:           &bytes.Buffer{},
	}

	_, ok := req.Header["Surrogate-Capability"]
	if ok == false {
		req.Header.Set("Surrogate-Capability", "ESI/1.0")
	}

	// Call the next handler
	p.next.ServeHTTP(customWriter, req)

	contentType := customWriter.Header().Get("Content-Type")

	if strings.HasPrefix(contentType, "text/html") {
		processedResponse := mesi.Parse(
			customWriter.body.String(),
			p.config.MaxDepth,
			req.URL.Scheme+"://"+req.URL.Host,
		)
		rw.Header().Set("Content-Length", strconv.Itoa(len(processedResponse)))
		for k, v := range customWriter.Header() {
			rw.Header()[k] = v
		}
		rw.WriteHeader(customWriter.statusCode)

		// Write the processed response
		rw.Write([]byte(processedResponse))

		return
	}

	rw.Write(customWriter.body.Bytes())
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func (rw *responseWriter) Write(b []byte) (int, error) {

	return rw.body.Write(b)
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
}
