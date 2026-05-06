# [traefik] Add `cacheKeyTemplate` plugin config option

## Problem

`CacheKeyFunc` is Go function pointer — cannot be specified in YAML config. Default `DefaultCacheKey` is URL-only.

## Proposed solution

Template-based, same as other servers:

```go
type Config struct {
    // ...
    CacheKeyTemplate string `json:"cacheKeyTemplate" yaml:"cacheKeyTemplate"`
}
```

In `ServeHTTP()`:

```go
config.CacheKeyFunc = func(url string) string {
    return buildCacheKey(url, p.config.CacheKeyTemplate, req)
}
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          cacheBackend: redis
          cacheTTL: "120s"
          cacheKeyTemplate: "mesi:${url}:${header:Accept-Language}"
```

## Acceptance criteria

- [ ] **Tests** — Template substitution: `${url}`, `${header:X}`, `${cookie:Y}`
- [ ] **Docs** — Document template syntax and supported placeholders
- [ ] **Functional tests** — Different `Accept-Language` → different cache entries
- [ ] **Changelog** — Entry
