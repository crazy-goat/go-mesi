# [roadrunner] Add `debug` config option

## Problem

`EsiParserConfig.Debug` not configurable. No debug logging.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    Debug bool `mapstructure:"debug"`
}
```

### 2. Map

```go
config.Debug = p.config.Debug
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      debug: true
```

## Acceptance criteria

- [ ] **Tests** — `true` / `false`, absent → `false`
- [ ] **Docs** — Add to README, note output to stderr → RR logs
- [ ] **Functional tests** — Integration test: `debug: true` → messages in logs
- [ ] **Changelog** — Entry
