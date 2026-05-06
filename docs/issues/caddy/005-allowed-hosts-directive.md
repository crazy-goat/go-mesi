# [caddy] Add `allowed_hosts` Caddyfile directive

## Problem

`servers/caddy/mesi.go` does not set `AllowedHosts` on `EsiParserConfig`. Every `<esi:include>` URL passes host validation unconditionally (subject to `BlockPrivateIPs`).

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    AllowedHosts []string `json:"allowed_hosts,omitempty"`
}
```

### 2. Parse

```go
case "allowed_hosts":
    m.AllowedHosts = d.RemainingArgs()
```

`RemainingArgs()` consumes all remaining tokens on the line as `[]string`. Caddyfile:

```
mesi {
    allowed_hosts backend.internal cdn.example.com api.trusted.org
}
```

### 3. Map

```go
config.AllowedHosts = m.AllowedHosts
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `allowed_hosts host1 host2` → `["host1", "host2"]`
- [ ] **Tests** — Unit test: absent → nil (no restriction)
- [ ] **Docs** — Add directive to README with subdomain matching semantics
- [ ] **Functional tests** — Caddy integration test:
  - `allowed_hosts backend` → include from `backend` works, from `evil.com` blocked
  - `allowed_hosts backend` → include from `sub.backend` works (subdomain suffix match)
  - Absent → all hosts allowed
- [ ] **Changelog** — Entry

## Notes

- Host matching: exact match or subdomain suffix (`sub.example.com` matches `example.com`). NOT suffix-injection safe for `attacker-example.com`.
- `RemainingArgs()` collects space-separated tokens. Works naturally with Caddyfile syntax.
