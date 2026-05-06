# [traefik] Add `debug` plugin config option

## Problem

`EsiParserConfig.Debug` not configurable. No debug logging.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    Debug bool `json:"debug" yaml:"debug"`
}
```

### 2. Map

```go
config.Debug = p.config.Debug
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          debug: true
```

## Acceptance criteria

- [ ] **Tests** — `true` / `false`, absent → `false`
- [ ] **Docs** — Add to README, note output to stderr → Traefik logs
- [ ] **Functional tests** — `debug: true` → messages in Traefik logs
- [ ] **Changelog** — Entry
