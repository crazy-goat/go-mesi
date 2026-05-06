# [caddy] Add `max_workers` Caddyfile directive

## Problem

`EsiParserConfig.MaxWorkers` (0 = `runtime.NumCPU()*4`) is not configurable.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    MaxWorkers int `json:"max_workers,omitempty"`
}
```

### 2. Parse

```go
case "max_workers":
    if !d.NextArg() { return d.ArgErr() }
    m.MaxWorkers, _ = strconv.Atoi(d.Val())
```

### 3. Map

```go
config.MaxWorkers = m.MaxWorkers
```

### Caddyfile

```
mesi {
    max_workers 8
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `4`, `0`, `-1` (invalid→default)
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Stress test with deep nesting, verify with `max_workers 2`
- [ ] **Changelog** — Entry

## Notes

- Token-processing goroutines, not HTTP fetch goroutines. Distinction from `max_concurrent_requests`.
