package roadrunner

import (
	"bytes"
	"fmt"
	"github.com/crazy-goat/go-mesi/mesi"
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

		customWriter := &responseWriter{
			ResponseWriter: w,
			body:           &bytes.Buffer{},
		}

		next.ServeHTTP(customWriter, r)

		//fmt.Println(mesi.ESI_CHOOSE)

		contentType := customWriter.Header().Get("Content-Type")
		if strings.HasPrefix(contentType, "text/html") {
			processedResponse := mesi.Parse(
				customWriter.body.String(),
				5,
				r.URL.Scheme+"://"+r.URL.Host,
			)

			w.Header().Set("Content-Length", strconv.Itoa(len(processedResponse)))
			fmt.Println(strconv.Itoa(len(processedResponse)))
			for k, v := range customWriter.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(customWriter.statusCode)
			w.Write([]byte(processedResponse))
		}
	})
}

func (p *Plugin) Name() string {
	return PluginName
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
