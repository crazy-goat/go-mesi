package middleware

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
)

type ResponseWriter struct {
	// ResponseWriter wraps an http.ResponseWriter to capture the response body.
	// Note: Write() buffers data internally, so Flush() only works for HTTP/2
	// compatibility but not for true streaming (SSE, chunked transfer).
	// For streaming responses, consider bypassing this wrapper or using a
	// different architecture that writes directly to the underlying writer.
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
}

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	return rw.body.Write(b)
}

func (rw *ResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
}

func (rw *ResponseWriter) StatusCode() int {
	return rw.statusCode
}

func (rw *ResponseWriter) Body() *bytes.Buffer {
	return rw.body
}

func (rw *ResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *ResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func GetScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func GetDefaultUrl(r *http.Request) string {
	scheme := GetScheme(r)
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}
