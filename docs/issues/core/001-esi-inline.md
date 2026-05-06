# [core] Implement `<esi:inline>` element

## Problem

`mesi/tokenizer.go` recognizes `<esi:inline>` during lexing — no parse errors. However, the parser drops its body content silently from the output.

```go
// tokenizer.go: token type exists (esiInlineToken)
// parser.go: no handling — content dropped
```

Spec behavior (W3C ESI 1.0): `<esi:inline>` outputs its body verbatim, **without** further ESI processing. This is the escape hatch — content inside `<esi:inline>` that looks like ESI markup (`<esi:include>`) must NOT be processed.

## Impact

- Applications using `<esi:inline>` to embed documentation or examples containing `<esi:*>` tags lose that content entirely.
- No escape hatch for including literal ESI markup in output.

## Context

This is the simplest ESI instruction — pure passthrough. No expression evaluation, no include fetching, no conditional logic.

Affected files:
- `mesi/tokenizer.go` — tokenizer already handles the tag (start/end boundaries)
- `mesi/include.go` / `mesi/parser.go` — tree construction and rendering
- `mesi/parser_test.go` — tests

**Affects all server integrations** — this is a core parser change used by every server.

## Proposed solution

### Token tree construction

When encountering `esiInlineToken`, create a leaf node with raw body content:

```go
case esiInlineToken:
    node := &esiNode{
        Type:    esiInline,
        Content: token.Body, // raw content between <esi:inline> and </esi:inline>
    }
    parent.AddChild(node)
```

### Rendering

Write `node.Content` directly to output — no escaping, no further ESI parsing:

```go
case esiInline:
    w.WriteString(node.Content)
```

### Example

Input:
```html
<esi:inline>
    Here is an example: <esi:include src="/fragment" />
</esi:inline>
```

Output (correct): the `<esi:include>` tag is rendered as literal text, NOT processed.

## Acceptance criteria

- [ ] **Tests** — Unit test: `<esi:inline>` body rendered verbatim in output
- [ ] **Tests** — Unit test: `<esi:include>` inside `<esi:inline>` is NOT processed (escape hatch works)
- [ ] **Tests** — Unit test: nested `<esi:inline>` (malformed but should not crash)
- [ ] **Tests** — Unit test: empty `<esi:inline></esi:inline>` → empty output
- [ ] **Tests** — Unit test: `<esi:inline>` with HTML markup inside → passed through unchanged
- [ ] **Docs** — Update `README.md` feature matrix: ⚠️ → ✅
- [ ] **Functional tests** — CI test fixture exercising `<esi:inline>` passthrough across all servers
- [ ] **Changelog** — Entry
