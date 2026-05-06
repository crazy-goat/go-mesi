# [traefik] Add Redis cache backend (`cacheBackend redis`)

## Problem

`mesi.NewRedisCache` not available via Traefik plugin config.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    CacheRedisAddr     string `json:"cacheRedisAddr" yaml:"cacheRedisAddr"`
    CacheRedisPassword string `json:"cacheRedisPassword" yaml:"cacheRedisPassword"`
    CacheRedisDB       int    `json:"cacheRedisDb" yaml:"cacheRedisDb"`
}
```

### 2. Init in New()

```go
case "redis":
    addr := config.CacheRedisAddr
    if addr == "" { addr = "localhost:6379" }
    p.cache = mesi.NewRedisCache(addr, config.CacheRedisPassword, config.CacheRedisDB)
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          cacheBackend: redis
          cacheTTL: "120s"
          cacheRedisAddr: "10.0.0.5:6379"
          cacheRedisDb: 2
```

## Acceptance criteria

- [ ] **Tests** — Unit test init
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Redis container: cache hit/miss, `KEYS mesi:*`, TTL, connection failure → degraded
- [ ] **Changelog** — Entry
