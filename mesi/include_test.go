package mesi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseIncludeValidXML(t *testing.T) {
	input := `<esi:include src="/fragment.html"/>`
	token, err := parseInclude(input)
	if err != nil {
		t.Fatalf("parseInclude() error = %v", err)
	}
	if token.Src != "/fragment.html" {
		t.Errorf("Src = %q, want %q", token.Src, "/fragment.html")
	}
}

func TestParseIncludeWithAlt(t *testing.T) {
	input := `<esi:include src="/primary.html" alt="/fallback.html"/>`
	token, err := parseInclude(input)
	if err != nil {
		t.Fatalf("parseInclude() error = %v", err)
	}
	if token.Src != "/primary.html" {
		t.Errorf("Src = %q, want %q", token.Src, "/primary.html")
	}
	if token.Alt != "/fallback.html" {
		t.Errorf("Alt = %q, want %q", token.Alt, "/fallback.html")
	}
}

func TestParseIncludeWithTimeout(t *testing.T) {
	input := `<esi:include src="/fragment.html" timeout="5000"/>`
	token, err := parseInclude(input)
	if err != nil {
		t.Fatalf("parseInclude() error = %v", err)
	}
	if token.Timeout != "5000" {
		t.Errorf("Timeout = %q, want %q", token.Timeout, "5000")
	}
}

func TestParseIncludeWithMaxDepth(t *testing.T) {
	input := `<esi:include src="/fragment.html" max-depth="3"/>`
	token, err := parseInclude(input)
	if err != nil {
		t.Fatalf("parseInclude() error = %v", err)
	}
	if token.MaxDepth != "3" {
		t.Errorf("MaxDepth = %q, want %q", token.MaxDepth, "3")
	}
}

func TestParseIncludeWithFetchMode(t *testing.T) {
	input := `<esi:include src="/fragment.html" fetch-mode="concurrent"/>`
	token, err := parseInclude(input)
	if err != nil {
		t.Fatalf("parseInclude() error = %v", err)
	}
	if token.FetchMode != "concurrent" {
		t.Errorf("FetchMode = %q, want %q", token.FetchMode, "concurrent")
	}
}

func TestParseIncludeWithABRatio(t *testing.T) {
	input := `<esi:include src="/a.html" alt="/b.html" ab-ratio="70:30"/>`
	token, err := parseInclude(input)
	if err != nil {
		t.Fatalf("parseInclude() error = %v", err)
	}
	if token.ABRatio != "70:30" {
		t.Errorf("ABRatio = %q, want %q", token.ABRatio, "70:30")
	}
}

func TestParseABValidRatio(t *testing.T) {
	token := &esiIncludeToken{ABRatio: "70:30"}
	ratio := token.parseAB()
	if ratio.A != 70 {
		t.Errorf("A = %d, want 70", ratio.A)
	}
	if ratio.B != 30 {
		t.Errorf("B = %d, want 30", ratio.B)
	}
}

func TestParseABInvalidRatioDefaults(t *testing.T) {
	cases := []struct {
		name  string
		ratio string
		wantA uint
		wantB uint
	}{
		{"missing colon", "7030", 50, 50},
		{"too many parts", "70:30:10", 50, 50},
		{"non-numeric", "abc:def", 50, 50},
		{"empty string", "", 50, 50},
		{"both zero", "0:0", 50, 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := &esiIncludeToken{ABRatio: tc.ratio}
			ratio := token.parseAB()
			if ratio.A != tc.wantA {
				t.Errorf("A = %d, want %d", ratio.A, tc.wantA)
			}
			if ratio.B != tc.wantB {
				t.Errorf("B = %d, want %d", ratio.B, tc.wantB)
			}
		})
	}
}

func TestSelectUrlChoosesBasedOnRatio(t *testing.T) {
	token := &esiIncludeToken{Src: "/src.html", Alt: "/alt.html"}

	srcCount := 0
	altCount := 0
	iterations := 1000

	for i := 0; i < iterations; i++ {
		ratio := abRatio{A: 80, B: 20}
		selected := ratio.selectUrl(token)
		if selected == "/src.html" {
			srcCount++
		} else if selected == "/alt.html" {
			altCount++
		}
	}

	srcPercent := float64(srcCount) / float64(iterations) * 100
	if srcPercent < 70 || srcPercent > 90 {
		t.Errorf("src selected %f%%, expected ~80%%", srcPercent)
	}
}

func TestSelectUrlNoAltReturnsSrc(t *testing.T) {
	token := &esiIncludeToken{Src: "/src.html", Alt: ""}
	ratio := abRatio{A: 50, B: 50}
	selected := ratio.selectUrl(token)
	if selected != "/src.html" {
		t.Errorf("selectUrl() = %q, want %q", selected, "/src.html")
	}
}

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

func TestToStringWithOnerrorContinue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src:     server.URL + "/fail",
		OnError: "continue",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	data, _ := token.toString(config)
	if data != "" {
		t.Errorf("toString() = %q, want empty string (onerror=continue)", data)
	}
}

func TestToStringWithFallbackContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src:     server.URL + "/fail",
		Content: "fallback content",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 1
	config.BlockPrivateIPs = false

	data, _ := token.toString(config)
	if data != "fallback content" {
		t.Errorf("toString() = %q, want %q", data, "fallback content")
	}
}

func TestToStringWithParseOnlyMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called in parse-only mode")
	}))
	defer server.Close()

	token := &esiIncludeToken{
		Src: server.URL + "/fragment",
	}

	config := CreateDefaultConfig()
	config.DefaultUrl = server.URL + "/"
	config.MaxDepth = 0
	config.BlockPrivateIPs = false

	data, _ := token.toString(config)
	if data != "esi max depth" {
		t.Errorf("toString() = %q, want %q", data, "esi max depth")
	}
}
