# [roadrunner] Add `timeout` config option

## Problem

`servers/roadrunner/mesi.go:36` hardcodes `Timeout` to 10 seconds. Not configurable.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    MaxDepth int    `mapstructure:"max_depth"`
    Timeout  string `mapstructure:"timeout"`  // e.g. "10s"
}
```

### 2. Parse in Init()

```go
if p.config.Timeout == "" {
    p.config.Timeout = "10s"
}
if _, err := time.ParseDuration(p.config.Timeout); err != nil {
    p.config.Timeout = "10s"
}
```

### 3. Use in Middleware()

```go
timeout, _ := time.ParseDuration(p.config.Timeout)
config := mesi.EsiParserConfig{
    Timeout: timeout,
    // ...
}
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      timeout: "15s"
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `"5s"`, `"1m"`, `"0s"` (invalid→default), `"abc"` (invalid→default)
- [ ] **Tests** — Unit test: absent → 10s default
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Integration test: `timeout: "2s"` + slow backend 5s → times out at 2s
- [ ] **Changelog** — Entry
