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
	caddy.RegisterModule(new(MesiMiddleware))
}

// Compile-time interface assertions
var (
	_ caddy.Provisioner  = (*MesiMiddleware)(nil)
	_ caddy.CleanerUpper = (*MesiMiddleware)(nil)
)

type MesiMiddleware struct {
	// SharedHTTPClient enables TCP connection reuse for ESI includes.
	// When true, a shared http.Transport with SSRF protection is created
	// in Provision() and reused for all requests. Without this, each
	// <esi:include> creates a fresh http.Client + http.Transport,
	// incurring N × TCP+TLS handshake overhead for multi-include pages.
	SharedHTTPClient bool `json:"shared_http_client,omitempty"`

	sharedTransport *http.Transport `json:"-"`
}

func (m *MesiMiddleware) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mesi",
		New: func() caddy.Module { return new(MesiMiddleware) },
	}
}

// Provision implements caddy.Provisioner. Called once at config load.
func (m *MesiMiddleware) Provision(ctx caddy.Context) error {
	if m.SharedHTTPClient {
		m.sharedTransport = mesi.NewSSRFSafeTransport(mesi.EsiParserConfig{
			BlockPrivateIPs: true,
		})
	}
	return nil
}

// Cleanup implements caddy.CleanerUpper. Closes idle connections on the
// shared transport during config reloads to prevent goroutine/resource leaks.
func (m *MesiMiddleware) Cleanup() error {
	if m.sharedTransport != nil {
		m.sharedTransport.CloseIdleConnections()
	}
	return nil
}

func (m *MesiMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
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

		if m.sharedTransport != nil {
			config.HTTPClient = &http.Client{
				Transport: m.sharedTransport,
				Timeout:   config.Timeout,
			}
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
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "shared_http_client":
				m.SharedHTTPClient = true
			default:
				return d.Errf("unrecognized directive: %s", d.Val())
			}
		}
	}
	return nil
}
