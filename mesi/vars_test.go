package mesi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvaluateExpression(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		config   EsiParserConfig
		expected string
	}{
		{
			name:     "no variable pattern",
			expr:     "hello world",
			config:   CreateDefaultConfig(),
			expected: "hello world",
		},
		{
			name: "simple variable substitution",
			expr: "prefix $(NAME) suffix",
			config: EsiParserConfig{
				Variables: map[string]string{"NAME": "world"},
			},
			expected: "prefix world suffix",
		},
		{
			name: "multiple variables",
			expr: "$(GREETING) $(TARGET)!",
			config: EsiParserConfig{
				Variables: map[string]string{"GREETING": "Hello", "TARGET": "World"},
			},
			expected: "Hello World!",
		},
		{
			name:     "undefined variable",
			expr:     "hello $(NO_SUCH_VAR)",
			config:   EsiParserConfig{Variables: map[string]string{}},
			expected: "hello ",
		},
		{
			name: "variable in URL",
			expr: "$(BACKEND)/fragment",
			config: EsiParserConfig{
				Variables: map[string]string{"BACKEND": "http://backend:8000"},
			},
			expected: "http://backend:8000/fragment",
		},
		{
			name: "no config variables",
			expr: "hello $(NAME)",
			config: EsiParserConfig{
				Variables: nil,
			},
			expected: "hello ",
		},
		{
			name: "HTTP_HEADER resolution",
			expr: "$(HTTP_HEADER{Accept-Language})",
			config: EsiParserConfig{
				RequestHeaders: http.Header{"Accept-Language": {"en-US"}},
			},
			expected: "en-US",
		},
		{
			name: "HTTP_HEADER with Variables fallback",
			expr: "$(HTTP_HEADER{X-Custom})",
			config: EsiParserConfig{
				Variables:      map[string]string{"HTTP_HEADER{X-Custom}": "fallback"},
				RequestHeaders: http.Header{},
			},
			expected: "fallback",
		},
		{
			name: "HTTP_COOKIE resolution",
			expr: "$(HTTP_COOKIE{session})",
			config: EsiParserConfig{
				RequestCookies: map[string]string{"session": "abc123"},
			},
			expected: "abc123",
		},
		{
			name: "QUERY_STRING resolution",
			expr: "$(QUERY_STRING{page})",
			config: EsiParserConfig{
				RequestQuery: map[string]string{"page": "home"},
			},
			expected: "home",
		},
		{
			name: "HTTP_HEADER missing returns empty",
			expr: "$(HTTP_HEADER{Missing})",
			config: EsiParserConfig{
				RequestHeaders: http.Header{},
			},
			expected: "",
		},
		{
			name: "HTTP_COOKIE missing returns empty",
			expr: "$(HTTP_COOKIE{missing})",
			config: EsiParserConfig{
				RequestCookies: map[string]string{},
			},
			expected: "",
		},
		{
			name: "QUERY_STRING missing returns empty",
			expr: "$(QUERY_STRING{missing})",
			config: EsiParserConfig{
				RequestQuery: map[string]string{},
			},
			expected: "",
		},
		{
			name: "explicit variable takes precedence over HTTP_HEADER",
			expr: "$(HTTP_HEADER{X-Custom})",
			config: EsiParserConfig{
				Variables:      map[string]string{"HTTP_HEADER{X-Custom}": "from_vars"},
				RequestHeaders: http.Header{"X-Custom": {"from_header"}},
			},
			expected: "from_vars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluateExpression(tt.expr, tt.config)
			if result != tt.expected {
				t.Errorf("evaluateExpression(%q) = %q, want %q", tt.expr, result, tt.expected)
			}
		})
	}
}

func TestParseVarsBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "single variable",
			input:    `<esi:vars><esi:variable name="BACKEND" value="http://backend:8000"/></esi:vars>`,
			expected: map[string]string{"BACKEND": "http://backend:8000"},
		},
		{
			name:     "multiple variables",
			input:    `<esi:vars><esi:variable name="A" value="1"/><esi:variable name="B" value="2"/></esi:vars>`,
			expected: map[string]string{"A": "1", "B": "2"},
		},
		{
			name:     "empty body",
			input:    `<esi:vars></esi:vars>`,
			expected: map[string]string{},
		},
		{
			name:     "vars with whitespace",
			input:    `<esi:vars>
	<esi:variable name="FOO" value="bar"/>
</esi:vars>`,
			expected: map[string]string{"FOO": "bar"},
		},
		{
			name:     "self-closing variable tag",
			input:    `<esi:vars><esi:variable name="X" value="y" /></esi:vars>`,
			expected: map[string]string{"X": "y"},
		},
		{
			name:     "malformed variable missing name",
			input:    `<esi:vars><esi:variable value="x"/></esi:vars>`,
			expected: map[string]string{},
		},
		{
			name:     "no esi:vars tags in content",
			input:    `plain text`,
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseVarsBlock(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("parseVarsBlock() = %v, len=%d, want len=%d", result, len(result), len(tt.expected))
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("parseVarsBlock()[%q] = %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

func TestMESIParseWithVarsAndTextSubstitution(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		config   EsiParserConfig
		expected string
	}{
		{
			name: "text substitution with variable from vars block",
			input: `<esi:vars><esi:variable name="USER" value="Alice"/></esi:vars>
Hello $(USER)!`,
			config:   CreateDefaultConfig(),
			expected: "\nHello Alice!",
		},
		{
			name: "vars block produces no output",
			input: `<esi:vars><esi:variable name="X" value="1"/></esi:vars>content`,
			config:   CreateDefaultConfig(),
			expected: "content",
		},
		{
			name:     "pre-populated variables work",
			input:    "$(GREETING) World",
			config:   EsiParserConfig{Variables: map[string]string{"GREETING": "Hello"}},
			expected: "Hello World",
		},
		{
			name: "multiple vars blocks merge",
			input: `<esi:vars><esi:variable name="A" value="1"/></esi:vars>
<esi:vars><esi:variable name="B" value="2"/></esi:vars>
$(A) $(B)`,
			config:   CreateDefaultConfig(),
			expected: "\n\n1 2",
		},
		{
			name: "vars block after text does not apply retroactively",
			input: `$(MSG)
<esi:vars><esi:variable name="MSG" value="hello"/></esi:vars>`,
			config:   CreateDefaultConfig(),
			expected: "\n",
		},
		{
			name: "no variables defined",
			input:    `$(NOTHING)`,
			config:   CreateDefaultConfig(),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MESIParse(tt.input, tt.config)
			if result != tt.expected {
				t.Errorf("MESIParse() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMESIParseWithVarsAndInclude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("included:" + r.URL.Path))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.DefaultUrl = server.URL + "/"
	config.Variables = map[string]string{"PATH": "/fragment"}

	input := `<!--esi <esi:include src="$(PATH)"/>-->`
	result := MESIParse(input, config)

	expected := " included:/fragment"
	if result != expected {
		t.Errorf("MESIParse() = %q, want %q", result, expected)
	}
}

func TestMESIParseWithVarsBlockAndInclude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("included:" + r.URL.Path))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.DefaultUrl = server.URL + "/"

	input := `<esi:vars><esi:variable name="PATH" value="/fragment"/></esi:vars><!--esi <esi:include src="$(PATH)"/>-->`
	result := MESIParse(input, config)

	expected := " included:/fragment"
	if result != expected {
		t.Errorf("MESIParse() = %q, want %q", result, expected)
	}
}

func TestMESIParseWithVarsIncludeUsingBACKEND(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("resource:" + r.URL.Path))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.DefaultUrl = server.URL + "/"
	config.Variables = map[string]string{"BACKEND": server.URL}

	input := `<!--esi <esi:include src="$(BACKEND)/resource"/>-->`
	result := MESIParse(input, config)

	expected := " resource:/resource"
	if result != expected {
		t.Errorf("MESIParse() = %q, want %q", result, expected)
	}
}

func TestMESIParseWithVarsAltAttribute(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fallback content"))
	}))
	defer fallback.Close()

	config := CreateDefaultConfig()
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.DefaultUrl = primary.URL + "/"
	config.Variables = map[string]string{"ALT": fallback.URL + "/alt"}

	input := `<!--esi <esi:include src="http://unreachable/x" alt="$(ALT)"/>-->`
	result := MESIParse(input, config)

	if result != " fallback content" {
		t.Errorf("MESIParse() = %q, want %q", result, " fallback content")
	}
}

func TestMESIParseVarsAndTemplatedURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("path:" + r.URL.Path))
	}))
	defer server.Close()

	config := CreateDefaultConfig()
	config.MaxDepth = 1
	config.BlockPrivateIPs = false
	config.DefaultUrl = server.URL + "/"
	config.Variables = map[string]string{
		"BASE":   server.URL,
		"SEGMENT": "data",
	}

	input := `<!--esi <esi:include src="$(BASE)/api/$(SEGMENT)"/>-->`
	result := MESIParse(input, config)

	if result != " path:/api/data" {
		t.Errorf("MESIParse() = %q, want %q", result, " path:/api/data")
	}
}
