# [traefik] Add `maxConcurrentRequests` plugin config option

## Problem

`EsiParserConfig.MaxConcurrentRequests` (0 = unlimited) not configurable.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    MaxConcurrentRequests int `json:"maxConcurrentRequests" yaml:"maxConcurrentRequests"`
}
```

### 2. Map

```go
config.MaxConcurrentRequests = p.config.MaxConcurrentRequests
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          maxConcurrentRequests: 5
```

## Acceptance criteria

- [ ] **Tests** — valid int, 0 → unlimited, absent → 0
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — 20 includes with limit 3 → funneled
- [ ] **Changelog** — Entry
