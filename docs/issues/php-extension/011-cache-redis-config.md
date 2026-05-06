# [php-extension] Add Redis cache backend (`cache_backend redis`)

## Problem

`mesi.NewRedisCache` not available from PHP.

## Proposed solution

### PHP API

```php
$config = [
    'cache_backend' => 'redis',
    'cache_ttl' => 120,
    'cache_redis_addr' => '10.0.0.5:6379',
    'cache_redis_db' => 2,
];
```

### libgomesi InitCache with Redis config

```c
// PHP_MINIT or first parse_with_config call
InitCache("redis", redis_addr, redis_password, redis_db);
```

## Acceptance criteria

- [ ] **Tests** — PHPT with Redis container: cache hit/miss
- [ ] **Tests** — PHPT: `KEYS mesi:*` shows entries
- [ ] **Tests** — PHPT: TTL expiry, connection failure → degraded
- [ ] **Docs** — Add to README
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked on libgomesi `InitCache`
