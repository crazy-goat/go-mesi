package traefik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestParseOnHeaderDefaultsToFalse(t *testing.T) {
	if CreateConfig().ParseOnHeader {
		t.Error("ParseOnHeader must default to false")
	}
}

func parseOnHeaderFragment(t *testing.T, body string, edgeControl string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/html")
		if edgeControl != "" {
			w.Header().Set("Edge-control", edgeControl)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func parseOnHeaderPage(src string) string {
	return `<html><body><esi:include src="` + src + `" /><esi:remove>no fragment</esi:remove></body></html>`
}

func serveParseOnHeaderPage(t *testing.T, parseOnHeader bool, page string, edgeControl string) *httptest.ResponseRecorder {
	t.Helper()
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Upstream", "kept")
		if edgeControl != "" {
			w.Header().Set("Edge-control", edgeControl)
		}
		_, _ = w.Write([]byte(page))
	})
	p, err := New(context.Background(), upstream, &Config{ParseOnHeader: parseOnHeader}, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	return rec
}

func TestParseOnHeaderDisabledAlwaysParses(t *testing.T) {
	fragment, calls := parseOnHeaderFragment(t, "FragmentText", "")
	page := parseOnHeaderPage(fragment.URL)

	rec := serveParseOnHeaderPage(t, false, page, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FragmentText") {
		t.Errorf("without parseOnHeader HTML must always be parsed, got %q", rec.Body.String())
	}
	if *calls != 1 {
		t.Errorf("fragment server calls = %d, want 1", *calls)
	}
}

func TestParseOnHeaderEnabledWithHeaderParses(t *testing.T) {
	for _, header := range []string{"dca=esi", "DCA=ESI", "no-cache, dca=esi"} {
		t.Run(header, func(t *testing.T) {
			fragment, calls := parseOnHeaderFragment(t, "FragmentText", "")
			page := parseOnHeaderPage(fragment.URL)

			rec := serveParseOnHeaderPage(t, true, page, header)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "FragmentText") {
				t.Errorf("Edge-control %q must enable ESI processing, got %q", header, rec.Body.String())
			}
			if *calls != 1 {
				t.Errorf("fragment server calls = %d, want 1", *calls)
			}
		})
	}
}

func TestParseOnHeaderEnabledWithoutHeaderPassesThrough(t *testing.T) {
	fragment, calls := parseOnHeaderFragment(t, "FragmentText", "")
	page := parseOnHeaderPage(fragment.URL)

	rec := serveParseOnHeaderPage(t, true, page, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != page {
		t.Errorf("body must be unchanged, got %q", rec.Body.String())
	}
	if *calls != 0 {
		t.Errorf("fragment server calls = %d, want 0", *calls)
	}
	if got := rec.Header().Get("Surrogate-Control"); got != "" {
		t.Errorf("Surrogate-Control = %q, want none for an unparsed page", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}
	if got := rec.Header().Get("X-Upstream"); got != "kept" {
		t.Errorf("X-Upstream = %q, want the upstream header unchanged", got)
	}
}

func TestParseOnHeaderEnabledNonHTMLUntouched(t *testing.T) {
	for _, header := range []string{"", "dca=esi"} {
		t.Run(header, func(t *testing.T) {
			body := `{"esi":"<esi:include src=\"/fragment\" />"}`
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if header != "" {
					w.Header().Set("Edge-control", header)
				}
				_, _ = w.Write([]byte(body))
			})

			p, err := New(context.Background(), handler, &Config{ParseOnHeader: true}, "test")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))

			if rec.Code != http.StatusOK || rec.Body.String() != body {
				t.Errorf("non-HTML response changed: %d %q", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestParseOnHeaderBodyLimitOnlyAppliesToSignalledPages(t *testing.T) {
	page := strings.Repeat("x", 100)
	handler := func(edgeControl string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if edgeControl != "" {
				w.Header().Set("Edge-control", edgeControl)
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(page)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(page))
		})
	}
	for _, tc := range []struct {
		name        string
		edgeControl string
		wantStatus  int
		wantBody    string
	}{
		{name: "not signalled", edgeControl: "", wantStatus: http.StatusOK, wantBody: page},
		{name: "signalled", edgeControl: "dca=esi", wantStatus: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(context.Background(), handler(tc.edgeControl), &Config{
				ParseOnHeader: true,
				MaxBodySize:   10,
				OnOversize:    "error",
			}, "test")
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestParseOnHeaderPropagatesToFragments(t *testing.T) {
	deepest, deepestCalls := parseOnHeaderFragment(t, "DeepestText", "")
	// The middle fragment does not signal ESI, so its nested include must
	// stay raw even though the top-level page signalled it.
	middle, _ := parseOnHeaderFragment(t, `<esi:include src="`+deepest.URL+`" />`, "")

	page := parseOnHeaderPage(middle.URL)
	rec := serveParseOnHeaderPage(t, true, page, "dca=esi")
	body := rec.Body.String()

	if strings.Contains(body, "DeepestText") {
		t.Errorf("a non-signalling fragment must not be parsed, got %q", body)
	}
	if !strings.Contains(body, "esi:include") {
		t.Errorf("the nested include must stay untouched, got %q", body)
	}
	if *deepestCalls != 0 {
		t.Errorf("deepest server calls = %d, want 0", *deepestCalls)
	}
}
