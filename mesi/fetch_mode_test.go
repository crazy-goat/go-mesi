package mesi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchFallbackPrimaryFailsThenAlt(t *testing.T) {
	primaryCalled := false
	altCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			primaryCalled = true
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/alt" {
			altCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("alt content"))
			return
		}
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src: server.URL + "/primary",
		Alt: server.URL + "/alt",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	data, _, err := fetchFallback(token, config)

	if !primaryCalled {
		t.Error("primary URL was not called")
	}
	if !altCalled {
		t.Error("alt URL was not called after primary failed")
	}
	if err != nil {
		t.Errorf("fetchFallback() error = %v", err)
	}
	if data != "alt content" {
		t.Errorf("data = %q, want %q", data, "alt content")
	}
}

func TestFetchConcurrentHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content from " + r.URL.Path))
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src:       server.URL + "/src",
		Alt:       server.URL + "/alt",
		FetchMode: "concurrent",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	data, _, err := fetchConcurrent(token, config)

	if err != nil {
		t.Errorf("fetchConcurrent() error = %v", err)
	}
	if data == "" {
		t.Error("fetchConcurrent() returned empty data")
	}
}
