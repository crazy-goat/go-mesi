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
