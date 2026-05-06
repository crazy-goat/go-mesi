# [apache] Add `MesiSharedHTTPClient` directive

## Problem

`EsiParserConfig.HTTPClient` when `nil` means each `<esi:include>` fetch creates a new `http.Client` + `http.Transport`. No TCP connection reuse, no keep-alive, N × TCP+TLS overhead for pages with multiple includes to the same backend.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    int shared_http_client;  // -1=unset, 0=off, 1=on
} mesi_config;
```

### 2. Add directive

```c
AP_INIT_FLAG("MesiSharedHTTPClient", set_shared_http_client, NULL, RSRC_CONF,
    "Share HTTP client across ESI includes for connection pooling (default: Off)"),
```

### 3. libgomesi InitHTTPClient (dependency)

```go
var sharedTransport *http.Transport
var sharedClient *http.Client

//export InitHTTPClient
func InitHTTPClient(blockPrivateIPs C.int) {
    config := mesi.EsiParserConfig{BlockPrivateIPs: blockPrivateIPs != 0}
    sharedTransport = mesi.NewSSRFSafeTransport(config)
    sharedClient = &http.Client{Transport: sharedTransport, Timeout: 30 * time.Second}
}
```

Called in `mesi_child_init`:

```c
InitHTTPClientFunc InitHTTPClient = dlsym(go_module, "InitHTTPClient");
if (InitHTTPClient && conf->shared_http_client) {
    int bp = (conf->block_private_ips != -1) ? conf->block_private_ips : 1;
    InitHTTPClient(bp);
}
```

### Apache config

```apache
MesiSharedHTTPClient On
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `On` / `Off`, default `Off`
- [ ] **Tests** — Unit test `InitHTTPClient` creates transport with SSRF protection
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Integration test:
  - `MesiSharedHTTPClient On` + 10 includes to same backend → reduced TCP connections (via `ss -tn`)
  - `MesiSharedHTTPClient Off` → per-include connections (backward compat)
  - Shared client respects `MesiBlockPrivateIPs` setting
  - Stress test: `ab -n 1000` → no fd exhaustion
- [ ] **Changelog** — Entry

## Notes

- Same architectural requirement as cache: needs libgomesi `InitHTTPClient`.
- The shared transport uses `NewSSRFSafeTransport` for dial-level SSRF blocking.
- If `MesiBlockPrivateIPs` is changed via config, the transport must be recreated. Simplest: document that `MesiSharedHTTPClient` uses the server-level `MesiBlockPrivateIPs` setting at startup.
- Per-call `Timeout` override: copy the shared client and replace `Timeout` per-`ParseJson` call.
- Go's default connection pool: 100 idle per host, 90s idle timeout. Reasonable for most deployments.
