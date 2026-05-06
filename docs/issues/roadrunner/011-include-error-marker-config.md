# [roadrunner] Add `include_error_marker` config option

## Problem

`EsiParserConfig.IncludeErrorMarker` always empty — failed includes produce invisible output.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    IncludeErrorMarker string `mapstructure:"include_error_marker"`
}
```

### 2. Map

```go
config.IncludeErrorMarker = p.config.IncludeErrorMarker
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      include_error_marker: "<!-- esi error -->"
```

## Acceptance criteria

- [ ] **Tests** — string set, empty, absent → ""
- [ ] **Docs** — Add to README with security warning (no internal details)
- [ ] **Functional tests** — Integration test: marker renders for failed include, not for onerror/fallback
- [ ] **Changelog** — Entry
