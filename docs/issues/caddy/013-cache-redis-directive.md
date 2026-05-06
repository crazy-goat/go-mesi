# [caddy] Add Redis cache backend (`cache_backend redis`)

## Problem

`mesi.NewRedisCache(addr, password, db)` enables Redis-backed caching shared across Caddy instances. No Caddyfile directive.

## Proposed solution

### 1. Add fields

```go
type MesiMiddleware struct {
    // ...
    CacheRedisAddr     string `json:"cache_redis_addr,omitempty"`
    CacheRedisPassword string `json:"cache_redis_password,omitempty"`
    CacheRedisDB       int    `json:"cache_redis_db,omitempty"`
}
```

### 2. Parse

```go
case "cache_redis_addr":
    if !d.NextArg() { return d.ArgErr() }
    m.CacheRedisAddr = d.Val()
case "cache_redis_password":
    if !d.NextArg() { return d.ArgErr() }
    m.CacheRedisPassword = d.Val()
case "cache_redis_db":
    if !d.NextArg() { return d.ArgErr() }
    m.CacheRedisDB, _ = strconv.Atoi(d.Val())
```

### 3. Init in Provision()

```go
case "redis":
    addr := m.CacheRedisAddr
    if addr == "" { addr = "localhost:6379" }
    m.cache = mesi.NewRedisCache(addr, m.CacheRedisPassword, m.CacheRedisDB)
```

### Caddyfile

```
mesi {
    cache_backend redis
    cache_ttl 120s
    cache_redis_addr 10.0.0.5:6379
    cache_redis_db 2
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: Redis backend init succeeds with valid addr
- [ ] **Docs** — Add directives to README
- [ ] **Functional tests** — Caddy integration test with Redis container:
  - `cache_backend redis` → first origin, second cache
  - `KEYS mesi:*` shows entries
  - TTL expiry test
  - Redis unreachable → degraded (origin hit), no crash
- [ ] **Changelog** — Entry

## Notes

- `go-redis` pools connections internally. No extra pool config needed.
- Password in Caddyfile — ensure proper file permissions.
- Key prefix: `mesi:<url>`.
