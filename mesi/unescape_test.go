package mesi

import "testing"

func TestUnescape(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		// Original cases (with corrected expectation for the space-stripping bug)
		{"normal closed tag", "Hello <!--esi <p>content</p>--> World", "Hello  <p>content</p> World"},
		{"unclosed in middle", "Hello <!--esi <p>content", "Hello <!--esi <p>content"},
		{"unclosed at start", "<!--esi <p>content", "<!--esi <p>content"},
		{"empty input", "", ""},
		{"no esi tags", "Plain text without ESI", "Plain text without ESI"},
		{"multiple esi tags all closed", "A<!--esi X-->B<!--esi Y-->C", "A XB YC"},
		{"closed then unclosed", "A<!--esi X-->B<!--esi Y", "A XB<!--esi Y"},
		{"closed at end then unclosed", "<!--esi X-->Y<!--esi Z", " XY<!--esi Z"},
		// Bug #109: no space after open, newline, tab, multiple spaces
		{"no space after open", "<!--esi<esi:include src=\"x\"/>-->", "<esi:include src=\"x\"/>"},
		{"newline after open", "<!--esi\n<esi:include src=\"x\"/>\n-->", "\n<esi:include src=\"x\"/>\n"},
		{"tab after open", "<!--esi\t<esi:include src=\"x\"/>-->", "\t<esi:include src=\"x\"/>"},
		{"two spaces after open", "<!--esi  <esi:include src=\"x\"/>-->", "  <esi:include src=\"x\"/>"},
		{"empty esi block", "<!--esi-->", ""},
		{"empty esi block with trailing text", "<!--esi-->after", "after"},
		{"multiple mixed spacing", "<!--esi<a/>--><!--esi <b/>-->", "<a/> <b/>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := unescape(c.input)
			if result != c.expected {
				t.Errorf("unescape(%q) = %q, want %q", c.input, result, c.expected)
			}
		})
	}
}
