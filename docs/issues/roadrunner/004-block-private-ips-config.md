# [roadrunner] Add `block_private_ips` config option

## Problem

`servers/roadrunner/mesi.go:37` hardcodes `BlockPrivateIPs: true`. No way to disable for legitimate internal includes.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    BlockPrivateIPs *bool `mapstructure:"block_private_ips"`
}
```

`*bool` for nil vs explicit false distinction.

### 2. Init defaults

```go
if p.config.BlockPrivateIPs == nil {
    defaultBP := true
    p.config.BlockPrivateIPs = &defaultBP
}
```

### 3. Use

```go
config.BlockPrivateIPs = *p.config.BlockPrivateIPs
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      block_private_ips: false
```

## Acceptance criteria

- [ ] **Tests** — `false` → disabled, `true` → enabled, absent → default true
- [ ] **Docs** — Add to README with security note
- [ ] **Functional tests** — Integration test: `false` → `http://127.0.0.1/` include succeeds; absent → blocked
- [ ] **Changelog** — Entry
