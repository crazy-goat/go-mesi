# [traefik] Add `maxWorkers` plugin config option

## Problem

`EsiParserConfig.MaxWorkers` (0 = `runtime.NumCPU()*4`) not configurable.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    MaxWorkers int `json:"maxWorkers" yaml:"maxWorkers"`
}
```

### 2. Map

```go
config.MaxWorkers = p.config.MaxWorkers
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          maxWorkers: 8
```

## Acceptance criteria

- [ ] **Tests** — valid int, 0 → library default, absent → 0
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Stress test with deep nesting
- [ ] **Changelog** — Entry
