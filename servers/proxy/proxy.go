package main

import (
	"context"
	"errors"
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

	_, hasSurrogate := req.Header["Surrogate-Capability"]
	if !hasSurrogate {
		req.Header.Set("Surrogate-Capability", "ESI/1.0")
	}

	p.reverse.ServeHTTP(customWriter, req)

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
	rw.Write([]byte(processed))
}

func (p *Proxy) writeResponse(rw http.ResponseWriter, customWriter *middleware.ResponseWriter) {
	for k, v := range customWriter.Header() {
		rw.Header()[k] = v
	}
	rw.WriteHeader(customWriter.StatusCode())
	rw.Write(customWriter.Body().Bytes())
}
