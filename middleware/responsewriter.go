package middleware

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// OnOversize says what happens when the parent HTML body is over the size
// limit set with SetBodyLimit (#538).
type OnOversize string

const (
	// OversizePass sends the body to the client unchanged, without ESI
	// processing. It is the default.
	OversizePass OnOversize = "pass"
	// OversizeError answers 502 Bad Gateway instead of the body.
	OversizeError OnOversize = "error"
)

// ParseOnOversize validates a configured mode. An empty value is the
// default (pass); anything but "pass" and "error" is an error.
func ParseOnOversize(s string) (OnOversize, error) {
	switch OnOversize(s) {
	case "", OversizePass:
		return OversizePass, nil
	case OversizeError:
		return OversizeError, nil
	}
	return "", fmt.Errorf("invalid on_oversize %q: must be %q or %q", s, OversizePass, OversizeError)
}

type ResponseWriter struct {
	// ResponseWriter wraps an http.ResponseWriter to capture the response body.
	// Note: Write() buffers data internally and the integration writes the
	// final response itself, so Flush() is accepted but does nothing: there
	// is no true streaming (SSE, chunked transfer). Forwarding it would make
	// net/http send the headers with an implicit 200 before the buffered
	// status is written (#535).
	// For streaming responses, consider bypassing this wrapper or using a
	// different architecture that writes directly to the underlying writer.
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer

	// Parent body limit (#538). maxBody == 0 means unlimited.
	maxBody int64
	mode    OnOversize
	// notify is called once when the limit is crossed. size is the
	// Content-Length when it is known, otherwise the size reached so far.
	notify    func(mode OnOversize, limit, size int64)
	decided   bool // the first Write or WriteHeader has been looked at
	watch     bool // the body is HTML and a limit is set
	oversize  bool // the limit has been crossed
	committed bool // pass mode: status, headers and body go to the client
}

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}
}

// SetBodyLimit bounds the HTML body that is buffered. maxBody is in bytes,
// 0 means unlimited. Only text/html responses are counted, like the
// integrations only run ESI on those. A known Content-Length over the limit
// is decided at once, before any byte is buffered. In OversizePass mode the
// status, headers and body are then streamed to the client unchanged; in
// OversizeError mode the data is dropped (so memory stays bounded) and the
// integration must call HandleOversize to answer 502. notify may be nil.
func (rw *ResponseWriter) SetBodyLimit(maxBody int64, mode OnOversize, notify func(mode OnOversize, limit, size int64)) {
	rw.maxBody = maxBody
	rw.mode = mode
	rw.notify = notify
}

func (rw *ResponseWriter) decide() {
	if rw.decided {
		return
	}
	rw.decided = true
	if rw.maxBody <= 0 {
		return
	}
	if !strings.HasPrefix(rw.Header().Get("Content-Type"), "text/html") {
		return
	}
	rw.watch = true
	if cl := rw.Header().Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n > rw.maxBody {
			rw.trigger(n)
		}
	}
}

func (rw *ResponseWriter) trigger(size int64) {
	rw.oversize = true
	if rw.notify != nil {
		rw.notify(rw.mode, rw.maxBody, size)
	}
	if rw.mode == OversizePass {
		rw.committed = true
		rw.ResponseWriter.WriteHeader(rw.statusCode)
		if rw.body.Len() > 0 {
			_, _ = rw.ResponseWriter.Write(rw.body.Bytes())
		}
	}
	rw.body = &bytes.Buffer{}
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	rw.decide()
	if rw.committed {
		return rw.ResponseWriter.Write(b)
	}
	if rw.oversize {
		return len(b), nil
	}
	if rw.watch && int64(rw.body.Len())+int64(len(b)) > rw.maxBody {
		// trigger sends the part buffered so far to the client (pass mode).
		rw.trigger(int64(rw.body.Len()) + int64(len(b)))
		if rw.committed {
			return rw.ResponseWriter.Write(b)
		}
		return len(b), nil
	}
	return rw.body.Write(b)
}

func (rw *ResponseWriter) WriteHeader(statusCode int) {
	if rw.committed {
		return
	}
	rw.statusCode = statusCode
	rw.decide()
}

// HandleOversize reports whether the response is already complete because
// the body limit was crossed. The integration calls it after the wrapped
// handler returned and stops when it returns true. In OversizeError mode it
// answers 502 Bad Gateway here; in OversizePass mode the body has been sent
// already.
func (rw *ResponseWriter) HandleOversize() bool {
	if !rw.oversize {
		return false
	}
	if rw.mode == OversizeError {
		h := rw.Header()
		for k := range h {
			delete(h, k)
		}
		http.Error(rw.ResponseWriter, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
	}
	return true
}

func (rw *ResponseWriter) StatusCode() int {
	return rw.statusCode
}

func (rw *ResponseWriter) Body() *bytes.Buffer {
	return rw.body
}

// Flush is a no-op. The response is held back until the integration has
// decided what to send, so the real writer must not see a flush (which
// commits the headers with an implicit 200) before that. The method stays
// so ResponseWriter keeps implementing http.Flusher, which handlers such as
// httputil.ReverseProxy look for.
func (rw *ResponseWriter) Flush() {
	// In pass mode the response is already being streamed (#538).
	if rw.committed {
		if f, ok := rw.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
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
