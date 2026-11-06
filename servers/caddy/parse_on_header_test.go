package caddy

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func TestParseOnHeaderCaddyfile(t *testing.T) {
	m := &MesiMiddleware{}
	if err := m.UnmarshalCaddyfile(caddyfile.NewTestDispenser("mesi {\n\tparse_on_header\n}")); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if !m.ParseOnHeader {
		t.Error("ParseOnHeader should be true after parsing parse_on_header")
	}

	absent := &MesiMiddleware{}
	if err := absent.UnmarshalCaddyfile(caddyfile.NewTestDispenser("mesi")); err != nil {
		t.Fatalf("UnmarshalCaddyfile: %v", err)
	}
	if absent.ParseOnHeader {
		t.Error("ParseOnHeader should be false when the directive is absent")
	}
}

func newParseOnHeaderModule(t *testing.T, parseOnHeader bool) *MesiMiddleware {
	t.Helper()
	blockPrivateIPs := false
	m := &MesiMiddleware{ParseOnHeader: parseOnHeader, BlockPrivateIPs: &blockPrivateIPs}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	return m
}

func fragmentServer(t *testing.T, body string, edgeControl string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if edgeControl != "" {
			w.Header().Set("Edge-Control", edgeControl)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func esiPage(src string) string {
	return `<html><body><esi:include src="` + src + `" /></body></html>`
}

func pageHandler(page string, edgeControl string) caddyhttp.Handler {
	return caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/html")
		if edgeControl != "" {
			w.Header().Set("Edge-Control", edgeControl)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(page))
		return nil
	})
}

func servePage(t *testing.T, m *MesiMiddleware, page string, edgeControl string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := m.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil), pageHandler(page, edgeControl)); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	return rec
}

func TestParseOnHeaderDisabledAlwaysParses(t *testing.T) {
	fragment := fragmentServer(t, "FragmentText", "")
	m := newParseOnHeaderModule(t, false)

	rec := servePage(t, m, esiPage(fragment.URL), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FragmentText") {
		t.Errorf("without parse_on_header HTML must always be parsed, got %q", rec.Body.String())
	}
}

func TestParseOnHeaderEnabledWithHeaderParses(t *testing.T) {
	for _, header := range []string{"dca=esi", "DCA=ESI", "no-cache, dca=esi"} {
		t.Run(header, func(t *testing.T) {
			fragment := fragmentServer(t, "FragmentText", "")
			m := newParseOnHeaderModule(t, true)

			rec := servePage(t, m, esiPage(fragment.URL), header)
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
	var calls int
	fragment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte("FragmentText"))
	}))
	t.Cleanup(fragment.Close)

	m := newParseOnHeaderModule(t, true)
	page := esiPage(fragment.URL)
	rec := servePage(t, m, page, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != page {
		t.Errorf("body must be unchanged, got %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "FragmentText") {
		t.Error("the include must not be fetched")
	}
	if calls != 0 {
		t.Errorf("fragment server calls = %d, want 0", calls)
	}
	if got := rec.Header().Get("Surrogate-Control"); got != "" {
		t.Errorf("Surrogate-Control = %q, want none for an unparsed page", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}
}

func TestParseOnHeaderEnabledNonHTMLUntouched(t *testing.T) {
	m := newParseOnHeaderModule(t, true)
	body := `{"esi":"<esi:include src=\"/fragment\" />"}`
	handler := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
		return nil
	})

	rec := httptest.NewRecorder()
	if err := m.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil), handler); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Errorf("non-HTML response changed: %d %q", rec.Code, rec.Body.String())
	}
}

func TestParseOnHeaderBodyLimitOnlyAppliesToSignalledPages(t *testing.T) {
	blockPrivateIPs := false
	m := &MesiMiddleware{
		ParseOnHeader:   true,
		BlockPrivateIPs: &blockPrivateIPs,
		MaxBodySize:     10,
		OnOversize:      "error",
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	page := strings.Repeat("x", 100)
	handler := func(edgeControl string) caddyhttp.Handler {
		return caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
			w.Header().Set("Content-Type", "text/html")
			if edgeControl != "" {
				w.Header().Set("Edge-Control", edgeControl)
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(page)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(page))
			return nil
		})
	}

	// Not signalled: the page is not parsed and therefore not limited.
	rec := httptest.NewRecorder()
	if err := m.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil), handler("")); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != page {
		t.Errorf("an unparsed page must not be limited: %d %.20q", rec.Code, rec.Body.String())
	}

	// Signalled: the page would be parsed, so the limit applies.
	rec = httptest.NewRecorder()
	if err := m.ServeHTTP(rec, httptest.NewRequest("GET", "http://example.com/", nil), handler("dca=esi")); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("a signalled page over max_body_size must answer 502, got %d", rec.Code)
	}
}

func TestParseOnHeaderNestedFragmentFollowsHeader(t *testing.T) {
	var deepestCalls int
	deepest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deepestCalls++
		_, _ = w.Write([]byte("DeepestText"))
	}))
	t.Cleanup(deepest.Close)

	cases := []struct {
		name        string
		edgeControl string
		wantNested  bool
	}{
		{name: "fragment signals ESI", edgeControl: "dca=esi", wantNested: true},
		{name: "fragment does not signal ESI", edgeControl: "", wantNested: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deepestCalls = 0
			// The middle fragment always contains another include. The
			// ParseOnHeader flag decides whether that content is parsed.
			middle := fragmentServer(t, `<esi:include src="`+deepest.URL+`" />`, tc.edgeControl)
			m := newParseOnHeaderModule(t, true)

			rec := servePage(t, m, esiPage(middle.URL), "dca=esi")
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
			if deepestCalls != 0 {
				t.Errorf("deepest server calls = %d, want 0", deepestCalls)
			}
		})
	}
}
