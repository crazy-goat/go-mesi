# [apache] Add Redis cache backend (`MesiCacheBackend redis`)

## Problem

`mesi.NewRedisCache(addr, password, db)` enables Redis-backed caching. Unlike memory cache (per-worker), Redis cache is shared across all Apache workers and instances. Apache has no directive.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    const char *cache_redis_addr;
    const char *cache_redis_password;
    int cache_redis_db;  // -1 = unset
} mesi_config;
```

### 2. Add directives

```c
AP_INIT_TAKE1("MesiCacheRedisAddr", set_cache_redis_addr, NULL, RSRC_CONF,
    "Redis server address for ESI caching (default: localhost:6379)"),
AP_INIT_TAKE1("MesiCacheRedisPassword", set_cache_redis_password, NULL, RSRC_CONF,
    "Redis password (default: none)"),
AP_INIT_TAKE1("MesiCacheRedisDB", set_cache_redis_db, NULL, RSRC_CONF,
    "Redis database number (default: 0)"),
```

### 3. libgomesi InitCache extension

Pass Redis config via JSON:

```c
char json[512];
snprintf(json, sizeof(json),
    "{\"redisAddr\":\"%s\",\"redisPassword\":\"%s\",\"redisDB\":%d}",
    conf->cache_redis_addr ? conf->cache_redis_addr : "localhost:6379",
    conf->cache_redis_password ? conf->cache_redis_password : "",
    conf->cache_redis_db != -1 ? conf->cache_redis_db : 0);
InitCache("redis", config_json);
```

### Apache config

```apache
MesiCacheBackend redis
MesiCacheTTL 120
MesiCacheRedisAddr 10.0.0.5:6379
MesiCacheRedisDB 2
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing
- [ ] **Docs** — Add directives to README
- [ ] **Functional tests** — Integration test with Redis container:
  - Redis cache → first request origin, second cache
  - `KEYS mesi:*` shows cached entries
  - TTL expiry → cache miss after TTL
  - Redis connection failure → degraded service (origin hit)
- [ ] **Changelog** — Entry

## Notes

- Redis key prefix: `mesi:<url>`. No collisions with other Redis keys expected.
- `go-redis` handles connection pooling internally. Default 10 conns per CPU.
- Password in Apache config — ensure proper file permissions.
- Sentinel/Cluster not supported by `NewRedisCache`. Use TCP load balancer for HA.
