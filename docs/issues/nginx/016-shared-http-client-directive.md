# [nginx] Add shared HTTP client directive

## Problem

`mesi.EsiParserConfig.HTTPClient` (`mesi/config.go:33`) accepts a shared `*http.Client` for connection pooling:

```go
HTTPClient *http.Client  // nil = create per request
```

When `nil`, each `<esi:include>` fetch creates a new `http.Client` with a new `http.Transport`. This means:

- **No TCP connection reuse** — each include performs a fresh TCP connect + TLS handshake.
- **No keep-alive** — connections are closed after each fetch.
- **Excessive latency** — a page with 10 includes to the same backend pays 10× TCP+TLS overhead.

When a shared `http.Client` is provided, Go's `http.Transport` pools idle connections (default: 100 per host, 90s idle timeout), dramatically reducing latency for multiple includes to the same origin.

The nginx module (via libgomesi) creates config per-call, so `HTTPClient` is always `nil`.

## Impact

- Multi-include pages to the same backend experience N × (TCP connect + TLS handshake) latency.
- Ephemeral port consumption under load — each ESI include consumes a new source port.
- No connection pooling across includes in a single page render.

## Context

This works in CGo context — the shared `http.Client` is a Go struct pointer that can be initialized once at libgomesi load time and reused across calls.

**Same architectural constraint as cache (#012)**: requires libgomesi-level persistent state. The `HTTPClient` (or its `Transport`) must be created once and shared.

### SSRF-safe transport

When providing a custom `HTTPClient`, operators must use `mesi.NewSSRFSafeTransport` to maintain SSRF protection:

```go
transport := mesi.NewSSRFSafeTransport(config)
client := &http.Client{Transport: transport, Timeout: config.Timeout}
```

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    ngx_flag_t  shared_http_client;
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_shared_http_client"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
 ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, shared_http_client), NULL},
```

### 3. Default and merge

```c
ngx_conf_merge_value(conf->shared_http_client, prev->shared_http_client, 0);  // default off
```

### 4. libgomesi persistent HTTP client

```go
var sharedTransport *http.Transport
var sharedClient *http.Client

//export InitHTTPClient
func InitHTTPClient(blockPrivateIPs C.int) {
    config := mesi.EsiParserConfig{
        BlockPrivateIPs: blockPrivateIPs != 0,
    }
    sharedTransport = mesi.NewSSRFSafeTransport(config)
    sharedClient = &http.Client{
        Transport: sharedTransport,
        Timeout:   30 * time.Second,  // default, overridden per-call by config.Timeout
    }
}

// In ParseJson, if sharedClient exists, use it:
func ParseJson(input *C.char, jsonConfig *C.char) *C.char {
    // ... unmarshal jsonConfig ...
    config = buildConfig(cfg)
    if cfg.SharedHTTPClient && sharedClient != nil {
        client := *sharedClient  // copy
        client.Timeout = config.Timeout  // override per-call timeout
        config.HTTPClient = &client
    }
    // ...
}
```

### nginx module init

```c
static ngx_int_t ngx_http_mesi_thread_init(ngx_cycle_t *cycle) {
    // ... dlopen, dlsym ...

    InitHTTPClientFunc InitHTTPClient = (InitHTTPClientFunc)dlsym(go_module, "InitHTTPClient");
    if (InitHTTPClient) {
        // BlockPrivateIPs default is on (1). This transport respects the config.
        // If an operator sets block_private_ips off per-location, the transport
        // must be reinitialized — see Notes.
        InitHTTPClient(1);  // default: block private IPs
    }
    return NGX_OK;
}
```

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_shared_http_client on;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive: `on` / `off`, default `off`
- [ ] **Tests** — Unit test `InitHTTPClient` creates shared transport with SSRF protection
- [ ] **Tests** — Unit test that `ParseJson` with `sharedHttpClient:true` uses the persistent client
- [ ] **Tests** — Unit test that per-call `Timeout` override works correctly with shared client
- [ ] **Docs** — Add directive to README with connection pooling explanation and defaults
- [ ] **Functional tests** — nginx integration test:
  - `shared_http_client on`, page with 10 includes to same backend → verify reduced connection count (e.g., `ss -tn` shows fewer connections)
  - `shared_http_client off` → each include creates new connection (default behavior)
  - Verify that shared client respects `block_private_ips` setting
  - Stress test: `ab -n 1000` with shared client → no file descriptor exhaustion
- [ ] **Changelog** — Entry in project changelog

## Notes

- **Interaction with `block_private_ips`**: The shared transport is initialized once with a `BlockPrivateIPs` setting. If a location sets `mesi_block_private_ips off`, the shared transport must be recreated (or two transports maintained). Recommended approach: `InitHTTPClient` accepts the flag; nginx picks the right transport per-location based on config.
- **Transport-level SSRF**: The `NewSSRFSafeTransport` applies SSRF blocking at the TCP dial level. Even with a shared client, private IP blocking is always active. The `AllowedHosts` check (hostname level) is per-include and unaffected by the shared transport.
- **Connection pool parameters**: Go default: 100 idle connections per host, 90s idle timeout, unlimited total connections. These defaults are reasonable for most deployments. If tuning is needed, expose as separate directives.
- **Go `http.Transport` goroutine leak**: Idle connection cleanup goroutines in `http.Transport` are managed by the runtime. Ensure `InitHTTPClient` is called once per worker process (in `ngx_http_mesi_thread_init`) and not recreated on each config reload.
