# [apache] Add memory cache backend (`MesiCacheBackend memory`)

## Problem

`EsiParserConfig.Cache` when `nil` means every `<esi:include>` hits origin. `mesi.NewMemoryCache(maxSize)` provides an in-memory LRU cache but Apache cannot use it — no directive, no cache construction.

**Architectural constraint** (same as nginx #012): `EsiParserConfig` is created per-`ParseJson` call. For caching to work, the cache must be persistent across calls. Requires libgomesi `InitCache` function.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    const char *cache_backend;  // NULL = off, "memory", "redis", "memcached"
    int cache_size;             // max entries, -1 = unset
    int cache_ttl;              // seconds, -1 = unset
} mesi_config;
```

### 2. Add directives

```c
AP_INIT_TAKE1("MesiCacheBackend", set_cache_backend, NULL, RSRC_CONF,
    "Cache backend for ESI includes: memory, redis, memcached (default: off)"),
AP_INIT_TAKE1("MesiCacheSize", set_cache_size, NULL, RSRC_CONF,
    "Max cache entries for memory backend (default: 10000)"),
AP_INIT_TAKE1("MesiCacheTTL", set_cache_ttl, NULL, RSRC_CONF,
    "Cache TTL in seconds (default: 30)"),
```

### 3. Defaults and merge

```c
conf->cache_backend = NULL;
conf->cache_size = -1;  // → 10000
conf->cache_ttl = -1;   // → 30
```

### 4. libgomesi InitCache (dependency)

```go
//export InitCache
func InitCache(backend *C.char, size C.int, ttlSeconds C.int) C.int {
    switch C.GoString(backend) {
    case "memory":
        sharedCache = mesi.NewMemoryCache(int(size))
    default:
        return -1
    }
    sharedCacheTTL = time.Duration(ttlSeconds) * time.Second
    return 0
}
```

należy wołać w `mesi_child_init`:

```c
InitCacheFunc InitCache = dlsym(go_module, "InitCache");
if (InitCache && conf->cache_backend) {
    int size = (conf->cache_size != -1) ? conf->cache_size : 10000;
    int ttl = (conf->cache_ttl != -1) ? conf->cache_ttl : 30;
    InitCache(conf->cache_backend, size, ttl);
}
```

### Apache config

```apache
MesiCacheBackend memory
MesiCacheSize 5000
MesiCacheTTL 60
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `MesiCacheBackend memory`, unset → off
- [ ] **Tests** — Unit test: `MesiCacheSize 100`, `MesiCacheTTL 5`
- [ ] **Docs** — Add directives to README with per-worker caveat
- [ ] **Functional tests** — Apache integration test:
  - `MesiCacheBackend memory; MesiCacheTTL 30` → first request hits origin, second hits cache
  - `MesiCacheTTL 1` → cache miss after 1s
  - No cache config → every request hits origin
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked on libgomesi `InitCache`

## Notes

- Memory cache is per-Apache-worker (NOT shared across workers). Each child process has its own cache via its own libgomesi instance.
- Cache key: `mesi:<url>` via `DefaultCacheKey`. Shared fragments with same URL share cache entries.
- Default `cache_size 10000` with average 10 KB entries ≈ 100 MB RAM per worker. Adjust for your worker count.
