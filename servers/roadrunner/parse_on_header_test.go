package roadrunner

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newParseOnHeaderPlugin(t *testing.T, config *Config) *Plugin {
	t.Helper()
	block := false
	if config == nil {
		config = &Config{}
	}
	config.BlockPrivateIPs = &block
	p := &Plugin{config: config}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return p
}

func parseOnHeaderFragmentServer(t *testing.T, body, edgeControl string) (*httptest.Server, *int) {
	t.Helper()
	var calls int
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

func esiPage(src string) string {
	return `<html><body><esi:include src="` + src + `" /></body></html>`
}

func parseOnHeaderHandler(page, edgeControl string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if edgeControl != "" {
			w.Header().Set("Edge-control", edgeControl)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(page))
	})
}

func serveParseOnHeaderPage(t *testing.T, p *Plugin, page, edgeControl string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	p.Middleware(parseOnHeaderHandler(page, edgeControl)).ServeHTTP(
		rec, httptest.NewRequest("GET", "http://example.com/", nil))
	return rec
}

func TestParseOnHeaderAbsentAndFalseAlwaysParse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *Config
	}{
		{name: "absent", config: &Config{}},
		{name: "explicit_false", config: &Config{ParseOnHeader: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fragment, _ := parseOnHeaderFragmentServer(t, "FragmentText", "")
			p := newParseOnHeaderPlugin(t, tc.config)

			rec := serveParseOnHeaderPage(t, p, esiPage(fragment.URL), "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "FragmentText") {
				t.Errorf("HTML must always be parsed when parse_on_header is off, got %q", rec.Body.String())
			}
		})
	}
}

func TestParseOnHeaderEnabledWithHeaderParses(t *testing.T) {
	for _, header := range []string{"dca=esi", "DCA=ESI", "no-cache, dca=esi"} {
		t.Run(header, func(t *testing.T) {
			fragment, _ := parseOnHeaderFragmentServer(t, "FragmentText", "")
			p := newParseOnHeaderPlugin(t, &Config{ParseOnHeader: true})

			rec := serveParseOnHeaderPage(t, p, esiPage(fragment.URL), header)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "FragmentText") {
				t.Errorf("Edge-control %q must enable ESI processing, got %q", header, rec.Body.String())
			}
		})
	}
}

func TestParseOnHeaderEnabledWithoutHeaderPassesThrough(t *testing.T) {
	fragment, calls := parseOnHeaderFragmentServer(t, "FragmentText", "")
	p := newParseOnHeaderPlugin(t, &Config{ParseOnHeader: true})
	page := esiPage(fragment.URL)

	rec := serveParseOnHeaderPage(t, p, page, "")

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
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(len(page)); got != want {
		t.Errorf("Content-Length = %q, want %q", got, want)
	}
}

func TestParseOnHeaderEnabledNonHTMLUntouched(t *testing.T) {
	p := newParseOnHeaderPlugin(t, &Config{ParseOnHeader: true})
	body := `{"esi":"<esi:include src=\"/fragment\" />"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})

	rec := httptest.NewRecorder()
	p.Middleware(handler).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Errorf("non-HTML response changed: %d %q", rec.Code, rec.Body.String())
	}
}

func TestParseOnHeaderBodyLimitOnlyAppliesToSignalledPages(t *testing.T) {
	block := false
	p := &Plugin{config: &Config{
		ParseOnHeader:   true,
		BlockPrivateIPs: &block,
		MaxBodySize:     10,
		OnOversize:      "error",
	}}
	if err := p.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

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

	// Not signalled: the page is not parsed and therefore not limited.
	rec := httptest.NewRecorder()
	p.Middleware(handler("")).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != page {
		t.Errorf("an unparsed page must not be limited: %d %.20q", rec.Code, rec.Body.String())
	}

	// Signalled: the page would be parsed, so the limit applies.
	rec = httptest.NewRecorder()
	p.Middleware(handler("dca=esi")).ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("a signalled page over max_body_size must answer 502, got %d", rec.Code)
	}
}

func TestParseOnHeaderNestedFragmentFollowsHeader(t *testing.T) {
	for _, tc := range []struct {
		name        string
		edgeControl string
		wantNested  bool
	}{
		{name: "fragment signals ESI", edgeControl: "dca=esi", wantNested: true},
		{name: "fragment does not signal ESI", edgeControl: "", wantNested: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deepest, deepestCalls := parseOnHeaderFragmentServer(t, "DeepestText", "")
			middle, _ := parseOnHeaderFragmentServer(t, `<esi:include src="`+deepest.URL+`" />`, tc.edgeControl)
			p := newParseOnHeaderPlugin(t, &Config{ParseOnHeader: true})

			rec := serveParseOnHeaderPage(t, p, esiPage(middle.URL), "dca=esi")
			body := rec.Body.String()
			if tc.wantNested {
				if !strings.Contains(body, "DeepestText") {
					t.Errorf("a signalling fragment must be parsed, got %q", body)
				}
				return
			}
			if strings.Contains(body, "DeepestText") {
				t.Errorf("a non-signalling fragment must not be parsed, got %q", body)
			}
			if !strings.Contains(body, "esi:include") {
				t.Errorf("the nested include must stay untouched, got %q", body)
			}
			if *deepestCalls != 0 {
				t.Errorf("deepest server calls = %d, want 0", *deepestCalls)
			}
		})
	}
}
