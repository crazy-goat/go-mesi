# [roadrunner] Add `allowed_hosts` config option

## Problem

No `AllowedHosts` on `EsiParserConfig`. Every include URL passes host validation unconditionally.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    AllowedHosts []string `mapstructure:"allowed_hosts"`
}
```

### 2. Map in Middleware()

```go
config.AllowedHosts = p.config.AllowedHosts
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      allowed_hosts:
        - backend.internal
        - cdn.example.com
```

## Acceptance criteria

- [ ] **Tests** — YAML list → `[]string` deserialization, absent → nil
- [ ] **Docs** — Add to README with subdomain matching semantics
- [ ] **Functional tests** — Integration test: `allowed_hosts: [backend]` → include from `backend` works, from `evil.com` blocked
- [ ] **Changelog** — Entry
