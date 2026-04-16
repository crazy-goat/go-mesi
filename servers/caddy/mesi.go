package caddy

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
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
		config := mesi.EsiParserConfig{
			Context:         r.Context(),
			MaxDepth:        5,
			DefaultUrl:      middleware.GetDefaultUrl(r),
			Timeout:         10 * time.Second,
			BlockPrivateIPs: true,
		}
		processedResponse := mesi.MESIParse(
			customWriter.Body().String(),
			config,
		)

		w.Header().Set("Content-Length", strconv.Itoa(len(processedResponse)))
		w.WriteHeader(customWriter.StatusCode())
		w.Write([]byte(processedResponse))
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(customWriter.Body().Len()))
		w.WriteHeader(customWriter.StatusCode())
		w.Write(customWriter.Body().Bytes())
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
