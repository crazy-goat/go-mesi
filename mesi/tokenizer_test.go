package mesi

import (
	"testing"
)

func TestEsiTokenizer(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected []esiToken
	}{
		{
			"no esi tags",
			"Hello World",
			[]esiToken{{staticContent: "Hello World"}},
		},
		{
			"empty input",
			"",
			[]esiToken{{staticContent: ""}},
		},
		{
			"simple include tag",
			"Before <esi:include src=\"/fragments/header\"/> After",
			[]esiToken{
				{staticContent: "Before "},
				{esiTagContent: "<esi:include src=\"/fragments/header\"/>", esiTagType: "include"},
				{staticContent: " After"},
			},
		},
		{
			"include tag with closing tag",
			"Before <esi:include src=\"/fragments/header\"></esi:include> After",
			[]esiToken{
				{staticContent: "Before "},
				{esiTagContent: "<esi:include src=\"/fragments/header\"></esi:include>", esiTagType: "include"},
				{staticContent: " After"},
			},
		},
		{
			"comment tag",
			"text <esi:comment text=\"my comment\"/> more",
			[]esiToken{
				{staticContent: "text "},
				{esiTagContent: "<esi:comment text=\"my comment\"/>", esiTagType: "comment"},
				{staticContent: " more"},
			},
		},
		{
			"multiple esi tags",
			"A<esi:include src=\"/a\"/>B<esi:include src=\"/b\"/>C",
			[]esiToken{
				{staticContent: "A"},
				{esiTagContent: "<esi:include src=\"/a\"/>", esiTagType: "include"},
				{staticContent: "B"},
				{esiTagContent: "<esi:include src=\"/b\"/>", esiTagType: "include"},
				{staticContent: "C"},
			},
		},
		{
			"unclosed include at end",
			"Start <esi:include src=\"/a\"",
			[]esiToken{
				{staticContent: "Start "},
				{staticContent: "<esi:include src=\"/a\""},
			},
		},
		{
			"nested esi tags - include inside choose",
			"<esi:choose><esi:when test=\"true\"><esi:include src=\"/a\"/></esi:when></esi:choose>",
			[]esiToken{
				{esiTagContent: "<esi:choose><esi:when test=\"true\"><esi:include src=\"/a\"/></esi:when></esi:choose>", esiTagType: "choose"},
				{staticContent: ""},
			},
		},
		{
			"attribute with greater than sign",
			`<esi:include src="/a?x=1&gt=2"/>`,
			[]esiToken{
				{esiTagContent: `<esi:include src="/a?x=1&gt=2"/>`, esiTagType: "include"},
				{staticContent: ""},
			},
		},
		{
			"multiline input",
			"line1\n<esi:include src=\"/a\"/>\nline3",
			[]esiToken{
				{staticContent: "line1\n"},
				{esiTagContent: "<esi:include src=\"/a\"/>", esiTagType: "include"},
				{staticContent: "\nline3"},
			},
		},
		{
			"choose tag with closing",
			"Start <esi:choose><esi:when test=\"true\">A</esi:when></esi:choose> End",
			[]esiToken{
				{staticContent: "Start "},
				{esiTagContent: "<esi:choose><esi:when test=\"true\">A</esi:when></esi:choose>", esiTagType: "choose"},
				{staticContent: " End"},
			},
		},
		{
			"try tag with closing",
			"Start <esi:try><esi:attempt>A</esi:attempt></esi:try> End",
			[]esiToken{
				{staticContent: "Start "},
				{esiTagContent: "<esi:try><esi:attempt>A</esi:attempt></esi:try>", esiTagType: "try"},
				{staticContent: " End"},
			},
		},
		{
			"remove tag with closing",
			"Start <esi:remove>hidden</esi:remove> End",
			[]esiToken{
				{staticContent: "Start "},
				{esiTagContent: "<esi:remove>hidden</esi:remove>", esiTagType: "remove"},
				{staticContent: " End"},
			},
		},
		{
			"inline tag with closing",
			"Start <esi:inline name=\"foo\">content</esi:inline> End",
			[]esiToken{
				{staticContent: "Start "},
				{esiTagContent: "<esi:inline name=\"foo\">content</esi:inline>", esiTagType: "inline"},
				{staticContent: " End"},
			},
		},
		{
			"vars tag with closing",
			"Start <esi:vars><esi:var name=\"x\" value=\"1\"/></esi:vars> End",
			[]esiToken{
				{staticContent: "Start "},
				{esiTagContent: "<esi:vars><esi:var name=\"x\" value=\"1\"/></esi:vars>", esiTagType: "vars"},
				{staticContent: " End"},
			},
		},
		{
			"static text between esi tags",
			"prefix<esi:include src=\"/a\"/>middle<esi:include src=\"/b\"/>suffix",
			[]esiToken{
				{staticContent: "prefix"},
				{esiTagContent: "<esi:include src=\"/a\"/>", esiTagType: "include"},
				{staticContent: "middle"},
				{esiTagContent: "<esi:include src=\"/b\"/>", esiTagType: "include"},
				{staticContent: "suffix"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := esiTokenizer(c.input)
			if len(result) != len(c.expected) {
				t.Errorf("len(%q) = %d, want %d", c.input, len(result), len(c.expected))
				return
			}
			for i, r := range result {
				if r.staticContent != c.expected[i].staticContent {
					t.Errorf("result[%d].staticContent = %q, want %q", i, r.staticContent, c.expected[i].staticContent)
				}
				if r.esiTagContent != c.expected[i].esiTagContent {
					t.Errorf("result[%d].esiTagContent = %q, want %q", i, r.esiTagContent, c.expected[i].esiTagContent)
				}
				if r.esiTagType != c.expected[i].esiTagType {
					t.Errorf("result[%d].esiTagType = %q, want %q", i, r.esiTagType, c.expected[i].esiTagType)
				}
			}
		})
	}
}

