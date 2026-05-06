# [roadrunner] Add `allow_private_ips_for_allowed_hosts` config option

## Problem

When `block_private_ips: true` and `allowed_hosts` contains internal hosts on private IPs, includes are still blocked. Need granular bypass for trusted hosts.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    AllowPrivateIPsForAllowedHosts bool `mapstructure:"allow_private_ips_for_allowed_hosts"`
}
```

### 2. Map

```go
config.AllowPrivateIPsForAllowedHosts = p.config.AllowPrivateIPsForAllowedHosts
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      block_private_ips: true
      allowed_hosts:
        - backend.internal
      allow_private_ips_for_allowed_hosts: true
```

## Acceptance criteria

- [ ] **Tests** — `true` / `false`, absent → `false`
- [ ] **Docs** — Add to README with DNS-control security warning
- [ ] **Functional tests** — Integration test: bypass works for listed host, not for unlisted
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked until core `ssrf.go` implements bypass
