package mesi

import "testing"

func TestUnescape(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal closed tag", "Hello <!--esi <p>content</p>--> World", "Hello <p>content</p> World"},
		{"unclosed in middle", "Hello <!--esi <p>content", "Hello <!--esi <p>content"},
		{"unclosed at start", "<!--esi <p>content", "<!--esi <p>content"},
		{"empty input", "", ""},
		{"no esi tags", "Plain text without ESI", "Plain text without ESI"},
		{"multiple esi tags all closed", "A<!--esi X-->B<!--esi Y-->C", "AXBYC"},
		{"closed then unclosed", "A<!--esi X-->B<!--esi Y", "AXB<!--esi Y"},
		{"closed at end then unclosed", "<!--esi X-->Y<!--esi Z", "XY<!--esi Z"},
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
