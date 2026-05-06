# [nginx] Add Memcached cache backend directive

## Problem

`mesi.NewMemcachedCache(servers ...string)` (`mesi/cache_memcached.go`) provides a Memcached-backed cache. Features:

- Distributed cache with consistent hashing across servers
- Simple key-value store (no Redis features needed)
- Memcached's built-in TTL-based expiration

The nginx module has no directive for Memcached caching.

## Impact

- Operators already using Memcached for application caching (PHP sessions, database query cache) cannot use the same infrastructure for ESI fragment caching.
- Memcached is lighter-weight than Redis for simple key-value workloads — operators who don't need Redis features pay unnecessary complexity.

## Context

```go
func NewMemcachedCache(servers ...string) Cache
```

Uses `gomemcache` client (`github.com/bradfitz/gomemcache`). The `Cache` interface implementation stores values with the configured Memcached timeout.

**Same architectural constraint**: requires libgomesi `InitCache`.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    ngx_str_t   cache_memcached_servers;  // space or comma-separated "host1:11211 host2:11211"
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

Space-separated server list (consistent with `mesi_allowed_hosts` convention):

```c
{ngx_string("mesi_cache_memcached_servers"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, cache_memcached_servers), NULL},
```

### 3. Pass to libgomesi

In `InitCache` config JSON:

```json
{"memcachedServers": ["10.0.0.1:11211", "10.0.0.2:11211"]}
```

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_cache_backend memcached;
    mesi_cache_ttl 120;
    mesi_cache_memcached_servers 10.0.0.1:11211 10.0.0.2:11211;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: single server, multiple servers (space-separated)
- [ ] **Tests** — Unit test that servers string → `[]string` conversion for JSON
- [ ] **Docs** — Add directive to README with Memcached setup and server list format
- [ ] **Functional tests** — nginx integration test:
  - Memcached container running, `cache_backend memcached` → first request hits origin, second hits cache
  - Verify cached entries exist in Memcached (via `stats items`, `stats cachedump`)
  - `cache_ttl 1` → entry expires after 1s
  - Multiple Memcached servers → cache survives one server failure (consistent hashing)
- [ ] **Changelog** — Entry in project changelog

## Notes

- Memcached has a 1 MB value size limit. ESI includes larger than 1 MB cannot be cached. Document this limitation.
- `gomemcache` uses standard text protocol. TLS is not natively supported by most Memcached deployments. Use a sidecar proxy (e.g., stunnel) for encrypted connections if needed.
- Server list format: `host:port` separated by spaces. Default port is 11211. If port omitted, nginx should default to 11211 or require explicit port. Recommend requiring explicit port for clarity.
