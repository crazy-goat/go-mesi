package roadrunner

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
)

const PluginName = "mesi"

type Plugin struct {
}

func (p *Plugin) Init() error {
	return nil
}

func getScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func getDefaultUrl(r *http.Request) string {
	scheme := getScheme(r)
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

func (p *Plugin) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Surrogate-Capability", "ESI/1.0")

		customWriter := middleware.NewResponseWriter(w)

		next.ServeHTTP(customWriter, r)

		contentType := customWriter.Header().Get("Content-Type")
		if strings.HasPrefix(contentType, "text/html") {
			config := mesi.EsiParserConfig{
				Context:         r.Context(),
				MaxDepth:        5,
				DefaultUrl:      getDefaultUrl(r),
				Timeout:         10 * time.Second,
				BlockPrivateIPs: true,
			}
			processedResponse := mesi.MESIParse(
				customWriter.Body().String(),
				config,
			)

			w.Header().Set("Content-Length", strconv.Itoa(len(processedResponse)))
			for k, v := range customWriter.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(customWriter.StatusCode())
			w.Write([]byte(processedResponse))
		}
	})
}

func (p *Plugin) Name() string {
	return PluginName
}
