package middleware

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewResponseWriter(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	if rw.StatusCode() != http.StatusOK {
		t.Errorf("expected StatusCode %d, got %d", http.StatusOK, rw.StatusCode())
	}

	if rw.Body() == nil {
		t.Error("expected Body to be non-nil")
	}

	if rw.ResponseWriter != w {
		t.Error("expected ResponseWriter to be set to the original ResponseWriter")
	}
}

func TestResponseWriter_Write(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	data := []byte("hello world")
	n, err := rw.Write(data)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}

	if got := rw.Body().String(); got != "hello world" {
		t.Errorf("expected body %q, got %q", "hello world", got)
	}
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"StatusOK", http.StatusOK},
		{"StatusCreated", http.StatusCreated},
		{"StatusNotFound", http.StatusNotFound},
		{"StatusInternalServerError", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			rw := NewResponseWriter(w)

			rw.WriteHeader(tt.statusCode)

			if rw.StatusCode() != tt.statusCode {
				t.Errorf("expected StatusCode %d, got %d", tt.statusCode, rw.StatusCode())
			}
		})
	}
}

func TestResponseWriter_StatusCode_Default(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	if rw.StatusCode() != http.StatusOK {
		t.Errorf("expected default StatusCode %d, got %d", http.StatusOK, rw.StatusCode())
	}
}

// #535: a flush must not reach the real writer while the response is still
// buffered, otherwise net/http commits the headers with an implicit 200 and
// the status written later is ignored.
func TestResponseWriter_Flush_DoesNotForward(t *testing.T) {
	var flushed bool
	mockWriter := &mockFlusher{ResponseWriter: httptest.NewRecorder(), flushed: &flushed}
	rw := NewResponseWriter(mockWriter)

	rw.WriteHeader(http.StatusNotFound)
	_, _ = rw.Write([]byte("missing"))
	rw.Flush()

	if flushed {
		t.Error("Flush() must not be forwarded to the underlying ResponseWriter")
	}
	if rw.StatusCode() != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.StatusCode())
	}
	if rw.Body().String() != "missing" {
		t.Errorf("body = %q, want it kept in the buffer", rw.Body().String())
	}
}

func TestResponseWriter_Flush_KeepsStatusOfUnderlyingWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := NewResponseWriter(rec)

	rw.WriteHeader(http.StatusBadGateway)
	rw.Flush()
	if rec.Flushed {
		t.Error("underlying recorder was flushed")
	}

	rec.WriteHeader(rw.StatusCode())
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestResponseWriter_Flush_NoOp_WhenNotSupported(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	rw.Flush()
}

func TestResponseWriter_Hijack_Success(t *testing.T) {
	expectedConn := &mockConn{}
	expectedBuf := &bufio.ReadWriter{}
	mockWriter := &mockHijacker{conn: expectedConn, buf: expectedBuf}
	rw := NewResponseWriter(mockWriter)

	conn, buf, err := rw.Hijack()

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if conn != expectedConn {
		t.Errorf("expected conn %v, got %v", expectedConn, conn)
	}
	if buf != expectedBuf {
		t.Errorf("expected buf %v, got %v", expectedBuf, buf)
	}
}

func TestResponseWriter_Hijack_Error(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	_, _, err := rw.Hijack()
	if !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("expected http.ErrNotSupported, got %v", err)
	}
}

func TestResponseWriter_ImplementsInterfaces(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w)

	if _, ok := interface{}(rw).(http.Flusher); !ok {
		t.Error("ResponseWriter should implement http.Flusher")
	}

	if _, ok := interface{}(rw).(http.Hijacker); !ok {
		t.Error("ResponseWriter should implement http.Hijacker")
	}
}

type mockFlusher struct {
	http.ResponseWriter
	flushed *bool
}

func (m *mockFlusher) Flush() {
	*m.flushed = true
}

type mockHijacker struct {
	http.ResponseWriter
	conn net.Conn
	buf  *bufio.ReadWriter
}

func (m *mockHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return m.conn, m.buf, nil
}

type mockConn struct {
	net.Conn
}

