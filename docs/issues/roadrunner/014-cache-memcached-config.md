# [roadrunner] Add Memcached cache backend (`cache_backend memcached`)

## Problem

`mesi.NewMemcachedCache` not available via RoadRunner config.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    CacheMemcachedServers []string `mapstructure:"cache_memcached_servers"`
}
```

### 2. Init in Plugin.Init()

```go
case "memcached":
    if len(p.config.CacheMemcachedServers) == 0 {
        return fmt.Errorf("cache_memcached_servers required for memcached backend")
    }
    p.cache = mesi.NewMemcachedCache(p.config.CacheMemcachedServers...)
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      cache_backend: memcached
      cache_ttl: "120s"
      cache_memcached_servers:
        - "10.0.0.1:11211"
        - "10.0.0.2:11211"
```

## Acceptance criteria

- [ ] **Tests** — Unit test init, empty servers → error
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Memcached container: cache hit/miss, TTL, multi-server consistent hashing
- [ ] **Changelog** — Entry

## Notes

- YAML list syntax for servers array.
- Memcached 1 MB value limit.
