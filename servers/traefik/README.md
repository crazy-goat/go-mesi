# ESI middleware for traefik
A lightweight implementation of Edge Side Includes (ESI) middleware for Traefik

## Installation

Add `mesi` plugin in main `traefik.yaml` configuration file
```yaml
experimental:
  plugins:
    mesi:
      modulename: https://github.com/crazy-goat/go-mesi
      version: v0.1
```

## Configuration

Add `mesi` plugin to http middleware and add it to specific server:
```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          maxDepth: 5
          sharedHTTPClient: true

  routers:
    test-server:
      middlewares:
        - mesi
      service: test-server
      # more config here

  services:
    test-server:
    # some service config here
```

## Include Error Marker

When `includeErrorMarker` is set, the specified string is rendered in place of
a failed `<esi:include>` when no `onerror="continue"` and no fallback body is
present. Default: empty string (silent — failed includes produce no output).

**SECURITY**: Never include raw error messages or URLs in the marker.

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          includeErrorMarker: "<!-- ESI_ERROR -->"
```

## Shared HTTP Client

When `sharedHTTPClient` is enabled, a shared `http.Transport` with SSRF protection
is created once and reused for all ESI include requests. This enables TCP connection
pooling (keep-alive), dramatically reducing latency for pages with multiple includes
to the same backend origin.

Without this option, each `<esi:include>` creates a fresh `http.Client` + `http.Transport`,
incurring N × (TCP connect + TLS handshake) overhead.

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          sharedHTTPClient: true
```

## Timeout

`timeout` bounds every `<esi:include>` fetch of a single page render —
end-to-end (redirect chain + body read), not the client-facing response.

- **Format:** Go duration string — `"5s"`, `"1.5s"`, `"1m"`, `"1h30m"`.
  Plain integers (`15`) are rejected (missing unit), exactly like Caddy's
  `timeout`.
- **Default:** `"10s"` when the option is absent — this plugin's budget
  since the initial middleware commit (10 s from day one: hardcoded in
  the core fetch path until #48 moved the literal into `ServeHTTP`; the
  same 10 s as Caddy's default and
  `mesi.CreateDefaultConfig()`; libgomesi's C entry points default to
  30 s, which does not apply to this Go-direct plugin).
- **Range:** `[1s, 24h]` (`86400s`), mirroring libgomesi's
  `config.MaxTimeoutSeconds` / Apache `MesiTimeout` / nginx
  `mesi_timeout`.
- **Reject behavior:** a malformed or out-of-range EXPLICIT value fails
  middleware creation with an error naming `timeout` — never a silent
  fallback to the default. In particular `""` (explicit empty), `"abc"`,
  `"0s"`, sub-second values below the floor (`"500ms"`, `"999ms"`) and
  negatives are rejected: `Timeout <= 0` makes every include
  fail immediately with `ErrTimeBudgetExceeded` in the core — `0` does
  not mean "unlimited". A bare `timeout:` (YAML null) is treated as
  absent and gets the documented `"10s"` default; only a non-null
  explicit value (including `""`) is validated.

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          # Tight SLA: abort slow fragment fetches at 2s — the fragment
          # renders as fallback/empty instead of holding the request.
          timeout: "2s"
