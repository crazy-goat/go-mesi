# [nginx] Add custom cache key function support

## Problem

`mesi.EsiParserConfig.CacheKeyFunc` (`mesi/cache.go:14`) allows custom cache key derivation from include URL:

```go
type CacheKeyFunc func(url string) string
func DefaultCacheKey(url string) string { return "mesi:" + url }
```

The default key is URL-based. This is correct for URL-stable includes (fragments that depend only on the include URL). However, ESI includes may depend on:

- **Request headers**: `Accept-Language` determines localized content. Caching by URL alone serves wrong language to subsequent requests.
- **Cookies**: A/B test variants, user segments.
- **Query parameters**: The include URL may be static but the actual rendered content depends on the parent request's cookies.

`CacheKeyFunc` is a Go function pointer — it cannot be passed from C via CGo in a portable, type-safe way.

## Impact

- **Incorrect caching for personalized content**: If localized fragments share the same URL, the cache returns the first-rendered language variant for all users.
- **No customization**: Operators building multilingual or personalized sites cannot safely use ESI caching.

## Constraints

`CacheKeyFunc` is a Go `func(string) string`. From CGo, passing a function pointer is possible but:

1. C function pointer ≠ Go function — requires `gobridge` or `syscall.NewCallback` (Windows-only).
2. Even if passed, the Go function would call back to C, creating a C→Go→C→Go call chain.
3. Performance: callback per cache lookup adds overhead.

**Verdict**: Custom `CacheKeyFunc` passing from C is impractical. This feature is Go-native-only.

## Proposed solution

### Alternative: Header-inclusive cache key via config

Instead of a general `CacheKeyFunc`, expose a **configuration-driven** cache key composition:

```json
{
    "cacheKeyTemplate": "mesi:${url}:${header:Accept-Language}:${cookie:segment}"
}
```

This is a string template, not a function. It can be passed via JSON config from C. The Go side evaluates the template at cache lookup time using the current request's context.

### Implementation in libgomesi

```go
// CacheKeyBuilder evaluates a template to produce a cache key
func buildCacheKey(url string, template string, headers http.Header) string {
    result := template
    result = strings.ReplaceAll(result, "${url}", url)
    for key, values := range headers {
        placeholder := fmt.Sprintf("${header:%s}", key)
        result = strings.ReplaceAll(result, placeholder, values[0])
    }
    // ... cookie extraction requires cookie parsing ...
    return result
}
```

### nginx directive

```nginx
mesi_cache_key_template "mesi:${url}:${header:Accept-Language}";
```

### Limitations

- Template-based composition is less flexible than a function pointer.
- Header and cookie extraction is limited to simple substitution.
- No logic (e.g., "if header X then hash Y").
- This is a "good enough" solution for 90% of use cases.

## Acceptance criteria

- [ ] **Design** — Confirm template-based approach is acceptable vs. function pointer (which is infeasible from C)
- [ ] **Tests** — Unit test template parsing: `${url}`, `${header:Accept-Language}`, `${cookie:segment}`
- [ ] **Tests** — Unit test template with unknown placeholder → literal (or error)
- [ ] **Docs** — Document template syntax, supported placeholders, examples
- [ ] **Docs** — Clearly state that `CacheKeyFunc` function pointers are NOT supported from nginx
- [ ] **Functional tests** — nginx integration test:
  - `cache_key_template "mesi:${url}:${header:Accept-Language}"` → request with `Accept-Language: pl` and `Accept-Language: en` → different cache entries, correct language in output
  - No template set → `DefaultCacheKey` used (URL-only)
- [ ] **Changelog** — Entry in project changelog noting this is a template-based compromise

## Notes

- This is a **compromise** feature. Full `CacheKeyFunc` flexibility is only available in Go-native integrations (Traefik, Caddy, RoadRunner). nginx users get template-based key composition.
- Template syntax should be simple: `${url}`, `${header:Header-Name}` (case-insensitive header lookup), `${cookie:Cookie-Name}`.
- Header values may contain characters invalid for cache keys (spaces, special chars). Consider URL-encoding or hashing header values in the template output.
- If operators need full `CacheKeyFunc` flexibility, they should use a Go-based proxy (Traefik/Caddy) or the standalone proxy server.
- Performance: template evaluation happens once per cache lookup (before `Cache.Get`). String replacement is O(n) per placeholder — negligible overhead for typical templates.
