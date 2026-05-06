# [caddy] Add `allow_private_ips_for_allowed_hosts` Caddyfile directive

## Problem

When `BlockPrivateIPs` is on and `AllowedHosts` contains an internal host on a private IP, the include is still blocked at the dial level. Operators need to allow private IP access ONLY for trusted hosts.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    AllowPrivateIPsForAllowedHosts bool `json:"allow_private_ips_for_allowed_hosts,omitempty"`
}
```

### 2. Parse

```go
case "allow_private_ips_for_allowed_hosts":
    m.AllowPrivateIPsForAllowedHosts = true
```

### 3. Map

```go
config.AllowPrivateIPsForAllowedHosts = m.AllowPrivateIPsForAllowedHosts
```

### Caddyfile

```
mesi {
    block_private_ips true
    allowed_hosts backend.internal
    allow_private_ips_for_allowed_hosts
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: present → true, absent → false
- [ ] **Docs** — Add directive to README with security warning (DNS control assumption)
- [ ] **Docs** — Document: only effective when BOTH `block_private_ips` and `allowed_hosts` are set
- [ ] **Functional tests** — Caddy integration test:
  - `allow_private_ips_for_allowed_hosts` + `allowed_hosts backend` + include to private IP (DNS→backend) → succeeds
  - Directive absent + same config → private IP blocked
  - Host NOT in `allowed_hosts` → private IP still blocked
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked until core `ssrf.go` implements the bypass logic

## Notes

- Boolean flag in Caddyfile (no argument). Present = enabled, absent = disabled.
- Security: trusts DNS for `allowed_hosts`. Only use with internal/controlled DNS.
