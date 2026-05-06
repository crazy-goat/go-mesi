# [cli] Add `-allowPrivateIPsForAllowedHosts` flag

## Problem

When `BlockPrivateIPs` is true and hosts in `AllowedHosts` resolve to private IPs, includes are still blocked. Need granular bypass.

## Proposed solution

### Flag

```go
var allowPrivateForAllowed = flag.Bool("allowPrivateIPsForAllowedHosts", false,
    "Allow private IP access for hosts in -allowedHosts")
```

### Map

```go
config.AllowPrivateIPsForAllowedHosts = *allowPrivateForAllowed
```

### Usage

```bash
mesi-cli -allowedHosts "backend.internal" -allowPrivateIPsForAllowedHosts input.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `true` / `false`, default `false`
- [ ] **Docs** — Add to README with DNS-control security warning
- [ ] **Functional tests** — CLI test: bypass works for listed host, not for unlisted
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked on core `ssrf.go` bypass
