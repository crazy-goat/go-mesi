package traefik

import (
	"context"
	"fmt"
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
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

	customWriter := middleware.NewResponseWriter(rw)

	_, ok := req.Header["Surrogate-Capability"]
	if ok == false {
		req.Header.Set("Surrogate-Capability", "ESI/1.0")
	}

	// Call the next handler
	p.next.ServeHTTP(customWriter, req)

	contentType := customWriter.Header().Get("Content-Type")

	if strings.HasPrefix(contentType, "text/html") {
		processedResponse := mesi.Parse(
			customWriter.Body().String(),
			p.config.MaxDepth,
			req.URL.Scheme+"://"+req.URL.Host,
		)
		rw.Header().Set("Content-Length", strconv.Itoa(len(processedResponse)))
		for k, v := range customWriter.Header() {
			rw.Header()[k] = v
		}
		rw.WriteHeader(customWriter.StatusCode())

		// Write the processed response
		rw.Write([]byte(processedResponse))

		return
	}

	rw.Write(customWriter.Body().Bytes())
}
