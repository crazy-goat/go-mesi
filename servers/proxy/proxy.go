package main

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/crazy-goat/go-mesi/mesi"
	"github.com/crazy-goat/go-mesi/middleware"
)

type Proxy struct {
	backend string
	config  mesi.EsiParserConfig
}

func NewProxy(backend string, config mesi.EsiParserConfig) *Proxy {
	return &Proxy{
		backend: backend,
		config:  config,
	}
}

func (p *Proxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	p.config.DefaultUrl = scheme + "://" + req.Host

	backendURL, _ := url.Parse(p.backend)
	proxy := httputil.NewSingleHostReverseProxy(backendURL)
	proxy.Transport = &http.Transport{}

	customWriter := middleware.NewResponseWriter(rw)

	_, hasSurrogate := req.Header["Surrogate-Capability"]
	if !hasSurrogate {
		req.Header.Set("Surrogate-Capability", "ESI/1.0")
	}

	proxy.ServeHTTP(customWriter, req)

	contentType := customWriter.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		for k, v := range customWriter.Header() {
			rw.Header()[k] = v
		}
		rw.WriteHeader(customWriter.StatusCode())
		rw.Write(customWriter.Body().Bytes())
		return
	}

	if p.config.ParseOnHeader {
		edgeControl := customWriter.Header().Get("Edge-control")
		if edgeControl != "dca=esi" {
			for k, v := range customWriter.Header() {
				rw.Header()[k] = v
			}
			rw.WriteHeader(customWriter.StatusCode())
			rw.Write(customWriter.Body().Bytes())
			return
		}
	}

	ctx, cancel := context.WithTimeout(req.Context(), p.config.Timeout)
	defer cancel()

	config := p.config
	config.Context = ctx

	processed := mesi.MESIParse(customWriter.Body().String(), config)

	for k, v := range customWriter.Header() {
		rw.Header()[k] = v
	}
	rw.Header().Set("Surrogate-Capability", "ESI/1.0")
	rw.Header().Set("Content-Length", strconv.Itoa(len(processed)))
	rw.WriteHeader(customWriter.StatusCode())
	rw.Write([]byte(processed))
}
