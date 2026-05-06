# [caddy] Add `max_depth` Caddyfile directive

## Problem

`servers/caddy/mesi.go:46` hardcodes `MaxDepth` to 5:

```go
config := mesi.EsiParserConfig{
    Context:    r.Context(),
    MaxDepth:   5,  // hardcoded
    // ...
}
```

No Caddyfile directive controls this. Operators cannot change ESI nesting depth.

## Context

Caddy's `MesiMiddleware` is an empty struct (line 22). `UnmarshalCaddyfile` (line 80) parses directives inside the `mesi { }` block. Currently accepts no arguments.

## Proposed solution

### 1. Add field to MesiMiddleware

```go
type MesiMiddleware struct {
    MaxDepth int `json:"max_depth,omitempty"`
}
```

### 2. Parse in UnmarshalCaddyfile

```go
case "max_depth":
    if !d.NextArg() { return d.ArgErr() }
    m.MaxDepth, _ = strconv.Atoi(d.Val())
```

### 3. Use in ServeHTTP

```go
depth := m.MaxDepth
if depth <= 0 {
    depth = 5
}
config := mesi.EsiParserConfig{
    MaxDepth: uint(depth),
    // ...
}
```

### Caddyfile

```
mesi {
    max_depth 3
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `max_depth 3` (valid), `max_depth 0` (valid=passthrough), `max_depth abc` (invalid→use default)
- [ ] **Tests** — Unit test default: unset → 5
- [ ] **Docs** — Add directive to `servers/caddy/README.md`
- [ ] **Functional tests** — Caddy integration test:
  - `max_depth 1` → 2-level nested include: inner NOT processed
  - `max_depth 5` → both levels processed
  - `max_depth 0` → ESI not processed (passthrough)
- [ ] **Changelog** — Entry
