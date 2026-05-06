# [caddy] Add `block_private_ips` Caddyfile directive

## Problem

`servers/caddy/mesi.go:48` hardcodes `BlockPrivateIPs` to `true` with no way to disable:

```go
BlockPrivateIPs: true,  // 🔒 hardcoded
```

Operators with legitimate internal ESI includes (service meshes, internal metadata services) cannot opt out of SSRF protection.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    BlockPrivateIPs *bool `json:"block_private_ips,omitempty"`
}
```

`*bool` to distinguish "not set" (nil → default true) from explicit "false".

### 2. Parse

```go
case "block_private_ips":
    if d.NextArg() {
        v, _ := strconv.ParseBool(d.Val())
        m.BlockPrivateIPs = &v
    }
```

Or boolean flag:
```
mesi {
    block_private_ips false
}
```

### 3. Use

```go
bp := true
if m.BlockPrivateIPs != nil {
    bp = *m.BlockPrivateIPs
}
config.BlockPrivateIPs = bp
```

### Caddyfile

```
mesi {
    block_private_ips false
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `block_private_ips false` → false, `block_private_ips true` → true, absent → default true
- [ ] **Docs** — Add directive to README with security note (default on is breaking change if upgrading)
- [ ] **Functional tests** — Caddy integration test:
  - `block_private_ips false` → include to `http://127.0.0.1/` succeeds
  - Directive absent (default) → include to `http://127.0.0.1/` blocked
- [ ] **Changelog** — Entry with BREAKING note

## Notes

- Changing from hardcoded-true to configurable-true is NOT breaking (same default). Making it configurable enables the operator to set `false`.
- `*bool` pattern is standard Go for nullable booleans in config structs.
- If `BlockPrivateIPs` is `false`, the `NewSSRFSafeTransport` dialer passes through all connections.
