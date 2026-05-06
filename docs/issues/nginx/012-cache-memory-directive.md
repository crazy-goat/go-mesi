# [nginx] Add `cache_backend memory` directive

## Problem

`mesi.EsiParserConfig.Cache` accepts a `mesi.Cache` implementation. When set, `<esi:include>` responses are cached and served from cache on subsequent identical requests (same URL, within TTL). When `nil`, every include hits the origin.

`mesi.NewMemoryCache(maxSize int)` provides a thread-safe in-memory LRU cache. The nginx module cannot enable it — no directive, no cache construction.

## Impact

- Every ESI include is fetched from origin on every request — even for cacheable fragments (headers, footers, shared navigation).
- Backend load scales linearly with traffic on pages with ESI includes.
- No cache invalidation mechanism (TTL, max entries) for repeated includes.

## Context

```go
// mesi/cache_memory.go
func NewMemoryCache(maxSize int) Cache
```

The cache stores `(key → value)` pairs with LRU eviction at `maxSize` entries. Key is `DefaultCacheKey(url)` which returns `"mesi:" + url`.

**Important caveat for nginx/CGo**: The memory cache is created per-`MESIParse` call in the current architecture because `Parse`/`ParseJson` construct a new `EsiParserConfig` each time. The cache would be created fresh and discarded after each request, providing **no benefit**. 

For caching to work in nginx's context, the cache must be **persistent** across `Parse` calls from the same nginx worker process. This requires one of:

1. **libgomesi-level cache**: Initialize a shared cache at libgomesi load time (in `init()` or via explicit `InitCache` export), reuse it across all `Parse` calls.
2. **Per-worker persistent config**: Pass a pre-constructed `Cache` object pointer via CGo rather than creating new config each time.

Both approaches require architectural changes to libgomesi, not just nginx config.

## Proposed solution

### Phase 1: nginx directive (config entry point)

Even though the caching implementation requires libgomesi changes, the nginx directive provides the operator-facing interface:

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    ngx_str_t   cache_backend;  // "" (off), "memory"
    ngx_int_t   cache_size;     // max entries for memory cache
    ngx_int_t   cache_ttl;      // TTL in seconds
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directives

```c
{ngx_string("mesi_cache_backend"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, cache_backend), NULL},

{ngx_string("mesi_cache_size"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, cache_size), NULL},

{ngx_string("mesi_cache_ttl"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, cache_ttl), NULL},
```

### 3. Defaults

```c
ngx_conf_merge_value(conf->cache_size, prev->cache_size, 10000);
ngx_conf_merge_value(conf->cache_ttl, prev->cache_ttl, 30);
```

### Phase 2: libgomesi persistent cache (dependency)

Add to libgomesi:

```go
var sharedCache mesi.Cache

//export InitCache
func InitCache(backend *C.char, size C.int, ttlSeconds C.int) C.int {
    // Initialize the shared cache at process start
    switch C.GoString(backend) {
    case "memory":
        sharedCache = mesi.NewMemoryCache(int(size))
    default:
        return -1
    }
    return 0
}

//export ParseJson
func ParseJson(input *C.char, jsonConfig *C.char) *C.char {
    var cfg struct {
        // ... other fields ...
        UseCache bool `json:"useCache"`
        CacheTTL int  `json:"cacheTTL"`
    }
    json.Unmarshal([]byte(C.GoString(jsonConfig)), &cfg)

    config := buildEsiParserConfig(cfg)
    if cfg.UseCache && sharedCache != nil {
        config.Cache = sharedCache
        config.CacheTTL = time.Duration(cfg.CacheTTL) * time.Second
    }
    // ...
}
```

### nginx module init changes

```c
static ngx_int_t ngx_http_mesi_thread_init(ngx_cycle_t *cycle) {
    // ... dlopen, dlsym existing functions ...

    // Load InitCache if available
    InitCacheFunc InitCache = (InitCacheFunc)dlsym(go_module, "InitCache");
    if (InitCache) {
        // Cache backend is per-process, not per-location.
        // Which location's config to use? First one? Main config?
        // This is an architectural question — see Notes.
        InitCache("memory", 10000, 30);
    }
    return NGX_OK;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: `mesi_cache_backend memory`, `mesi_cache_backend off` (or unset)
- [ ] **Tests** — Unit test `cache_size` and `cache_ttl` values
- [ ] **Docs** — Add directives to README with cache behavior, limitations, and the per-worker scope
- [ ] **Docs** — Document that memory cache is per-nginx-worker-process (NOT shared across workers)
- [ ] **Functional tests** — nginx integration test:
  - `cache_backend memory; cache_ttl 30` → first request hits origin, second identical request within 30s serves from cache
  - Verify cache hit: backend access log shows only ONE request for the include
  - `cache_ttl 1` → cache hit within 1s, cache miss after 1s (origin hit again)
  - `cache_backend` unset → no caching, every request hits origin
- [ ] **Changelog** — Entry in project changelog

## Notes

- **Architectural constraint**: `EsiParserConfig` is ephemeral (created per-`Parse` call). Caching requires shared state across calls. This is a libgomesi-level change, not just nginx config. Mark this issue as blocked on libgomesi `InitCache`.
- **Per-worker scope**: nginx has multiple worker processes. Each worker loads libgomesi independently (via its own `dlopen`). The in-memory cache is per-worker, not global. Two different nginx workers will NOT share cached entries. Document this limitation.
- **Cache key**: Uses `DefaultCacheKey` (URL-based). Two different pages including the same URL fragment share the cache entry. This is correct behavior for shared fragments.
- **TTL precision**: CGo passes `int` seconds. Convert to `time.Duration` (nanoseconds) in Go.
- **Eviction**: LRU at `cache_size` entries. When full, least-recently-used entry is evicted. Operators should estimate cacheable include count and set `cache_size` accordingly.
- **Memory usage**: Estimated as `cache_size × average_include_body_size`. A 10,000-entry cache with 10 KB average entries uses ~100 MB. Document this for capacity planning.
