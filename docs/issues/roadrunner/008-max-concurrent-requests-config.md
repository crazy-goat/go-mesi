# [roadrunner] Add `max_concurrent_requests` config option

## Problem

`EsiParserConfig.MaxConcurrentRequests` (0 = unlimited) not configurable.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    MaxConcurrentRequests int `mapstructure:"max_concurrent_requests"`
}
```

### 2. Map

```go
config.MaxConcurrentRequests = p.config.MaxConcurrentRequests
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      max_concurrent_requests: 5
```

## Acceptance criteria

- [ ] **Tests** — valid int, 0 → unlimited, absent → unlimited
- [ ] **Docs** — Add to README, per-request scope
- [ ] **Functional tests** — Integration test: 20 includes with limit 3 → funneled
- [ ] **Changelog** — Entry
