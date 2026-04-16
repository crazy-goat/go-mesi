package middleware

import (
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