```

## Response Size

`maxResponseSize` caps the HTTP response body size, in bytes, of a single
`<esi:include>` fetch. Until #210 the Traefik plugin had no way to
control this limit.

- **Format:** plain integer — bytes (no `k`/`m`/`g` suffixes, exactly
  like Apache `MesiMaxResponseSize`, nginx `mesi_max_response_size`,
  the PHP extension `max_response_size` and the CLI
  `-max-response-size`).
- **Default / absent:** `0` = **unlimited** when the option is absent —
  byte-identical to previous behaviour: `ServeHTTP` is Go-direct — it
  builds `mesi.EsiParserConfig` itself and never set the field, so it
  stayed at its zero value `0`, which the core treats as "no limit"
  (`mesi/fetch.go` only limits when `MaxResponseSize > 0`). There is
  **no implicit 10 MB default on this path** — the
  `10 * 1024 * 1024` of `mesi.CreateDefaultConfig()` only reaches Go
  callers of that constructor, which this plugin never is (the same
  premise correction as #169 / #201 / #208; see the `timeout` section
  above for the same Go-direct note).
- **Range:** `[0, 9223372036854775806]` bytes (`math.MaxInt64 - 1`, the
  same cap as Apache `MesiMaxResponseSize`, nginx
  `mesi_max_response_size` (#208), the PHP extension's
  `max_response_size` (#201) and the CLI `-max-response-size` (#186)).
  The upper bound exists because the core computes
  `MaxResponseSize + 1` for its `io.LimitReader` (`mesi/fetch.go`): at
  `math.MaxInt64` that wraps negative and the include would silently
  render an empty body instead of failing (#448).
- **Scope:** **per SINGLE include, not per page** — a page with 10
  includes each under the limit can total far more than the limit. An
  over-limit include **fails closed** through the include-error path
  (the empty `includeErrorMarker`, a fallback `<esi:include>` body, or
  `onerror="continue"`) — never a truncated body.
- **`0` is accepted and means "unlimited"** — the documented core
  contract shared with Caddy `max_response_size 0`, Apache
  `MesiMaxResponseSize 0` and nginx's `mesi_max_response_size 0` (#208).
  **Memory-exhaustion risk:** with `0` (and with the option absent) a
  single `<esi:include>` pointing at an unbounded backend can exhaust
  Traefik's memory — set a cap wherever the backend is not fully
  trusted.
- **Reject behavior:** a negative (`-1`, `-5`) or out-of-range
  (`9223372036854775807` = `math.MaxInt64`) EXPLICIT value fails
  middleware creation with an error naming `maxResponseSize` — never a
  silent fallback to a default (a negative would silently behave as
  "unlimited" on the core's `> 0` check). Values an `int64` cannot
  represent (`9223372036854775808`+) and non-integers never reach
  `New()`: the config decode into the typed field fails first, which
  also fails middleware creation.

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          # Fragments over 1 MB fail their fetch and render as the
          # fallback body / empty marker instead of the body.
          maxResponseSize: 1048576
```

## Max Concurrent Requests

`maxConcurrentRequests` caps how many `<esi:include>` HTTP fetches may run at
the same time during a single ESI parse (one page render). Until #215 the
Traefik plugin had no way to configure this limit.

- **Format:** plain integer — concurrent fetches (no suffixes, exactly like
  Apache `MesiMaxConcurrentRequests`, nginx `mesi_max_concurrent_requests`,
  the PHP extension `max_concurrent_requests` and the CLI
  `-max-concurrent-requests`).
- **Default / absent:** `0` = **unlimited** — byte-identical to previous
  behaviour: `ServeHTTP` is Go-direct — it builds `mesi.EsiParserConfig`
  itself and never set the field, so it stayed at its zero value `0`, and
  the core only installs the admission-control semaphore when the value is
  `> 0` (`mesi/parser.go:78`). There is no hidden default on this path
  (`mesi.CreateDefaultConfig()` never sets the field either —
  `mesi/config.go`).
