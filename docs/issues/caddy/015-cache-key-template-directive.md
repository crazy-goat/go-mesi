# [caddy] Add `cache_key_template` Caddyfile directive

## Problem

`CacheKeyFunc` is a Go function pointer — cannot be specified from Caddyfile. Default `DefaultCacheKey(url)` is URL-only, which caches incorrectly for header/cookie-dependent fragments (Accept-Language, A/B test segments).

## Proposed solution

Template-based cache key, same approach as nginx #015 and apache #013:

```go
type MesiMiddleware struct {
    // ...
    CacheKeyTemplate string `json:"cache_key_template,omitempty"`
}
```

### Parse

```go
case "cache_key_template":
    if !d.NextArg() { return d.ArgErr() }
    m.CacheKeyTemplate = d.Val()
```

### Template evaluation in libgomesi or middleware

```go
func buildCacheKey(url string, template string, r *http.Request) string {
    result := template
    result = strings.ReplaceAll(result, "${url}", url)
    for key, vals := range r.Header {
        result = strings.ReplaceAll(result, fmt.Sprintf("${header:%s}", key), vals[0])
    }
    // ${cookie:Name} — parse r.Cookie("Name")
    return result
}
```

Pass the built key as `CacheKeyFunc`:

```go
config.CacheKeyFunc = func(url string) string {
    return buildCacheKey(url, m.CacheKeyTemplate, r)
}
```

### Caddyfile

```
mesi {
    cache_backend redis
    cache_ttl 120s
    cache_key_template "mesi:${url}:${header:Accept-Language}"
}
```

## Acceptance criteria

- [ ] **Design** — Confirm template approach acceptable vs infeasible function pointer from Caddyfile
- [ ] **Tests** — Unit test: `${url}`, `${header:X}`, `${cookie:Y}` substitution
- [ ] **Tests** — Unit test: unknown placeholder → literal
- [ ] **Docs** — Document template syntax, placeholders
- [ ] **Functional tests** — Caddy integration test:
  - Template with `${header:Accept-Language}` → `pl` and `en` get different cache entries
  - No template → `DefaultCacheKey` (URL-only)
- [ ] **Changelog** — Entry

## Notes

- Unlike C modules, Caddy can pass a closure as `CacheKeyFunc` because it's Go-native. The template approach wraps a string template in a Go closure — much cleaner than C template evaluation.
- Placeholders: `${url}`, `${header:X}` (case-insensitive), `${cookie:Y}`.
- Template strings with spaces need Caddyfile quoting.
