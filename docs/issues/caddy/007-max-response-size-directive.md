# [caddy] Add `max_response_size` Caddyfile directive

## Problem

`EsiParserConfig.MaxResponseSize` (default 10 MB) is not configurable via Caddyfile. Operators cannot set tighter or larger limits.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    MaxResponseSize int64 `json:"max_response_size,omitempty"`
}
```

### 2. Parse

```go
case "max_response_size":
    if !d.NextArg() { return d.ArgErr() }
    m.MaxResponseSize, _ = strconv.ParseInt(d.Val(), 10, 64)
```

### 3. Map

```go
if m.MaxResponseSize > 0 {
    config.MaxResponseSize = m.MaxResponseSize
}
// 0 = library default (10 MB)
```

### Caddyfile

```
mesi {
    max_response_size 1048576
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `max_response_size 1048576`, `max_response_size 0`, `max_response_size -1` (invalid→default)
- [ ] **Tests** — Unit test: absent → library default (10 MB)
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Caddy integration test:
  - `max_response_size 100` → 200-byte include → rejected
  - `max_response_size 1048576` → 500 KB include → succeeds
  - `max_response_size 0` → unlimited
- [ ] **Changelog** — Entry

## Notes

- Values in bytes. Could support human-readable suffixes (`1m`, `1g`) later via `humanize` library.
- 0 = library default (10 MB). Document this vs `0 = unlimited` (which the library also supports for 0 passed explicitly — clarify).
