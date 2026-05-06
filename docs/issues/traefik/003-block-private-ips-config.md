# [traefik] Add `blockPrivateIPs` plugin config option

## Problem

`servers/traefik/mesi.go:66` hardcodes `BlockPrivateIPs: true`. No way to disable.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    BlockPrivateIPs *bool `json:"blockPrivateIPs" yaml:"blockPrivateIPs"`
}
```

`*bool` to distinguish nil (default true) from explicit `false`.

### 2. Default

```go
func CreateConfig() *Config {
    defaultBP := true
    return &Config{
        // ...
        BlockPrivateIPs: &defaultBP,
    }
}
```

### 3. Use

```go
bp := true
if p.config.BlockPrivateIPs != nil {
    bp = *p.config.BlockPrivateIPs
}
config.BlockPrivateIPs = bp
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          blockPrivateIPs: false
```

## Acceptance criteria

- [ ] **Tests** — `false` → false, `true` → true, absent → default true
- [ ] **Docs** — Add to README with security note
- [ ] **Functional tests** — `false` → `http://127.0.0.1/` include succeeds; absent → blocked
- [ ] **Changelog** — Entry