func (m *mockConn) Close() error {
	return nil
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return 0, io.EOF
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func htmlWriter(rec *httptest.ResponseRecorder) *ResponseWriter {
	rec.Header().Set("Content-Type", "text/html; charset=utf-8")
	return NewResponseWriter(rec)
}

func TestResponseWriter_BodyLimit_Under(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := htmlWriter(rec)
	rw.SetBodyLimit(10, OversizePass, nil)

	_, _ = rw.Write([]byte("12345"))
	_, _ = rw.Write([]byte("67890"))

	if rw.HandleOversize() {
		t.Fatal("a body exactly at the limit must not be oversize")
	}
	if rw.Body().String() != "1234567890" {
		t.Errorf("body = %q, want the buffered body", rw.Body().String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("nothing may be sent to the client yet, got %q", rec.Body.String())
	}
}

func TestResponseWriter_BodyLimit_PassStreamed(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := htmlWriter(rec)
	var got []int64
	rw.SetBodyLimit(10, OversizePass, func(_ OnOversize, limit, size int64) { got = append(got, limit, size) })

	rw.WriteHeader(http.StatusTeapot)
	_, _ = rw.Write([]byte("123456"))
	n, err := rw.Write([]byte("7890ABC"))
	_, _ = rw.Write([]byte("DEF"))

	if n != 7 || err != nil {
		t.Errorf("Write = %d, %v; want 7, nil", n, err)
	}
	if !rw.HandleOversize() {
		t.Fatal("expected oversize")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "1234567890ABCDEF" {
		t.Errorf("body = %q, want the whole body unchanged", rec.Body.String())
	}
	if len(got) != 2 || got[0] != 10 || got[1] != 13 {
		t.Errorf("notify args = %v, want one call with limit 10 and size 13", got)
	}
}

func TestResponseWriter_BodyLimit_PassKnownLength(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := htmlWriter(rec)
	rec.Header().Set("Content-Length", "16")
	rw.SetBodyLimit(10, OversizePass, nil)

	rw.WriteHeader(http.StatusOK)
	if rec.Body.Len() != 0 || !rw.committed {
		t.Fatal("a known oversize length must be decided before any byte is buffered")
	}
	_, _ = rw.Write([]byte("1234567890ABCDEF"))

	if !rw.HandleOversize() {
		t.Fatal("expected oversize")
	}
	if rec.Body.String() != "1234567890ABCDEF" || rec.Header().Get("Content-Length") != "16" {
		t.Errorf("body %q, Content-Length %q: want unchanged", rec.Body.String(), rec.Header().Get("Content-Length"))
	}
}

func TestResponseWriter_BodyLimit_ErrorStreamed(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := htmlWriter(rec)
	rec.Header().Set("X-Upstream", "1")
	rw.SetBodyLimit(10, OversizeError, nil)

	_, _ = rw.Write([]byte("123456"))
	n, err := rw.Write([]byte("7890ABC"))

	if n != 7 || err != nil {
		t.Errorf("Write = %d, %v; want 7, nil (data dropped, no error)", n, err)
	}
	if rw.Body().Len() != 0 {
		t.Errorf("buffer must be released, has %d bytes", rw.Body().Len())
	}
	if !rw.HandleOversize() {
		t.Fatal("expected oversize")
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if rec.Header().Get("X-Upstream") != "" {
		t.Error("upstream headers must not leak into the 502")
	}
}

func TestResponseWriter_BodyLimit_ErrorKnownLength(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := htmlWriter(rec)
	rec.Header().Set("Content-Length", "99")
	rw.SetBodyLimit(10, OversizeError, nil)

	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write([]byte("whatever"))

	if !rw.HandleOversize() || rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Length") == "99" {
		t.Error("the upstream Content-Length must not stay on the 502")
	}
}

func TestResponseWriter_BodyLimit_IgnoresNonHTMLAndUnlimited(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rw := NewResponseWriter(rec)
	rw.SetBodyLimit(3, OversizeError, nil)
	_, _ = rw.Write([]byte("0123456789"))
	if rw.HandleOversize() || rw.Body().Len() != 10 {
		t.Error("non-HTML bodies are not limited")
	}

	rec = httptest.NewRecorder()
	rw = htmlWriter(rec)
	rw.SetBodyLimit(0, OversizeError, nil)
	_, _ = rw.Write([]byte("0123456789"))
	if rw.HandleOversize() || rw.Body().Len() != 10 {
		t.Error("0 means unlimited")
	}
}

func TestParseOnOversize(t *testing.T) {
	for in, want := range map[string]OnOversize{"": OversizePass, "pass": OversizePass, "error": OversizeError} {
		got, err := ParseOnOversize(in)
		if err != nil || got != want {
			t.Errorf("ParseOnOversize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"drop", "Pass", " pass", "error "} {
		if _, err := ParseOnOversize(in); err == nil {
			t.Errorf("ParseOnOversize(%q) must fail", in)
		}
	}
}

func TestResponseWriter_BodyLimit_EarlyHintsDoNotSkipTheLimit(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := NewResponseWriter(rec)
	rw.SetBodyLimit(10, OversizeError, nil)

	rw.WriteHeader(http.StatusEarlyHints) // before the real headers exist
	rec.Header().Set("Content-Type", "text/html")
	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write([]byte("0123456789ABC"))

	if rw.StatusCode() != http.StatusOK {
		t.Errorf("status = %d, a 1xx must not replace it", rw.StatusCode())
	}
	if !rw.HandleOversize() || rec.Code != http.StatusBadGateway {
		t.Errorf("the limit must still apply after a 103, got %d", rec.Code)
	}
}

func TestResponseWriter_BodyLimit_NoBodyStatusesAreNotCounted(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		rec := httptest.NewRecorder()
		rw := htmlWriter(rec)
		rec.Header().Set("Content-Length", "99")
		rw.SetBodyLimit(10, OversizeError, nil)
		rw.WriteHeader(status)
		if rw.HandleOversize() {
			t.Errorf("status %d has no body: Content-Length must be ignored", status)
		}
	}
}

func TestResponseWriter_BodyLimit_LimitOnlyIf(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := htmlWriter(rec)
	rw.SetBodyLimit(3, OversizeError, nil)
	rw.LimitOnlyIf(func(h http.Header) bool { return h.Get("Edge-control") == "dca=esi" })
	_, _ = rw.Write([]byte("0123456789"))
	if rw.HandleOversize() || rw.Body().Len() != 10 {
		t.Error("a response the filter rejects must not be limited")
	}

	rec = httptest.NewRecorder()
	rec.Header().Set("Edge-control", "dca=esi")
	rw = htmlWriter(rec)
	rw.SetBodyLimit(3, OversizeError, nil)
	rw.LimitOnlyIf(func(h http.Header) bool { return h.Get("Edge-control") == "dca=esi" })
	_, _ = rw.Write([]byte("0123456789"))
	if !rw.HandleOversize() {
		t.Error("a response the filter accepts must be limited")
	}
}
