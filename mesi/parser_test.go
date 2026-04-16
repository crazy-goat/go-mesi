package mesi

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		maxDepth   int
		defaultUrl string
		expected   string
	}{
		{
			name:       "empty input",
			input:      "",
			maxDepth:   5,
			defaultUrl: "http://example.com/",
			expected:   "",
		},
		{
			name:       "no ESI tags",
			input:      "<html><body>Hello World</body></html>",
			maxDepth:   5,
			defaultUrl: "http://example.com/",
			expected:   "<html><body>Hello World</body></html>",
		},
		{
			name:       "max depth 0 with include triggers max depth error",
			input:      "<!--esi <esi:include src=\"x\"/>-->",
			maxDepth:   0,
			defaultUrl: "http://example.com/",
			expected:   "esi max depth",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := Parse(c.input, c.maxDepth, c.defaultUrl)
			if result != c.expected {
				t.Errorf("Parse() = %q, want %q", result, c.expected)
			}
		})
	}
}

func TestParseDefault(t *testing.T) {
	result := Parse("no esi tags", 5, "http://127.0.0.1/")
	if result != "no esi tags" {
		t.Errorf("Parse() = %q, want %q", result, "no esi tags")
	}
}
