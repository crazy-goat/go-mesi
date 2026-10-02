package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
)

// TestNonHTMLPassthroughKeepsUpstreamStatus pins #491: the non-HTML
// branch of ServeHTTP must forward the status the upstream wrote, not
// let the first Write answer with an implicit 200.
func TestNonHTMLPassthroughKeepsUpstreamStatus(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		contentType string
		location    string
		body        string
	}{
		{name: "json 404", status: http.StatusNotFound, contentType: "application/json", body: `{"error":"not found"}`},
		{name: "plain 502", status: http.StatusBadGateway, contentType: "text/plain; charset=utf-8", body: "Bad Gateway"},
		{name: "redirect 302", status: http.StatusFound, contentType: "text/plain", location: "/elsewhere", body: "Found"},
		{name: "redirect 301 without content type", status: http.StatusMovedPermanently, location: "/moved"},
		{name: "created 201", status: http.StatusCreated, contentType: "application/json", body: `{"id":1}`},
		{name: "no content 204", status: http.StatusNoContent},
		{name: "implicit 200", status: 0, contentType: "application/json", body: `{"ok":true}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				if tc.location != "" {
					w.Header().Set("Location", tc.location)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				if tc.body != "" {
					_, _ = w.Write([]byte(tc.body))
				}
			})

			p, err := New(context.Background(), handler, CreateConfig(), "test")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			want := tc.status
			if want == 0 {
				want = http.StatusOK
			}
			if rec.Code != want {
				t.Errorf("status = %d, want %d", rec.Code, want)
			}
			if got := rec.Header().Get("Location"); got != tc.location {
				t.Errorf("Location = %q, want %q", got, tc.location)
			}
			if got := rec.Body.String(); got != tc.body {
				t.Errorf("body = %q, want %q", got, tc.body)
			}
		})
	}
}

// TestNonHTMLPassthroughRedirectIsFollowed checks the #491 fix end to
// end: a client behind the middleware sees a real redirect, not a 200
// that carries a Location header.
func TestNonHTMLPassthroughRedirectIsFollowed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"arrived":true}`))
			return
		}
		// Not http.Redirect: it answers a GET with text/html, which
		// would take the HTML branch instead of the passthrough.
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Location", "/target")
		w.WriteHeader(http.StatusFound)
	})

	p, err := New(context.Background(), handler, CreateConfig(), "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	srv := httptest.NewServer(p)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/start")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.Request.URL.Path != "/target" {
		t.Errorf("final path = %q, want /target (redirect not followed)", resp.Request.URL.Path)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("final status = %d, want 200", resp.StatusCode)
	}
}

// TestHTMLBranchKeepsUpstreamStatus is the regression guard for the
// HTML branch, which already forwarded the status before #491.
func TestHTMLBranchKeepsUpstreamStatus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html><body>missing</body></html>"))
	})

	p, err := New(context.Background(), handler, CreateConfig(), "test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Body.String(); got != "<html><body>missing</body></html>" {
		t.Errorf("body = %q", got)
	}
}

// TestChunkedUpstreamKeepsStatus pins #535: httputil.ReverseProxy flushes
// chunked responses, and a Flush forwarded to the real writer used to send
// the headers with an implicit 200 before the buffered status was written.
func TestChunkedUpstreamKeepsStatus(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{name: "json 404", status: http.StatusNotFound, contentType: "application/json", body: `{"error":"not found"}`},
		{name: "plain 502", status: http.StatusBadGateway, contentType: "text/plain", body: "Bad Gateway"},
		{name: "html 404", status: http.StatusNotFound, contentType: "text/html", body: "<html>missing</html>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
				w.(http.Flusher).Flush() // no Content-Length: the response is chunked
			}))
			defer backend.Close()

			backendURL, err := url.Parse(backend.URL)
			if err != nil {
				t.Fatalf("parse backend URL: %v", err)
			}
			p, err := New(context.Background(), httputil.NewSingleHostReverseProxy(backendURL), CreateConfig(), "test")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
			if got := rec.Body.String(); got != tc.body {
				t.Errorf("body = %q, want %q", got, tc.body)
			}
		})
	}
}
