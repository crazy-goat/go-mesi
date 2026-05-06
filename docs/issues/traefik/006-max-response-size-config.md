# [traefik] Add `maxResponseSize` plugin config option

## Problem

`EsiParserConfig.MaxResponseSize` (default 10 MB) not configurable.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    MaxResponseSize int64 `json:"maxResponseSize" yaml:"maxResponseSize"`
}
```

### 2. Map

```go
if p.config.MaxResponseSize > 0 {
    config.MaxResponseSize = p.config.MaxResponseSize
}
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          maxResponseSize: 1048576
```

## Acceptance criteria

- [ ] **Tests** — valid bytes, 0 → default, absent → default
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — 100 byte limit → 200 byte include rejected
- [ ] **Changelog** — Entry
