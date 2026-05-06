# [caddy] Add `max_concurrent_requests` Caddyfile directive

## Problem

`EsiParserConfig.MaxConcurrentRequests` (0 = unlimited) is not configurable. A page with 100 includes spawns 100 concurrent HTTP goroutines.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    MaxConcurrentRequests int `json:"max_concurrent_requests,omitempty"`
}
```

### 2. Parse

```go
case "max_concurrent_requests":
    if !d.NextArg() { return d.ArgErr() }
    m.MaxConcurrentRequests, _ = strconv.Atoi(d.Val())
```

### 3. Map

```go
config.MaxConcurrentRequests = m.MaxConcurrentRequests
// 0 = unlimited (backward compatible)
```

### Caddyfile

```
mesi {
    max_concurrent_requests 5
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `5`, `0` (unlimited), `-1` (invalid→default)
- [ ] **Docs** — Add directive to README, clarify per-request scope
- [ ] **Functional tests** — Caddy integration test:
  - 20 includes, `max_concurrent_requests 3` → funneled through 3 slots
  - `0` → unlimited (backward compat)
- [ ] **Changelog** — Entry
