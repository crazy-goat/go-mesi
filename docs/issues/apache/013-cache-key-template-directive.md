# [apache] Add `MesiCacheKeyTemplate` directive

## Problem

`mesi.EsiParserConfig.CacheKeyFunc` is a Go function pointer — cannot be passed from C via CGo in a portable way. The default `DefaultCacheKey(url)` returns `"mesi:" + url`, which is URL-only. This caches incorrectly for includes that depend on request headers (Accept-Language, cookies for A/B testing).

## Proposed solution

Same template-based approach as nginx #015:

```apache
MesiCacheKeyTemplate "mesi:${url}:${header:Accept-Language}"
```

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    const char *cache_key_template;  // NULL = use DefaultCacheKey (URL-only)
} mesi_config;
```

### 2. Add directive

```c
AP_INIT_TAKE1("MesiCacheKeyTemplate", set_cache_key_template, NULL, RSRC_CONF,
    "Cache key template: ${url}, ${header:Name}, ${cookie:Name} (default: mesi:${url})"),
```

### 3. libgomesi template evaluation

```go
func buildCacheKey(url string, template string, headers http.Header, cookies []*http.Cookie) string {
    result := template
    result = strings.ReplaceAll(result, "${url}", url)
    for key, vals := range headers {
        result = strings.ReplaceAll(result, fmt.Sprintf("${header:%s}", key), vals[0])
    }
    // Cookie extraction from request cookies
    // ...
    return result
}
```

### Apache config

```apache
MesiCacheBackend redis
MesiCacheTTL 120
MesiCacheKeyTemplate "mesi:${url}:${header:Accept-Language}:${cookie:segment}"
```

## Acceptance criteria

- [ ] **Design** — Confirm template approach is acceptable vs infeasible function pointer
- [ ] **Tests** — Unit test template parsing: `${url}`, `${header:X}`, `${cookie:Y}`
- [ ] **Tests** — Unit test unknown placeholder → literal
- [ ] **Docs** — Document template syntax, supported placeholders
- [ ] **Docs** — Explicitly state `CacheKeyFunc` pointers NOT supported from C modules
- [ ] **Functional tests** — Apache integration test:
  - Template with `${header:Accept-Language}` → `pl` and `en` requests get different cache entries
  - No template → `DefaultCacheKey` used (URL-only, backward compat)
- [ ] **Changelog** — Entry noting template-based compromise

## Notes

- This is a compromise. Full `CacheKeyFunc` flexibility only in Go-native servers (Traefik, Caddy).
- Template placeholders: `${url}`, `${header:Header-Name}` (case-insensitive), `${cookie:Cookie-Name}`.
- Header values may need escaping for cache key safety — spaces, special chars could break key formats.
- Template eval happens once per cache lookup. O(n) string replace per placeholder — negligible overhead.
