# [php-extension] Add Memcached cache backend (`cache_backend memcached`)

## Problem

`mesi.NewMemcachedCache` not available from PHP.

## Proposed solution

### PHP API

```php
$config = [
    'cache_backend' => 'memcached',
    'cache_ttl' => 120,
    'cache_memcached_servers' => ['10.0.0.1:11211', '10.0.0.2:11211'],
];
```

## Acceptance criteria

- [ ] **Tests** — PHPT with Memcached container: cache hit/miss, TTL
- [ ] **Docs** — Add to README
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked on libgomesi `InitCache`
