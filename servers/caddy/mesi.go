package caddy

import (
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
	"net/http"
	"strconv"
	"strings"
)

func init() {
	httpcaddyfile.RegisterHandlerDirective("mesi", parseCaddyfile)
	caddy.RegisterModule(MesiMiddleware{})
}

type MesiMiddleware struct{}

func (MesiMiddleware) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mesi",
		New: func() caddy.Module { return new(MesiMiddleware) },
	}
}

func (MesiMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	r.Header.Set("Surrogate-Capability", "ESI/1.0")

	customWriter := middleware.NewResponseWriter(w)

	err := next.ServeHTTP(customWriter, r)
	if err != nil {
		return err
	}

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

	return nil
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	mesi := new(MesiMiddleware)
	err := mesi.UnmarshalCaddyfile(h.Dispenser)
	if err != nil {
		return mesi, err
	}

	return mesi, err
}

func (m *MesiMiddleware) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	if !d.NextArg() {
		return nil
	}
	return nil
}
