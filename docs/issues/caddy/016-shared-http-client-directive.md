# [caddy] Add `shared_http_client` Caddyfile directive

## Problem

`EsiParserConfig.HTTPClient` is `nil` — each `<esi:include>` creates a fresh `http.Client` + `http.Transport`. No TCP connection reuse, N × TCP+TLS overhead for multi-include pages.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    SharedHTTPClient bool             `json:"shared_http_client,omitempty"`
    sharedTransport  *http.Transport  `json:"-"`
}
```

### 2. Parse

```go
case "shared_http_client":
    m.SharedHTTPClient = true
```

### 3. Init in Provision()

```go
func (m *MesiMiddleware) Provision(ctx caddy.Context) error {
    if m.SharedHTTPClient {
        bp := true
        if m.BlockPrivateIPs != nil {
            bp = *m.BlockPrivateIPs
        }
        m.sharedTransport = mesi.NewSSRFSafeTransport(mesi.EsiParserConfig{
            BlockPrivateIPs: bp,
        })
    }
    return nil
}
```

### 4. Map in ServeHTTP

```go
if m.sharedTransport != nil {
    config.HTTPClient = &http.Client{
        Transport: m.sharedTransport,
        Timeout:   timeout,  // per-call timeout override
    }
}
```

### Caddyfile

```
mesi {
    shared_http_client
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `Provision()` creates SSRF-safe transport
- [ ] **Tests** — Unit test: absent → no shared client (per-request clients)
- [ ] **Tests** — Unit test: per-call timeout override works with shared client
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Caddy integration test:
  - `shared_http_client` + 10 includes to same origin → reduced TCP connections (`ss -tn`)
  - Absent → per-include connections (backward compat)
  - Stress: `hey -c 50 -n 1000` → no fd exhaustion
  - Shared transport respects `block_private_ips` setting
- [ ] **Changelog** — Entry

## Notes

- `Provision()` is called once at config load. Transport is reused for all requests.
- Caddy handles config reloads gracefully — `Provision()` re-runs, recreating the transport.
- Go's default connection pool: 100 idle per host, 90s idle timeout. Reasonable defaults.
- `NewSSRFSafeTransport` applies dial-level SSRF blocking. AllowedHosts check is per-include and unaffected.
- Per-call `Timeout` override: copy the shared client struct, replace `Timeout`, use for this `MESIParse` call.
