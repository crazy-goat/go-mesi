# [core] Implement `<esi:inline>`, `<esi:choose>`, `<esi:try>`, `<esi:vars>`

## Problem

The ESI tokenizer (`mesi/tokenizer.go`) recognizes `<esi:inline>`, `<esi:choose>`, `<esi:try>`, and `<esi:vars>` tags during lexing — they do not cause parse errors. However, the parser drops their content silently from the output. These are standard ESI Language Specification 1.0 instructions that enable conditional content inclusion, variable substitution, and exception handling at the edge layer.

Affected tags:

| Instruction | Spec behavior | Current behavior |
|---|---|---|
| `<esi:inline>` | Outputs its body without further ESI processing; useful for escaping ESI-aware markup | Body dropped |
| `<esi:choose>` + `<esi:when>` + `<esi:otherwise>` | Conditional content selection based on boolean expressions | Entire block dropped |
| `<esi:try>` + `<esi:attempt>` + `<esi:except>` | Exception handling — renders `attempt` on success, `except` on failure | Entire block dropped |
| `<esi:vars>` | Defines variables for use in other ESI expressions | Body dropped |

## Impact

- Edge-side includes are the primary use case, but these auxiliary instructions are essential for real-world ESI workflows (e.g., Akamai, Varnish ESI).
- Any downstream application relying on `<esi:choose>` for A/B testing or `<esi:try>` for graceful degradation cannot use mESI as a drop-in replacement.
- Tokenizer already does the heavy lifting — the remaining work is semantic evaluation in the parser.

## Context

`mesi/tokenizer.go` already tokenizes these tags with proper types (`esiInlineToken`, `esiChooseToken`, `esiTryToken`, `esiVarsToken`). The issue is purely in the parser/renderer — `mesi/parser.go` and `mesi/include.go` do not handle these token types during tree construction and rendering.

The tokenizer currently stores tag boundaries but does not extract the structured content (expression attributes, `when`/`otherwise`/`attempt`/`except` sub-blocks). This needs to be extended.

Reference: [W3C ESI Language Specification 1.0](https://www.w3.org/TR/esi-lang) (W3C Note, 2001-08-04).

## Proposed solution

### Phase 1: `<esi:inline>`

Simplest case — output body verbatim, bypassing ESI parsing.

```go
// pseudo-code for parser tree construction
case esiInlineToken:
    child := &esiNode{
        Type:    esiInline,
        Content: token.Body, // raw content, no further parsing
    }
    parent.AddChild(child)
```

Rendering is a plain write of `node.Content`.

### Phase 2: `<esi:choose>`

Parse `<esi:when test="...">` / `<esi:otherwise>` sub-blocks. Evaluate `test` expressions against request variables available at parse time (e.g. `$(HTTP_COOKIE{name})`, `$(HTTP_HOST)`, `$(QUERY_STRING{param})`).

```go
type esiChooseNode struct {
    Conditions []esiWhenClause
    Otherwise  *esiNode // optional
}

type esiWhenClause struct {
    Test string
    Body *esiNode
}
```

### Phase 3: `<esi:try>`

Parse `<esi:attempt>` / `<esi:except>` sub-blocks. Render `attempt` on success, `except` if the attempt's includes fail.

```go
type esiTryNode struct {
    Attempt *esiNode
    Except  *esiNode
}
```

### Phase 4: `<esi:vars>`

Parse `<esi:variable name="..."/>` inside the vars block. Store variables for use by `$(...)` expressions in scope.

### Variable/expression evaluation

Implement a minimal expression evaluator for `$(...)` substitutions. Variables come from:
- `<esi:vars>` definitions
- HTTP request headers: `$(HTTP_HEADER{name})`
- Other ESI variables per spec

## Acceptance criteria

- [ ] **Tests** — Unit tests in `tokenizer_test.go` for all new token types with edge cases (malformed, nested, empty body)
- [ ] **Tests** — Unit tests in `parser_test.go` verifying correct output for each instruction (inline passthrough, choose branching, try fallback)
- [ ] **Tests** — Unit tests for the expression evaluator (variable substitution, boolean logic, edge cases)
- [ ] **Docs** — Update `README.md` feature matrix from ⚠️ to ✅ for all four instructions
- [ ] **Functional tests** — CI test fixtures exercising:
  - `<esi:inline>` with nested ESI markup (must NOT be processed)
  - `<esi:choose>` with `test` conditions matching and non-matching
  - `<esi:choose>` with `otherwise` fallback
  - `<esi:try>` with successful and failing includes
  - `<esi:vars>` with variable references in downstream `<esi:include>` URLs
- [ ] **Changelog** — Entry in `CHANGELOG.md` documenting newly supported ESI instructions
- [ ] **Spec compliance** — Reference the W3C ESI 1.0 spec for each instruction; any intentional deviation must be documented
- [ ] **Backward compatibility** — Documents already containing these tags must produce identical output to the previous version when the feature is disabled (opt-in via config flag initially?)

## Notes

- `<esi:inline>` contains its name as reference only — the implementation is straightforward output bypass.
- `<esi:choose>` evaluation requires access to request context. Consider whether `EsiParserConfig` needs a new field for variable/header injection (e.g. `Variables map[string]string`).
- `<esi:try>` interacts with `onerror="continue"` semantics on nested `<esi:include>` tags. Ensure consistency.
- Consider a phased rollout: `<esi:inline>` first (trivial), then `<esi:choose>`, then `<esi:try>` + `<esi:vars>` together (they share expression evaluation).
- Expression format `$(...)` may conflict with existing shell or template syntax in non-ESI contexts. Since mESI only processes documents with ESI content, this is not a concern.