func TestEsiTokenIsEsi(t *testing.T) {
	cases := []struct {
		name     string
		token    esiToken
		expected bool
	}{
		{"static only", esiToken{staticContent: "hello"}, false},
		{"empty", esiToken{}, false},
		{"has esi tag", esiToken{esiTagType: "include", esiTagContent: "test"}, true},
		{"has esi tag type only", esiToken{esiTagType: "include"}, false},
		{"has esi content only", esiToken{esiTagContent: "test"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := c.token.isEsi()
			if result != c.expected {
				t.Errorf("isEsi() = %v, want %v", result, c.expected)
			}
		})
	}
}

func TestEsiTokenIsStaticText(t *testing.T) {
	cases := []struct {
		name     string
		token    esiToken
		expected bool
	}{
		{"static only", esiToken{staticContent: "hello"}, true},
		{"empty", esiToken{}, false},
		{"has esi tag", esiToken{esiTagType: "include", esiTagContent: "test"}, false},
		{"static with esi tag", esiToken{staticContent: "hello", esiTagType: "include", esiTagContent: "test"}, false},
		{"empty static", esiToken{staticContent: ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := c.token.isStaticText()
			if result != c.expected {
				t.Errorf("isStaticText() = %v, want %v", result, c.expected)
			}
		})
	}
}

func TestEsiTokenIsSupported(t *testing.T) {
	cases := []struct {
		name     string
		token    esiToken
		expected bool
	}{
		{"include tag", esiToken{esiTagType: "include", esiTagContent: "test"}, true},
		{"choose tag", esiToken{esiTagType: "choose", esiTagContent: "test"}, false},
		{"try tag", esiToken{esiTagType: "try", esiTagContent: "test"}, false},
		{"remove tag", esiToken{esiTagType: "remove", esiTagContent: "test"}, false},
		{"comment tag", esiToken{esiTagType: "comment", esiTagContent: "test"}, false},
		{"vars tag", esiToken{esiTagType: "vars", esiTagContent: "test"}, false},
		{"inline tag", esiToken{esiTagType: "inline", esiTagContent: "test"}, false},
		{"static only", esiToken{staticContent: "hello"}, false},
		{"empty", esiToken{}, false},
		{"include tag type only without content", esiToken{esiTagType: "include"}, false},
		{"include with static content", esiToken{staticContent: "prefix", esiTagType: "include", esiTagContent: "test"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := c.token.isSupported()
			if result != c.expected {
				t.Errorf("isSupported() = %v, want %v", result, c.expected)
			}
		})
	}
}
