# [roadrunner] Add `max_depth` config option

## Problem

`servers/roadrunner/mesi.go:34` hardcodes `MaxDepth` to 5:

```go
config := mesi.EsiParserConfig{
    Context:    r.Context(),
    MaxDepth:   5,  // hardcoded
    // ...
}
```

RoadRunner plugin has no configuration struct — `Plugin` is empty. No `.rr.yaml` config key controls depth.

## Context

RoadRunner plugins receive config via the `config.Configurer` interface. Plugin struct is a singleton per worker pool. Config is deserialized with `mapstructure` tags.

## Proposed solution

### 1. Add Config struct + Plugin fields

```go
type Config struct {
    MaxDepth int `mapstructure:"max_depth"`
}

type Plugin struct {
    config *Config
}
```

### 2. Read config in Init()

```go
import "github.com/roadrunner-server/roadrunner/v2/plugins/config"

func (p *Plugin) Init(cfg config.Configurer) error {
    p.config = &Config{MaxDepth: 5}  // default
    if err := cfg.UnmarshalKey("http.middleware.mesi", p.config); err != nil {
        return err
    }
    if p.config.MaxDepth <= 0 {
        p.config.MaxDepth = 5
    }
    return nil
}
```

### 3. Use in Middleware()

```go
config := mesi.EsiParserConfig{
    MaxDepth: uint(p.config.MaxDepth),
    // ...
}
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      max_depth: 3
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `max_depth: 3` → parsed, `max_depth: 0` → defaults to 5, absent → defaults to 5
- [ ] **Docs** — Add to `servers/roadrunner/README.md` with `.rr.yaml` example
- [ ] **Functional tests** — RoadRunner integration test:
  - `max_depth: 1` → 2-level nested: inner NOT processed
  - `max_depth: 5` → both levels processed
- [ ] **Changelog** — Entry
