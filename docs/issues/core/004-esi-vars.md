# [core] Implement `<esi:vars>` and `$(...)` expression evaluation

## Problem

`mesi/tokenizer.go` recognizes `<esi:vars>` and `<esi:variable>` tags but the parser drops their content. Variable definitions and `$(...)` expression substitution are not implemented.

Spec: `<esi:vars>` defines named variables scoped to the current ESI document. Variables are referenced via `$(VAR_NAME)` syntax in:
- `<esi:when test="$(VAR_NAME)">` conditions
- `<esi:include src="$(URL_VAR)">` URLs
- Any text content where ESI substitution is expected

No `$(...)` evaluation exists anywhere in the parser.

## Impact

- `<esi:choose>` conditions are limited to boolean literals (see issue #002) — no variable-based branching.
- Dynamic include URLs (e.g., `src="$(BACKEND_HOST)/fragment"`) are impossible.
- No content substitution (e.g., `$(USER_NAME)` in text).

## Context

**Affects all servers.** Core parser change. This issue enables:
- `<esi:choose>` with variable-based conditions (`test="$(COOKIE_segment) == 'premium'"`)
- Dynamic `<esi:include>` URLs using variables
- Text substitution

Variables can come from:
1. `<esi:variable name="..." value="..."/>` in `<esi:vars>` blocks
2. Request headers: `$(HTTP_HEADER{Accept-Language})`
3. Query parameters: `$(QUERY_STRING{param})`
4. Cookies: `$(HTTP_COOKIE{cookie_name})`

## Proposed solution

### Variable storage in EsiParserConfig

```go
type EsiParserConfig struct {
    // ...
    Variables map[string]string  // populated from <esi:vars> blocks
}
```

### Parser: `<esi:vars>` block

```go
case esiVarsToken:
    for _, child := range token.Children {
        if varToken, ok := child.(esiVariableToken); ok {
            config.Variables[varToken.Name] = varToken.Value
        }
    }
```

### Expression evaluator

```go
var varPattern = regexp.MustCompile(`\$\(([^)]+)\)`)

func evaluateExpression(expr string, vars map[string]string, r *http.Request) string {
    return varPattern.ReplaceAllStringFunc(expr, func(match string) string {
        name := match[2 : len(match)-1] // strip $()
        
        // Check explicit variables first
        if val, ok := vars[name]; ok {
            return val
        }
        
        // HTTP_HEADER{Name}
        if strings.HasPrefix(name, "HTTP_HEADER{") {
            header := name[12 : len(name)-1]
            return r.Header.Get(header)
        }
        
        // HTTP_COOKIE{Name}
        if strings.HasPrefix(name, "HTTP_COOKIE{") {
            cookie := name[12 : len(name)-1]
            if c, err := r.Cookie(cookie); err == nil {
                return c.Value
            }
        }
        
        // QUERY_STRING{param}
        if strings.HasPrefix(name, "QUERY_STRING{") {
            param := name[13 : len(name)-1]
            return r.URL.Query().Get(param)
        }
        
        return "" // undefined variable → empty string
    })
}
```

Apply evaluation to:
- `<esi:include src="...">` — before fetching
- `<esi:when test="...">` — before boolean evaluation
- Text content between tags — substitution pass

### Example

```html
<esi:vars>
    <esi:variable name="BACKEND" value="http://backend:8000"/>
</esi:vars>
<esi:include src="$(BACKEND)/fragment" />
```

Evaluates `src` to `http://backend:8000/fragment` before fetching.

## Acceptance criteria

- [ ] **Tests** — Unit test: `<esi:variable>` definitions populate `config.Variables`
- [ ] **Tests** — Unit test: `$(VAR_NAME)` substitution in `<esi:include src>` — URL resolved correctly
- [ ] **Tests** — Unit test: `$(HTTP_HEADER{Accept-Language})` → resolves from request header
- [ ] **Tests** — Unit test: `$(HTTP_COOKIE{session})` → resolves from cookie
- [ ] **Tests** — Unit test: `$(QUERY_STRING{page})` → resolves from query parameter
- [ ] **Tests** — Unit test: undefined variable `$(NO_SUCH_VAR)` → empty string (not error)
- [ ] **Tests** — Unit test: nested `$($(...))` → handled gracefully (probably no-op)
- [ ] **Tests** — Unit test: text substitution — `$(USER)` in body text replaced
- [ ] **Tests** — Unit test: `<esi:vars>` with empty body → no-op
- [ ] **Tests** — Unit test: `<esi:vars>` with malformed `<esi:variable>` (missing name/value) → skip, log warning
- [ ] **Docs** — Update README feature matrix ⚠️ → ✅
- [ ] **Docs** — Document supported `$(...)` variable sources and syntax
- [ ] **Functional tests** — CI fixture with variables-driven include URLs across all servers
- [ ] **Changelog** — Entry

## Notes

- Scoping: `<esi:vars>` is document-scoped. Variables are available to all subsequent ESI instructions in the same document.
- Variable name collision: explicit `<esi:variable>` definitions take precedence over `HTTP_HEADER` / `HTTP_COOKIE` / `QUERY_STRING` lookups.
- Security: `$(HTTP_HEADER{...})` exposes request headers to ESI document authors. This is by design (spec) but operators should be aware that ESI templates can read arbitrary headers.
- Performance: regex-based substitution per text node. For documents with many `$(...)` references, consider a single-pass scanner.
- Integration with `<esi:choose>` (#002): once variables work, `<esi:when test="$(COOKIE_segment) == 'premium'">` requires the boolean comparison to support string equality. Coordinate with #002.
