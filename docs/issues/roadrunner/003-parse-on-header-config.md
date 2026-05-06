# [roadrunner] Add `parse_on_header` config option

## Problem

`servers/roadrunner/mesi.go` does not set `ParseOnHeader`. Always `false`. Backends cannot signal ESI intent via `Surrogate-Control` header.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    ParseOnHeader bool `mapstructure:"parse_on_header"`
}
```

### 2. Check in Middleware() + pass to config

```go
if p.config.ParseOnHeader {
    sc := customWriter.Header().Get("Surrogate-Control")
    if !strings.Contains(sc, "ESI") {
        // passthrough without ESI
        rw.Write(customWriter.Body().Bytes())
        return
    }
}
config.ParseOnHeader = p.config.ParseOnHeader
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      parse_on_header: true
```

## Acceptance criteria

- [ ] **Tests** — `true` → processed, `false` → always processed, absent → `false`
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Integration test: `parse_on_header: true` + no ESI header → passthrough
- [ ] **Changelog** — Entry
