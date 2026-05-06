# [roadrunner] Add `cache_key_template` config option

## Problem

`CacheKeyFunc` is Go function pointer — cannot be specified in `.rr.yaml`. Default `DefaultCacheKey` is URL-only.

## Proposed solution

Template-based, same as Caddy:

```go
type Config struct {
    // ...
    CacheKeyTemplate string `mapstructure:"cache_key_template"`
}

// In Middleware():
config.CacheKeyFunc = func(url string) string {
    return buildCacheKey(url, p.config.CacheKeyTemplate, r)
}
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      cache_backend: redis
      cache_ttl: "120s"
      cache_key_template: "mesi:${url}:${header:Accept-Language}"
```

## Acceptance criteria

- [ ] **Tests** — Template substitution: `${url}`, `${header:X}`, `${cookie:Y}`
- [ ] **Docs** — Document template syntax
- [ ] **Functional tests** — Different `Accept-Language` → different cache entries
- [ ] **Changelog** — Entry noting template-based (not function pointer)
