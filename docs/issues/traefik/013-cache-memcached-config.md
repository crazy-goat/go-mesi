# [traefik] Add Memcached cache backend (`cacheBackend memcached`)

## Problem

`mesi.NewMemcachedCache` not available via Traefik plugin config.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    CacheMemcachedServers []string `json:"cacheMemcachedServers" yaml:"cacheMemcachedServers"`
}
```

### 2. Init in New()

```go
case "memcached":
    if len(config.CacheMemcachedServers) == 0 {
        return nil, fmt.Errorf("cacheMemcachedServers required for memcached backend")
    }
    p.cache = mesi.NewMemcachedCache(config.CacheMemcachedServers...)
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          cacheBackend: memcached
          cacheTTL: "120s"
          cacheMemcachedServers:
            - "10.0.0.1:11211"
            - "10.0.0.2:11211"
```

## Acceptance criteria

- [ ] **Tests** — Unit test init, empty servers → error
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Memcached container: cache hit/miss, TTL, multi-server consistent hashing
- [ ] **Changelog** — Entry
