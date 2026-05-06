# [roadrunner] Add Redis cache backend (`cache_backend redis`)

## Problem

`mesi.NewRedisCache` not available via RoadRunner config.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    CacheRedisAddr     string `mapstructure:"cache_redis_addr"`
    CacheRedisPassword string `mapstructure:"cache_redis_password"`
    CacheRedisDB       int    `mapstructure:"cache_redis_db"`
}
```

### 2. Init in Plugin.Init()

```go
case "redis":
    addr := p.config.CacheRedisAddr
    if addr == "" { addr = "localhost:6379" }
    p.cache = mesi.NewRedisCache(addr, p.config.CacheRedisPassword, p.config.CacheRedisDB)
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      cache_backend: redis
      cache_ttl: "120s"
      cache_redis_addr: "10.0.0.5:6379"
      cache_redis_db: 2
```

## Acceptance criteria

- [ ] **Tests** — Unit test init with valid config
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Redis container: cache hit/miss, `KEYS mesi:*`, TTL, connection failure → degraded
- [ ] **Changelog** — Entry
