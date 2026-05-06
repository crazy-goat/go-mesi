# [roadrunner] Add `shared_http_client` config option

## Problem

`EsiParserConfig.HTTPClient` is `nil` — each include creates fresh `http.Client`. No TCP connection reuse.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    SharedHTTPClient bool `mapstructure:"shared_http_client"`
}
```

### 2. Init transport in Plugin.Init()

```go
type Plugin struct {
    // ...
    sharedTransport *http.Transport
}

func (p *Plugin) Init(cfg config.Configurer) error {
    // ... read config ...
    if p.config.SharedHTTPClient {
        bp := true
        if p.config.BlockPrivateIPs != nil {
            bp = *p.config.BlockPrivateIPs
        }
        p.sharedTransport = mesi.NewSSRFSafeTransport(mesi.EsiParserConfig{
            BlockPrivateIPs: bp,
        })
    }
    return nil
}
```

### 3. Map in Middleware()

```go
if p.sharedTransport != nil {
    config.HTTPClient = &http.Client{
        Transport: p.sharedTransport,
        Timeout:   timeout,
    }
}
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      shared_http_client: true
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `Init()` creates SSRF-safe transport
- [ ] **Tests** — Unit test: absent → no shared client
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Integration test: reduced TCP connections, respects `block_private_ips`
- [ ] **Changelog** — Entry