- **Range:** `[0, 999999999]` — the same cap as Apache
  `MesiMaxConcurrentRequests` (#170), nginx `mesi_max_concurrent_requests`
  (#214), the PHP extension's `max_concurrent_requests` (#206) and the CLI
  `-max-concurrent-requests` (#192) — libgomesi's
  `config.MaxMaxConcurrentRequests`. The bound is transport-derived (a C
  `int` + the shared 9-digit strict parsers) — neither the core nor Caddy
  caps the value (Caddy's uncapped `strconv.Atoi` belongs to the #452 gap
  family and is deliberately not inherited).
- **Scope: per-page-render, NOT Traefik-global.** The limit applies within
  ONE `mesi.MESIParse` call — one page render. Each concurrent request's own
  parse builds its own admission semaphore, so with 4 Traefik workers each
  rendering a page under `maxConcurrentRequests: 5`, total outbound ESI
  connections can reach 20 (4 × 5). Includes queued beyond the cap **wait**
  for a free slot (bounded by the `timeout` fetch budget — the admission
  wait shares the same deadline, `mesi/fetch.go`); they are never dropped.
- **`0` is accepted and means "unlimited"** — the documented core contract
  shared with Apache `MesiMaxConcurrentRequests 0` (#170), Caddy
  `max_concurrent_requests 0`, the PHP extension (#206) and the CLI (#192).
- **Reject behavior:** a negative (`-1`, `-5`) or out-of-range
  (`1000000000`) EXPLICIT value fails middleware creation with an error
  naming `maxConcurrentRequests` — never a silent fallback to the default
  (the core would only warn `max_concurrent_requests_invalid` and normalize
  a negative to `0` = unlimited, #329, so a malformed explicit value must
  never silently pass as the documented "unlimited" — the same config-load
  rejection as every landed sibling). Values an `int` cannot represent
  (20-digit overflow) and non-integers (`1.5`, `"abc"`) never reach
  `New()`: the config decode into the typed field fails first, which also
  fails middleware creation.

> **Known core limitation (#453):** nested `MESIParse` calls (a fetched
> fragment containing further includes) replace the inherited semaphore with
> a fresh one, so with NESTED includes the effective cap can reach cap ×
> nesting depth for one page render. Flat pages (the common case) are capped
> exactly; #453 is tracked separately and is not fixed by this option.

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          # At most 3 concurrent include fetches per page render —
          # queued includes wait for a slot (never dropped).
          maxConcurrentRequests: 3
```

## Allowed Hosts (SSRF whitelist)

When `allowedHosts` is set, only ESI include destinations whose host is listed
(or is a subdomain of a listed host) are fetched. This is the **effective SSRF
control** for the Traefik plugin: because the plugin runs under Yaegi, the
dial-time private-IP blocking transport is stubbed, so URL-level host
restriction is what actually prevents includes to arbitrary/internal hosts.

Matching rules:

- **Exact match** — `backend.internal` matches `backend.internal`.
- **Subdomain suffix** — `example.com` matches `sub.example.com` (and any
  deeper subdomain). The `.` boundary prevents suffix injection: `evil.com`
  does **not** match `example.com`, and `notexample.com` does **not** match
  `example.com`.
- **Empty list** (default) allows all hosts, subject to `blockPrivateIPs`
  (backward compatible).

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          allowedHosts:
            - backend.internal
            - cdn.trusted.com
```

**SECURITY**: Always set `allowedHosts` in untrusted environments. Without it,
any `<esi:include src>` URL is fetched unconditionally.

### Private-IP bypass for whitelisted hosts

`allowPrivateIPsForAllowedHosts` lets hosts listed in `allowedHosts` resolve to
private/reserved IP addresses even when `blockPrivateIPs` is enabled:

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          blockPrivateIPs: true
          allowedHosts:
            - backend.internal
          allowPrivateIPsForAllowedHosts: true
```

- Default is `false` (no bypass — backward compatible).
- Only effective when BOTH `blockPrivateIPs` is `true` AND `allowedHosts` is
  non-empty; otherwise a no-op. Unlisted hosts and empty allowlists can never
  bypass (fail closed) — the whitelist check runs before any dial.
- **No effect under `sharedHTTPClient`**: the shared transport bakes
  `blockPrivateIPs` at startup, so the bypass is not consulted for
  shared-client fetches.
- **Yaegi note**: under the interpreted plugin the dial-time IP-blocking
  transport is stubbed (see `servers/traefik/Dockerfile`), so the bypass is not
  observable in functional tests — the unit tests exercise the real Go path.
  URL-level `allowedHosts` remains the effective SSRF control.
- **SECURITY**: the bypass **trusts DNS** for hosts in `allowedHosts` — only
  use it with internal DNS (Consul, Kubernetes DNS, `/etc/hosts`).

## Cache Backend

The plugin supports multiple cache backends for ESI fragment caching:

### Memory Cache

In-memory LRU cache with configurable size and TTL:
```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          maxDepth: 5
          cacheBackend: memory
          cacheSize: 10000
          cacheTTL: "60s"
```

### Redis Cache

Redis-backed cache for sharing ESI fragments across Traefik instances:
```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          maxDepth: 5
          cacheBackend: redis
          cacheTTL: "120s"
          cacheRedisAddr: "10.0.0.5:6379"
          cacheRedisPassword: "your-password"
          cacheRedisDb: 0
```

### Memcached Cache

Memcached-backed cache for distributed ESI fragment caching:
```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          maxDepth: 5
          cacheBackend: memcached
          cacheTTL: "120s"
          cacheMemcachedServers:
            - "10.0.0.1:11211"
            - "10.0.0.2:11211"
```

#### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `maxDepth` | int | `5` | Maximum ESI recursion depth. Omit for the default. Explicit `0` is passthrough (no ESI fetch). Values outside `[0, 10000]` are rejected. |
| `timeout` | string | `"10s"` | Per-include fetch budget as a Go duration (e.g. `"5s"`, `"1m"`); range `[1s, 24h]`. Malformed or out-of-range explicit values fail middleware creation (no silent default). |
| `maxResponseSize` | int64 | `0` (unlimited) | Per-include response body cap in **bytes** (per SINGLE include, not per page; over-limit includes fail closed through the include-error path — never truncated); range `[0, 9223372036854775806]`. Absent = unlimited (no implicit 10 MB). Negative / `MaxInt64` explicit values fail middleware creation; above-`int64` values fail the config decode (no silent default). |
| `maxConcurrentRequests` | int | `0` (unlimited) | Concurrent `<esi:include>` fetch cap per **page render** (one `MESIParse` — not Traefik-global; each concurrent request's own parse builds its own semaphore, 4 workers × 5 → up to 20 outbound); range `[0, 999999999]`. Absent = unlimited. Includes beyond the cap **wait** for a slot (bounded by `timeout`), never dropped. Negative / `1000000000` explicit values fail middleware creation; non-integers / overflow fail the config decode (no silent default). Known core limitation: nested includes can reach cap × depth (#453) |
| `sharedHTTPClient` | bool | `false` | Enable shared HTTP client for connection pooling |
| `includeErrorMarker` | string | `""` | String rendered for failed includes (empty = silent) |
| `cacheBackend` | string | `""` | Cache backend: `""` (off), `memory`, `redis`, `memcached` |
| `cacheTTL` | string | `""` | Cache TTL as Go duration (e.g., `"60s"`, `"5m"`) |
| `cacheSize` | int | `10000` | Max entries for memory cache |
| `cacheRedisAddr` | string | `"localhost:6379"` | Redis server address |
| `cacheRedisPassword` | string | `""` | Redis AUTH password |
| `cacheRedisDb` | int | `0` | Redis database number |
| `cacheMemcachedServers` | []string | `[]` | Memcached server addresses (host:port) |
| `allowedHosts` | []string | `[]` | ESI include host whitelist (exact or subdomain-suffix match); empty = allow all |
| `allowPrivateIPsForAllowedHosts` | bool | `false` | Let `allowedHosts` entries resolve to private/reserved IPs when `blockPrivateIPs` is on (trusts DNS; no effect under `sharedHTTPClient`) |
| `cacheKeyTemplate` | string | `""` | Custom cache key template: `${url}`, `${header:Name}`, `${cookie:Name}`; unknown placeholders left literal; empty = default URL-only key |

### Custom cache key template

Customize cache keys with placeholders substituted from the incoming request (mirrors Caddy `cache_key_template` and RoadRunner `cache_key_template`, backed by `mesi.BuildCacheKey`):

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          cacheBackend: memory
          cacheTTL: "60s"
          cacheKeyTemplate: "mesi:${url}:lang=${header:Accept-Language}"
```

| Placeholder | Substituted with |
|---|---|
| `${url}` | Full URL of the `<esi:include>` |
| `${header:Name}` | Request header `Name` (case-insensitive) |
| `${cookie:Name}` | Request cookie `Name` (case-insensitive) |

- Unknown placeholders (e.g. `${unknown:foo}`) are left literal — no error.
- Empty / absent `cacheKeyTemplate` = default URL-only key (`mesi.DefaultCacheKey`).
- **Warning:** a template without `${url}` collapses all include URLs to a single cache key — different URLs will share the same cached body (cross-URL collision). Always include `${url}` unless you intentionally want one entry for every URL.

#### Redis Features


- **Cache sharing**: Share ESI fragments across multiple Traefik instances
- **Persistence**: Cache survives Traefik restarts
- **TTL support**: Automatic expiration of cached entries
- **Connection pooling**: Managed by go-redis library

#### Redis Key Format

Cached entries are stored with key format: `mesi:<url>` when no template is set.

With `cacheKeyTemplate` the key is the rendered template result (e.g. `pfx:http://backend/fragment:sfx`), plus an SSRF-policy fingerprint suffix (see `mesi/fetch.go:200`) so different policies never share a cache entry.

Example: `mesi:http://backend/fragment`

#### Redis Connection Failure

When Redis is unreachable, the plugin continues to work in degraded mode:
- ESI processing continues without caching
- Origin server is hit for each request
- When Redis becomes available, caching resumes

#### Memcached Features

- **Distributed cache**: Share ESI fragments across multiple Traefik instances
- **Consistent hashing**: Cache is distributed across multiple Memcached servers
- **Lightweight**: Simpler than Redis for simple key-value workloads
- **TTL support**: Automatic expiration of cached entries

#### Memcached Limitations

- **1 MB value size limit**: ESI includes larger than 1 MB cannot be cached
- **No TLS support**: Use a sidecar proxy (e.g., stunnel) for encrypted connections
- **Server format**: `host:port` separated by spaces or YAML list items

## Development

### Yaegi Compatibility

This plugin runs inside Traefik's embedded [Yaegi](https://github.com/traefik/yaegi)
Go interpreter (v0.16.1). Yaegi has several limitations that affect which Go
features can be used in the plugin source code:

| Limitation | Workaround | Issue |
|---|---|---|
| `for range N` (Go 1.22+) panics | Use `for i := 0; i < N; i++` | [#1701](https://github.com/traefik/yaegi/issues/1701) |
| `math/rand/v2` not supported | Use `math/rand` instead | [#1674](https://github.com/traefik/yaegi/issues/1674) |
| `syscall` / `unsafe` not supported | Dialer code in `ssrf_dialer.go` excluded from build | — |
| Build tags ignored by Yaegi | Problematic files removed in Dockerfile | — |
| `min`/`max` builtins (Go 1.21+) | Not used | [#1674](https://github.com/traefik/yaegi/issues/1674) |
| `nil type` panic in complex packages | Avoid combinations that trigger it | [#1636](https://github.com/traefik/yaegi/issues/1636) |

**Impact**: The Traefik plugin does not support Redis or Memcached cache backends
(they depend on third-party packages with `unsafe` usage). Only the in-memory
cache backend is available. Dial-time SSRF protection (private IP blocking at
TCP connect) is also not available; URL-level protection (allowed hosts) still
works.

When modifying the `mesi/` package, verify changes with the Yaegi compatibility
test before pushing (no Docker needed, completes in seconds):

```bash
# Quick Yaegi compatibility check (standalone tool)
go run ./servers/traefik/yaegi-check/

# Or via Go test
go test -run TestYaegiCompatibility -v -count=1 ./servers/traefik/
```

The test sets up a temporary GOPATH, copies the plugin sources (excluding
files that are known to be incompatible: `ssrf_dialer.go`, `cache_redis/`,
`cache_memcached/`, test files), and uses Yaegi to import the `mesi/` package.
Any regression (e.g. `for range N`, `math/rand/v2`, `syscall`) will cause a
clear test failure instead of a cryptic "nil type" panic in the Docker-based
integration test.

### Building

```bash
# Default (memory only)
go build ./...

# With Redis support
go build -tags redis ./...

# With Memcached support
go build -tags memcached ./...

# With both Redis and Memcached
go build -tags "redis,memcached" ./...
```

### Testing

```bash
# Run unit tests (no external dependencies)
go test -v ./...

# Run tests with Redis (requires Redis running)
go test -tags redis -v ./...

# Run tests with Memcached (requires Memcached running)
go test -tags memcached -v ./...

# Run integration tests (requires Redis/Memcached running)
go test -tags redis -v -run TestCacheIntegration ./...
```