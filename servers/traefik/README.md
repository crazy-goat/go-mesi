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
| `maxDepth` | int | `5` | Maximum ESI recursion depth |
| `sharedHTTPClient` | bool | `false` | Enable shared HTTP client for connection pooling |
| `includeErrorMarker` | string | `""` | String rendered for failed includes (empty = silent) |
| `cacheBackend` | string | `""` | Cache backend: `""` (off), `memory`, `redis`, `memcached` |
| `cacheTTL` | string | `""` | Cache TTL as Go duration (e.g., `"60s"`, `"5m"`) |
| `cacheSize` | int | `10000` | Max entries for memory cache |
| `cacheRedisAddr` | string | `"localhost:6379"` | Redis server address |
| `cacheRedisPassword` | string | `""` | Redis AUTH password |
| `cacheRedisDb` | int | `0` | Redis database number |
| `cacheMemcachedServers` | []string | `[]` | Memcached server addresses (host:port) |

#### Redis Features

- **Cache sharing**: Share ESI fragments across multiple Traefik instances
- **Persistence**: Cache survives Traefik restarts
- **TTL support**: Automatic expiration of cached entries
- **Connection pooling**: Managed by go-redis library

#### Redis Key Format

Cached entries are stored with key format: `mesi:<url>`

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