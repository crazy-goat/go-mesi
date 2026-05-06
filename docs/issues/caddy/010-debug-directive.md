# [caddy] Add `debug` Caddyfile directive

## Problem

`EsiParserConfig.Debug` is not configurable. No debug logging for ESI processing.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    Debug bool `json:"debug,omitempty"`
}
```

### 2. Parse

```go
case "debug":
    m.Debug = true
```

### 3. Map

```go
config.Debug = m.Debug
```

### Caddyfile

```
mesi {
    debug
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: present → true, absent → false
- [ ] **Docs** — Add directive to README, note output goes to stderr
- [ ] **Functional tests** — Integration test:
  - `debug` → debug messages in Caddy logs
  - Absent → no debug output
- [ ] **Changelog** — Entry

## Notes

- Go debug log goes to stderr. Caddy captures this in its log output.
- Boolean flag, no argument.
- Debug includes fetch URLs — potential info leak in shared logs.
