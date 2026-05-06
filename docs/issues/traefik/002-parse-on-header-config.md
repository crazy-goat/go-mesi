# [traefik] Add `parseOnHeader` plugin config option

## Problem

`ParseOnHeader` not exposed in Traefik plugin config. Always `false`.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    ParseOnHeader bool `json:"parseOnHeader" yaml:"parseOnHeader"`
}
```

### 2. Check in ServeHTTP() + pass

```go
if p.config.ParseOnHeader {
    sc := customWriter.Header().Get("Surrogate-Control")
    if !strings.Contains(sc, "ESI") {
        rw.Write(customWriter.Body().Bytes())
        return
    }
}
config.ParseOnHeader = p.config.ParseOnHeader
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          parseOnHeader: true
```

## Acceptance criteria

- [ ] **Tests** — `true` / `false`, absent → `false`
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — `true` + no ESI header → passthrough; `true` + header → processed
- [ ] **Changelog** — Entry
