# [caddy] Add Memcached cache backend (`cache_backend memcached`)

## Problem

`mesi.NewMemcachedCache(servers ...string)` enables Memcached-backed caching. No Caddyfile directive.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    CacheMemcachedServers []string `json:"cache_memcached_servers,omitempty"`
}
```

### 2. Parse

```go
case "cache_memcached_servers":
    m.CacheMemcachedServers = d.RemainingArgs()
```

### 3. Init in Provision()

```go
case "memcached":
    if len(m.CacheMemcachedServers) == 0 {
        return fmt.Errorf("cache_memcached_servers is required for memcached backend")
    }
    m.cache = mesi.NewMemcachedCache(m.CacheMemcachedServers...)
```

### Caddyfile

```
mesi {
    cache_backend memcached
    cache_ttl 120s
    cache_memcached_servers 10.0.0.1:11211 10.0.0.2:11211
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: servers list parsing
- [ ] **Tests** — Unit test: backend=memcached without servers → Provision error
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Caddy integration test with Memcached container:
  - Cache hit/miss, TTL expiry
  - Multiple servers → consistent hashing
- [ ] **Changelog** — Entry

## Notes

- Memcached 1 MB value limit.
- `RemainingArgs()` collects space-separated `host:port` tokens.
