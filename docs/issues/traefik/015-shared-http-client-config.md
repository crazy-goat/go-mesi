# [traefik] Add `sharedHTTPClient` plugin config option

## Problem

`EsiParserConfig.HTTPClient` is `nil` — each include creates fresh `http.Client`. No TCP connection reuse.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    SharedHTTPClient bool `json:"sharedHTTPClient" yaml:"sharedHTTPClient"`
}
```

### 2. Init transport in New()

```go
type ResponsePlugin struct {
    // ...
    sharedTransport *http.Transport
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
    // ...
    if config.SharedHTTPClient {
        bp := true
        if config.BlockPrivateIPs != nil {
            bp = *config.BlockPrivateIPs
        }
        p.sharedTransport = mesi.NewSSRFSafeTransport(mesi.EsiParserConfig{
            BlockPrivateIPs: bp,
        })
    }
    return p, nil
}
```

### 3. Map in ServeHTTP()

```go
if p.sharedTransport != nil {
    config.HTTPClient = &http.Client{
        Transport: p.sharedTransport,
        Timeout:   timeout,
    }
}
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          sharedHTTPClient: true
```

## Acceptance criteria

- [ ] **Tests** — `New()` creates SSRF-safe transport when enabled
- [ ] **Tests** — absent → no shared client
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Reduced TCP connections, respects `blockPrivateIPs`
- [ ] **Changelog** — Entry
