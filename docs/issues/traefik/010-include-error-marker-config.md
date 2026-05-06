# [traefik] Add `includeErrorMarker` plugin config option

## Problem

`EsiParserConfig.IncludeErrorMarker` always empty — failed includes produce invisible output.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    IncludeErrorMarker string `json:"includeErrorMarker" yaml:"includeErrorMarker"`
}
```

### 2. Map

```go
config.IncludeErrorMarker = p.config.IncludeErrorMarker
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          includeErrorMarker: "<!-- esi error -->"
```

## Acceptance criteria

- [ ] **Tests** — string, empty, absent → ""
- [ ] **Docs** — Add to README with security warning
- [ ] **Functional tests** — Marker renders for failed include, not for onerror/fallback
- [ ] **Changelog** — Entry
