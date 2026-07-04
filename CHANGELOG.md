# Changelog

## [0.9.0] - Unreleased

### Added
- Traefik memory cache backend: `cacheBackend: memory`, `cacheSize`, and `cacheTTL` config options wire the in-memory LRU cache into Traefik ESI processing. Duplicate `<esi:include>` URLs within TTL are served from cache, reducing backend load (#234)
- libgomesi `InitCache` and `FreeCache` exports enable shared cache across C-based consumers (nginx, Apache, PHP extension). Supported backend: `"memory"` with configurable size and TTL (#232)
- nginx cache backend directives: `mesi_cache_backend memory`, `mesi_cache_size`, and `mesi_cache_ttl` wire the in-memory LRU cache into nginx ESI processing. Duplicate `<esi:include>` URLs within TTL are served from cache, reducing backend load (#232)
- Apache memory cache backend directives: `MesiCacheBackend memory`, `MesiCacheSize`, and `MesiCacheTTL` wire the in-memory LRU cache into Apache mESI output filtering. The directives validate strictly (`MesiCacheBackend` must be `memory` or empty — any other value is rejected at startup to prevent silent cache disablement due to typos; `MesiCacheSize` in `[1, 1000000]`; `MesiCacheTTL` in `[0, 86400]` seconds). A duplicate-include fixture and backend hit-count assertion in `test.sh` prove the cache dedups within a single response (#174)
- CLI memory cache backend: `-cache-backend=memory`, `-cache-size`, and `-cache-ttl` flags wire the `mesi.MemoryCache` into CLI invocations so duplicate `<esi:include>` URLs are served from cache within a single run (#207)
- CLI Redis cache backend: `-cache-backend=redis`, `-cache-redis-addr`, `-cache-redis-password`, and `-cache-redis-db` flags wire the `cache_redis.RedisCache` into CLI invocations, enabling Redis-backed ESI caching from the command line (#212)
- CLI Memcached cache backend: `-cache-backend=memcached` and `-cache-memcached-servers` flags wire the `cache_memcached.MemcachedCache` into CLI invocations, enabling Memcached-backed ESI caching from the command line (#217)
- RoadRunner Memcached cache backend: `cache_backend: memcached` and `cache_memcached_servers` config option wire the `cache_memcached.MemcachedCache` into the RoadRunner middleware, enabling Memcached-backed ESI caching (#245)
- libgomesi `InitCacheWithConfig` C entrypoint: takes a JSON config blob alongside the existing `backend`/`size`/`ttl` params, enabling `redis` (addr/password/db) and `memcached` (server list) configuration for C consumers like Apache. Calling on the `redis`/`memcached` backends without proper config returns `-1` so a typo never silently falls back to no cache (#175)
- Apache Redis cache backend directives: `MesiCacheBackend redis`, `MesiCacheRedisAddr` (host:port), `MesiCacheRedisPassword`, and `MesiCacheRedisDB` (0..15) wire the `cache_redis.RedisCache` into Apache mESI output filtering, enabling Redis-backed ESI caching shared across Apache workers and instances. Strict validation: `MesiCacheBackend` accepts only `memory`, `redis`, `memcached`, or empty; `MesiCacheRedisAddr` requires a colon and a 1..65535 port (rejects whitespace, control chars, and JSON-meta chars so the rendered JSON is safe); `MesiCacheRedisPassword` rejects embedded control chars with a generic error that does not leak the value into error logs; `MesiCacheRedisDB` requires a non-negative integer in `[0, 15]`. Malformed config returns `-1` from libgomesi and is logged; ESI continues without cache (#175)

### Changed
- **`<esi:include fetch-mode="ab" ab-ratio="…">` now rejects malformed input.** Previously the parser silently substituted the documented `{A:50, B:50}` default for every malformed value (missing colon, extra colons, negative numbers, decimals, non-integer operands, oversized integers, both-sides-zero). It now returns `*ErrInvalidABRatio` through the existing include-error path, surfacing the operator's actual input in logs and the `IncludeErrorMarker` (#315).

### Fixed
- `selectUrl` integer-sum overflow at high ratios guarded against `math.MaxInt` saturation (#315).

## [0.8.0] - Unreleased

### Added
- Traefik integration tests: docker-compose, test.sh, and CI job (#79, #302)
- RoadRunner integration tests: Dockerfile, docker-compose, Makefile, test.sh, and CI job (#80, #306)
- Nginx integration tests: Dockerfile, docker-compose, test.sh, and CI job (#81, #300)
- Caddy integration tests: Dockerfile, docker-compose, Caddyfile.test, test.sh, and CI job (#82, #301)
- FrankenPHP integration tests: Dockerfile, docker-compose, Caddyfile.ci, test.sh, PHP fixtures, and CI job (#83, #304)
- Standalone proxy tests: Go unit tests, E2E test.sh, and CI job (#84, #271)
- PHP extension test suite: `.phpt` tests, Dockerfile, docker-compose, test.sh, and CI job (#85, #305)
- CLI tests: Go unit tests, E2E test.sh, and separate unit/E2E CI jobs (#86, #272)
