# [caddy] Add `timeout` Caddyfile directive

## Problem

`servers/caddy/mesi.go:47` hardcodes `Timeout` to 10 seconds:

```go
Timeout: 10 * time.Second,  // hardcoded
```

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    MaxDepth int    `json:"max_depth,omitempty"`
    Timeout  string `json:"timeout,omitempty"`  // e.g. "10s", "30s"
}
```

### 2. Parse

```go
case "timeout":
    if !d.NextArg() { return d.ArgErr() }
    m.Timeout = d.Val()
```

### 3. Use in ServeHTTP

```go
timeout := 10 * time.Second
if m.Timeout != "" {
    if d, err := time.ParseDuration(m.Timeout); err == nil && d > 0 {
        timeout = d
    }
}
config.Timeout = timeout
```

### Caddyfile

```
mesi {
    timeout 15s
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `timeout 5s`, `timeout 1m`, `timeout 0s` (invalid→default), `timeout abc` (invalid→default)
- [ ] **Tests** — Unit test default: unset → 10s
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Caddy integration test:
  - `timeout 2s` → backend include sleeps 5s → timeout at 2s
  - `timeout 30s` → backend responds at 10s → success
- [ ] **Changelog** — Entry

## Notes

- Uses `time.ParseDuration` — supports `s`, `m`, `h` suffixes.
- Empty or invalid → default 10s (backward compatible). Log a warning for invalid values.
