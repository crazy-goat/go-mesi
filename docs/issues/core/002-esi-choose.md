# [core] Implement `<esi:choose>`, `<esi:when>`, `<esi:otherwise>`

## Problem

`mesi/tokenizer.go` recognizes `<esi:choose>`, `<esi:when>`, and `<esi:otherwise>` tags but the parser drops all their content. Conditional content selection is not implemented.

Spec: `<esi:choose>` selects one branch based on boolean `test` expressions on `<esi:when>` children. If no `<esi:when>` matches, `<esi:otherwise>` is rendered (if present).

## Impact

- A/B testing via ESI — impossible without conditional logic.
- Feature flags at the edge — impossible.
- User-agent-based content selection — impossible.

## Context

**Affects all servers.** Core parser change.

The expression evaluator (`$(...)` variable substitution) is needed but can be minimal initially: support `<esi:when test="...">` with boolean literals (`true`/`false`) and simple comparisons. Full expression support (`$(HTTP_COOKIE{...})`, `$(QUERY_STRING{...})`) can follow in a separate issue or be delegated to `<esi:vars>` (issue #004).

## Proposed solution

### Token tree

```go
type esiChooseNode struct {
    Conditions []esiWhenClause
    Otherwise  *esiNode // optional
}

type esiWhenClause struct {
    Test string   // raw test expression
    Body *esiNode  // child nodes from inside <esi:when>
}
```

### Initial expression evaluator

Start with boolean literals:

```go
func evaluateTest(expr string, vars map[string]string) bool {
    expr = strings.TrimSpace(expr)
    switch expr {
    case "true", "1":
        return true
    case "false", "0", "":
        return false
    }
    // Future: $(HTTP_COOKIE{name}) == "value"
    return false
}
```

### Rendering

```go
case esiChoose:
    for _, cond := range node.Conditions {
        if evaluateTest(cond.Test, config.Variables) {
            cond.Body.Render(w)
            return
        }
    }
    if node.Otherwise != nil {
        node.Otherwise.Render(w)
    }
```

### Example

```html
<esi:choose>
    <esi:when test="true">
        <esi:include src="/variant-a" />
    </esi:when>
    <esi:otherwise>
        <esi:include src="/variant-b" />
    </esi:otherwise>
</esi:choose>
```

Renders `/variant-a`.

## Acceptance criteria

- [ ] **Tests** — Unit test: `test="true"` → renders `<esi:when>` body
- [ ] **Tests** — Unit test: `test="false"` → skips, renders `<esi:otherwise>`
- [ ] **Tests** — Unit test: all `<esi:when>` false, no `<esi:otherwise>` → empty output
- [ ] **Tests** — Unit test: first matching `<esi:when>` wins (short-circuit)
- [ ] **Tests** — Unit test: nested `<esi:include>` inside `<esi:when>` IS processed
- [ ] **Tests** — Unit test: empty test attribute → false
- [ ] **Tests** — Unit test: malformed (missing `</esi:choose>`) → parse error or graceful degradation
- [ ] **Docs** — Update README feature matrix ⚠️ → ✅
- [ ] **Functional tests** — CI fixture across all servers
- [ ] **Changelog** — Entry

## Notes

- Full expression syntax (`$(...)` variables, comparisons, boolean operators) deferred to `<esi:vars>` implementation (issue #004). Boolean literals provide immediate value for flag-based content selection.
- `test` attribute evaluation is case-sensitive for now. Spec suggests case-insensitive boolean keywords.
- Multiple `<esi:choose>` blocks per document must work correctly.
