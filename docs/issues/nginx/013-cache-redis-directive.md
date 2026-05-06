# [nginx] Add Redis cache backend directive

## Problem

`mesi.NewRedisCache(addr, password, db)` provides a Redis-backed cache (`mesi/cache_redis.go`). When used as `EsiParserConfig.Cache`, ESI include responses are cached in Redis, enabling:

- Cache sharing across nginx worker processes (unlike memory cache which is per-worker)
- Cache sharing across multiple nginx instances
- Persistence across process restarts
- TTL-based automatic expiration

The nginx module has no directive to enable Redis caching.

## Impact

- Multi-worker nginx deployments cannot share cached ESI fragments.
- Cache is lost on nginx process restart/reload.
- No external cache visibility — operators cannot inspect cached entries.

## Context

```go
func NewRedisCache(addr string, password string, db int) Cache
```

Requires `go-redis` dependency (already in root `go.mod`). Redis connection is managed internally by the library.

**Same architectural constraint as memory cache (#012)**: requires libgomesi-level shared state initialization. The nginx directive defines the operator-facing config; a libgomesi `InitCache` function constructs and stores the Redis client for reuse across `Parse` calls.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    ngx_str_t   cache_backend;       // "" (off), "memory", "redis", "memcached"
    ngx_int_t   cache_ttl;           // TTL in seconds
    ngx_str_t   cache_redis_addr;    // e.g., "localhost:6379"
    ngx_str_t   cache_redis_password;
    ngx_int_t   cache_redis_db;
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directives

```c
{ngx_string("mesi_cache_redis_addr"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, cache_redis_addr), NULL},

{ngx_string("mesi_cache_redis_password"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, cache_redis_password), NULL},

{ngx_string("mesi_cache_redis_db"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, cache_redis_db), NULL},
```

### 3. Defaults

```c
ngx_conf_merge_value(conf->cache_redis_db, prev->cache_redis_db, 0);
// addr defaults to "localhost:6379" if not set but backend is "redis"
// password defaults to "" (no auth)
```

### 4. libgomesi InitCache extension

```go
//export InitCache
func InitCache(backend *C.char, configJson *C.char) C.int {
    // configJson: {"redisAddr":"localhost:6379","redisPassword":"","redisDB":0}
    // ... initialize Redis client ...
    sharedCache = mesi.NewRedisCache(addr, password, db)
    return 0
}
```

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_cache_backend redis;
    mesi_cache_ttl 60;
    mesi_cache_redis_addr 10.0.0.5:6379;
    mesi_cache_redis_db 2;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: `cache_redis_addr`, `cache_redis_password`, `cache_redis_db`
- [ ] **Tests** — Unit test defaults: no password (empty), db 0
- [ ] **Docs** — Add directives to README with Redis configuration examples
- [ ] **Docs** — Document Redis key format: `mesi:<url>` (from `DefaultCacheKey`)
- [ ] **Functional tests** — nginx integration test:
  - Redis container running, `cache_backend redis` → first request hits origin, second hits cache
  - Verify cached entries appear in Redis: `KEYS mesi:*`
  - `cache_ttl 1` → entry expires after 1s, next request hits origin
  - Redis connection failure → nginx still serves (degraded, cache miss, origin hit)
  - Redis reconnection → cache resumes working after Redis comes back
- [ ] **Changelog** — Entry in project changelog

## Notes

- **Same architectural constraint as #012**: Requires `InitCache` in libgomesi for persistent cache across calls.
- Redis key prefix: `mesi:<url>`. If using Redis for other purposes, ensure no key collisions.
- Connection pooling: `go-redis` handles connection pooling internally. Default pool size is 10 connections per CPU. This is shared across all `Parse` calls in the worker process.
- Password in nginx config: nginx config files are typically readable by the nginx user only. Store Redis password there or use `include` with restricted permissions.
- Redis Sentinel/Cluster: not supported by `NewRedisCache`. For highly available Redis, use a TCP load balancer (HAProxy, Envoy) in front of Redis. Cluster support is a separate enhancement.
- Redis SSL/TLS: verify `go-redis` supports TLS connections (it does via `redis.NewClient(&redis.Options{TLSConfig: ...})`). `NewRedisCache` currently does not expose TLS config. This is a library enhancement.
