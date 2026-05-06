# [traefik] Add `timeout` plugin config option

## Problem

`servers/traefik/mesi.go:65` hardcodes `Timeout` to 10 seconds. Not configurable.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    MaxDepth int    `json:"maxDepth" yaml:"maxDepth"`
    Timeout  string `json:"timeout" yaml:"timeout"`  // e.g. "10s"
}
```

### 2. Default in CreateConfig()

```go
func CreateConfig() *Config {
    return &Config{
        MaxDepth: 5,
        Timeout:  "10s",
    }
}
```

### 3. Parse + use in ServeHTTP()

```go
timeout, err := time.ParseDuration(p.config.Timeout)
if err != nil || timeout <= 0 {
    timeout = 10 * time.Second
}
config.Timeout = timeout
```

### YAML config

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          timeout: "15s"
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `"5s"`, `"1m"`, `""` (default), `"abc"` (default)
- [ ] **Tests** — Unit test: absent → `"10s"`
- [ ] **Docs** — Add to `servers/traefik/README.md`
- [ ] **Functional tests** — Docker integration test:
  - `timeout: "2s"` + slow backend 5s → timeout at 2s
  - `timeout: "30s"` + backend 10s → success
- [ ] **Changelog** — Entry
