# [traefik] Add `allowPrivateIPsForAllowedHosts` plugin config option

## Problem

When `blockPrivateIPs: true` and `allowedHosts` contains internal hosts on private IPs, includes are still blocked. Need granular bypass.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    AllowPrivateIPsForAllowedHosts bool `json:"allowPrivateIPsForAllowedHosts" yaml:"allowPrivateIPsForAllowedHosts"`
}
```

### 2. Map

```go
config.AllowPrivateIPsForAllowedHosts = p.config.AllowPrivateIPsForAllowedHosts
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          blockPrivateIPs: true
          allowedHosts:
            - backend.internal
          allowPrivateIPsForAllowedHosts: true
```

## Acceptance criteria

- [ ] **Tests** — `true` / `false`, absent → `false`
- [ ] **Docs** — Add to README with DNS-control security warning
- [ ] **Functional tests** — Bypass works for listed host, not for unlisted
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked until core `ssrf.go` implements bypass
