# [traefik] Add `cacheBackend memory` plugin config option

## Problem

`EsiParserConfig.Cache` is `nil` — no caching. `mesi.NewMemoryCache` provides in-memory LRU cache.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    CacheBackend string `json:"cacheBackend" yaml:"cacheBackend"`  // "" (off), "memory", "redis", "memcached"
    CacheSize    int    `json:"cacheSize" yaml:"cacheSize"`
    CacheTTL     string `json:"cacheTTL" yaml:"cacheTTL"`  // e.g. "30s"
}
```

### 2. Init cache in New()

```go
type ResponsePlugin struct {
    // ...
    cache    mesi.Cache
    cacheTTL time.Duration
}

func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
    p := &ResponsePlugin{...}
    
    if config.CacheTTL != "" {
        d, err := time.ParseDuration(config.CacheTTL)
        if err != nil { return nil, err }
        p.cacheTTL = d
    }
    
    switch config.CacheBackend {
    case "":
        // no cache
    case "memory":
        size := config.CacheSize
        if size <= 0 { size = 10000 }
        p.cache = mesi.NewMemoryCache(size)
    }
    return p, nil
}
```

### 3. Map in ServeHTTP()

```go
if p.cache != nil {
    config.Cache = p.cache
    config.CacheTTL = p.cacheTTL
}
```

### YAML

```yaml
http:
  middlewares:
    mesi:
      plugin:
        mesi:
          cacheBackend: memory
          cacheSize: 5000
          cacheTTL: "60s"
```

## Acceptance criteria

- [ ] **Tests** — Unit test cache init in `New()`
- [ ] **Tests** — absent → no cache
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Cache hit/miss, TTL expiry
- [ ] **Changelog** — Entry
