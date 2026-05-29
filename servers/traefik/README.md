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

#### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `sharedHTTPClient` | bool | `false` | Enable shared HTTP client for connection pooling |
| `cacheBackend` | string | `""` | Cache backend: `""` (off), `memory`, `redis` |
| `cacheTTL` | string | `""` | Cache TTL as Go duration (e.g., `"60s"`, `"5m"`) |
| `cacheSize` | int | `10000` | Max entries for memory cache |
| `cacheRedisAddr` | string | `"localhost:6379"` | Redis server address |
| `cacheRedisPassword` | string | `""` | Redis AUTH password |
| `cacheRedisDb` | int | `0` | Redis database number |

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

## Development

### Building

```bash
go build -tags redis ./...
```

### Testing

```bash
# Run unit tests (requires Redis)
go test -tags redis -v ./...

# Run integration tests (requires Redis running)
go test -tags redis -v -run TestCacheIntegration ./...
```