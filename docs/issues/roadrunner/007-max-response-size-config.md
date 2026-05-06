# [roadrunner] Add `max_response_size` config option

## Problem

`EsiParserConfig.MaxResponseSize` (default 10 MB) not configurable.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    MaxResponseSize int64 `mapstructure:"max_response_size"`  // bytes, 0 = default 10MB
}
```

### 2. Map

```go
if p.config.MaxResponseSize > 0 {
    config.MaxResponseSize = p.config.MaxResponseSize
}
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      max_response_size: 1048576  # 1 MB
```

## Acceptance criteria

- [ ] **Tests** — valid bytes, 0 → default, absent → default
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Integration test: 100 byte limit → 200 byte include rejected
- [ ] **Changelog** — Entry
