# [caddy] Add `cache_backend memory` Caddyfile directive

## Problem

`EsiParserConfig.Cache` is `nil` — no caching. `mesi.NewMemoryCache(maxSize)` provides in-memory LRU cache but Caddy has no directive.

## Proposed solution

### 1. Add fields

```go
type MesiMiddleware struct {
    // ...
    CacheBackend string `json:"cache_backend,omitempty"`  // "" (off), "memory", "redis", "memcached"
    CacheSize    int    `json:"cache_size,omitempty"`     // max entries
    CacheTTL     string `json:"cache_ttl,omitempty"`      // e.g. "30s"

    cache    mesi.Cache       `json:"-"`
    cacheTTL time.Duration    `json:"-"`
}
```

### 2. Parse

```go
case "cache_backend":
    if !d.NextArg() { return d.ArgErr() }
    m.CacheBackend = d.Val()
case "cache_size":
    if !d.NextArg() { return d.ArgErr() }
    m.CacheSize, _ = strconv.Atoi(d.Val())
case "cache_ttl":
    if !d.NextArg() { return d.ArgErr() }
    m.CacheTTL = d.Val()
```

### 3. Init in Provision()

```go
func (m *MesiMiddleware) Provision(ctx caddy.Context) error {
    // ... existing provision logic ...

    if m.CacheTTL != "" {
        d, err := time.ParseDuration(m.CacheTTL)
        if err != nil {
            return fmt.Errorf("invalid cache_ttl: %w", err)
        }
        m.cacheTTL = d
    }

    switch m.CacheBackend {
    case "":
        // no cache
    case "memory":
        size := m.CacheSize
        if size <= 0 { size = 10000 }
        m.cache = mesi.NewMemoryCache(size)
    default:
        return fmt.Errorf("unknown cache backend: %s", m.CacheBackend)
    }
    return nil
}
```

### 4. Map in ServeHTTP

```go
if m.cache != nil {
    config.Cache = m.cache
    config.CacheTTL = m.cacheTTL
}
```

### Caddyfile

```
mesi {
    cache_backend memory
    cache_size 5000
    cache_ttl 60s
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `cache_backend memory` → cache created in Provision
- [ ] **Tests** — Unit test: `cache_size` and `cache_ttl` values
- [ ] **Tests** — Unit test: absent → no cache
- [ ] **Docs** — Add directives to README
- [ ] **Functional tests** — Caddy integration test:
  - Cache enabled → first request origin, second cache hit
  - TTL expired → cache miss
  - No cache directive → every request origin (backward compat)
- [ ] **Changelog** — Entry

## Notes

- `Provision()` is called once at config load. Cache persists across requests.
- Memory cache is per-Caddy-process. Multi-instance deployments need Redis/Memcached for shared caching.
- `cache_size` default 10000 ≈ 100 MB at 10 KB avg entries.
