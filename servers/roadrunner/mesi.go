package roadrunner

import (
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
	"net/http"
	"strconv"
	"strings"
)

const PluginName = "mesi"

type Plugin struct {
}

func (p *Plugin) Init() error {
	return nil
}

func (p *Plugin) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Surrogate-Capability", "ESI/1.0")

		customWriter := middleware.NewResponseWriter(w)

		next.ServeHTTP(customWriter, r)

		contentType := customWriter.Header().Get("Content-Type")
		if strings.HasPrefix(contentType, "text/html") {
			processedResponse := mesi.Parse(
				customWriter.Body().String(),
				5,
				r.URL.Scheme+"://"+r.URL.Host,
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
