# [traefik] Add `allowedHosts` plugin config option

## Problem

No `AllowedHosts` on `EsiParserConfig`. Every include URL passes host validation.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    AllowedHosts []string `json:"allowedHosts" yaml:"allowedHosts"`
}
```

### 2. Map

```go
config.AllowedHosts = p.config.AllowedHosts
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          allowedHosts:
            - backend.internal
            - cdn.example.com
```

## Acceptance criteria

- [ ] **Tests** — YAML list → `[]string`, absent → nil
- [ ] **Docs** — Add to README with subdomain matching semantics
- [ ] **Functional tests** — `allowedHosts: [backend]` → include from `backend` works, `evil.com` blocked
- [ ] **Changelog** — Entry
