package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
)

type Proxy struct {
	backend    string
	backendURL *url.URL
	config     mesi.EsiParserConfig
	transport  *http.Transport
	reverse    *httputil.ReverseProxy

	// Parent body limit (#538): 0 = unlimited.
	maxBodySize int64
	onOversize  middleware.OnOversize
}

// SetBodyLimit bounds the HTML body that is buffered for ESI processing.
// maxBody is in bytes (0 = unlimited); mode is "pass" (default, send the body
// unchanged and log a warning) or "error" (log an error and answer 502).
// Call it before the proxy serves requests.
func (p *Proxy) SetBodyLimit(maxBody int64, mode string) error {
	if maxBody < 0 {
		return fmt.Errorf("invalid max-body-size %d: must be non-negative (0 = unlimited)", maxBody)
	}
	m, err := middleware.ParseOnOversize(mode)
	if err != nil {
		return err
	}
	p.maxBodySize = maxBody
	p.onOversize = m
	return nil
}

func logOversize(mode middleware.OnOversize, limit, size int64) {
	if mode == middleware.OversizeError {
		log.Printf("mesi: parent body is over max-body-size %d (size %d); answering 502 (on-oversize error)", limit, size)
		return
	}
	log.Printf("mesi: parent body is over max-body-size %d (size %d); sending it unchanged without ESI processing (on-oversize pass)", limit, size)
}

func NewProxy(backend string, config mesi.EsiParserConfig) (*Proxy, error) {
	backendURL, err := url.Parse(backend)
	if err != nil {
		return nil, errors.New("invalid backend URL: " + err.Error())
	}

	transport := &http.Transport{
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	reverse := httputil.NewSingleHostReverseProxy(backendURL)
	reverse.Transport = transport

	return &Proxy{
		backend:    backend,
		backendURL: backendURL,
		config:     config,
		transport:  transport,
		reverse:    reverse,
	}, nil
}

func (p *Proxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	defaultUrl := middleware.GetDefaultUrl(req)

	customWriter := middleware.NewResponseWriter(rw)
	if req.Method != http.MethodHead { // a HEAD Content-Length has no body behind it
		customWriter.SetBodyLimit(p.maxBodySize, p.onOversize, logOversize)
		if p.config.ParseOnHeader {
			// Without the header the page is not parsed, so it is not buffered
			// for ESI either.
			customWriter.LimitOnlyIf(func(h http.Header) bool { return h.Get("Edge-control") == "dca=esi" })
		}
	}

	_, hasSurrogate := req.Header["Surrogate-Capability"]
	if !hasSurrogate {
		req.Header.Set("Surrogate-Capability", "ESI/1.0")
	}

	p.reverse.ServeHTTP(customWriter, req)

	if customWriter.HandleOversize() {
		return
	}

	contentType := customWriter.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		p.writeResponse(rw, customWriter)
		return
	}

	if p.config.ParseOnHeader {
		edgeControl := customWriter.Header().Get("Edge-control")
		if edgeControl != "dca=esi" {
			p.writeResponse(rw, customWriter)
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.config.Timeout)
	defer cancel()

	config := p.config
	config.Context = ctx
	config.DefaultUrl = defaultUrl

	processed := mesi.MESIParse(customWriter.Body().String(), config)

	for k, v := range customWriter.Header() {
		rw.Header()[k] = v
	}
	rw.Header().Set("Surrogate-Control", "ESI/1.0")
	rw.Header().Set("Content-Length", strconv.Itoa(len(processed)))
	rw.WriteHeader(customWriter.StatusCode())
	_, _ = rw.Write([]byte(processed))
}

func (p *Proxy) writeResponse(rw http.ResponseWriter, customWriter *middleware.ResponseWriter) {
	for k, v := range customWriter.Header() {
		rw.Header()[k] = v
	}
	rw.WriteHeader(customWriter.StatusCode())
	_, _ = rw.Write(customWriter.Body().Bytes())
}
