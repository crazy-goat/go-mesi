# [core] Implement `<esi:try>`, `<esi:attempt>`, `<esi:except>`

## Problem

`mesi/tokenizer.go` recognizes `<esi:try>`, `<esi:attempt>`, and `<esi:except>` but the parser drops their content. Exception handling for ESI includes is not implemented.

Spec: `<esi:try>` renders the `<esi:attempt>` body. If any `<esi:include>` inside `<esi:attempt>` fails (timeout, connection refused, HTTP error), the `<esi:except>` body is rendered instead. This enables graceful degradation: show a fallback when a backend service is unavailable.

## Impact

- No graceful degradation — if one include fails, the entire page may be missing content.
- Operators must use `onerror="continue"` (which leaves an empty hole) or fallback body on every `<esi:include>` individually — no batch error handling.
- Real-world ESI workflows (Akamai, Varnish) heavily use `<esi:try>` for resilience.

## Context

**Affects all servers.** Core parser change.

`<esi:try>` interacts with the include fetch mechanism. The parser must detect include failures within the `<esi:attempt>` subtree and switch rendering to `<esi:except>`.

## Proposed solution

### Token tree

```go
type esiTryNode struct {
    Attempt *esiNode
    Except  *esiNode
}
```

### Rendering with error detection

```go
case esiTry:
    var attemptBuf bytes.Buffer
    attemptErr := node.Attempt.Render(&attemptBuf)
    if attemptErr != nil {
        // Include fetch failed — render except
        node.Except.Render(w)
    } else {
        w.Write(attemptBuf.Bytes())
    }
```

The `Render` method must signal include fetch errors. Options:
- Return `error` from `Render()` for subtrees that had failed includes
- Track failure state in a render context struct

```go
type renderContext struct {
    hasError bool
}

func (n *esiIncludeNode) Render(ctx *renderContext, w io.Writer) error {
    body, err := fetchInclude(n.Src, n.Config)
    if err != nil {
        ctx.hasError = true
        // Render error marker or fallback
        return err
    }
    w.Write(body)
    return nil
}
```

### Example

```html
<esi:try>
    <esi:attempt>
        <esi:include src="http://backend:8000/fragment" />
    </esi:attempt>
    <esi:except>
        <p>Service temporarily unavailable</p>
    </esi:except>
</esi:try>
```

If `backend:8000/fragment` fails → renders `<p>Service temporarily unavailable</p>`.

## Acceptance criteria

- [ ] **Tests** — Unit test: `<esi:attempt>` include succeeds → `<esi:attempt>` body rendered, `<esi:except>` ignored
- [ ] **Tests** — Unit test: `<esi:attempt>` include fails (timeout) → `<esi:except>` body rendered
- [ ] **Tests** — Unit test: `<esi:attempt>` include fails (404) → `<esi:except>` rendered
- [ ] **Tests** — Unit test: `<esi:attempt>` has no failing includes → `<esi:attempt>` rendered
- [ ] **Tests** — Unit test: nested `<esi:try>` — inner try catches, outer try gets clean output
- [ ] **Tests** — Unit test: `<esi:try>` with only `<esi:attempt>` (no `<esi:except>`) → failed include renders empty/error marker
- [ ] **Tests** — Unit test: multiple includes in `<esi:attempt>` — any one fails → whole except rendered
- [ ] **Docs** — Update README feature matrix ⚠️ → ✅
- [ ] **Functional tests** — CI fixture with failing backend, verify graceful degradation across all servers
- [ ] **Changelog** — Entry

## Notes

- Design decision: should ONE failed include switch to `<esi:except>` for the entire try block, or only replace that specific include? Spec says the entire attempt fails → except rendered. Implement spec behavior.
- Interaction with `onerror="continue"`: if an include inside `<esi:attempt>` has `onerror="continue"`, it does NOT constitute a failure for the try block. Only unhandled failures trigger `<esi:except>`.
- Interaction with `<esi:include>` fallback body: fallback body inside `<esi:include>` is rendered, and the include is NOT considered failed for the try block.
