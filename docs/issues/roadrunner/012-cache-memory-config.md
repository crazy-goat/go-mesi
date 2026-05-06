# [roadrunner] Add `cache_backend memory` config option

## Problem

`EsiParserConfig.Cache` is `nil` — no caching. `mesi.NewMemoryCache` provides in-memory LRU cache.

## Proposed solution

### 1. Add to Config

```go
type Config struct {
    // ...
    CacheBackend string `mapstructure:"cache_backend"`  // "" (off), "memory", "redis", "memcached"
    CacheSize    int    `mapstructure:"cache_size"`     // default 10000
    CacheTTL     string `mapstructure:"cache_ttl"`      // e.g. "30s"
}
```

### 2. Init cache in Plugin.Init()

```go
type Plugin struct {
    config   *Config
    cache    mesi.Cache
    cacheTTL time.Duration
}

func (p *Plugin) Init(cfg config.Configurer) error {
    // ... read config ...
    
    if p.config.CacheBackend == "memory" {
        size := p.config.CacheSize
        if size <= 0 { size = 10000 }
        p.cache = mesi.NewMemoryCache(size)
    }
    
    if p.config.CacheTTL != "" {
        d, err := time.ParseDuration(p.config.CacheTTL)
        if err != nil { return err }
        p.cacheTTL = d
    }
    return nil
}
```

### 3. Map in Middleware()

```go
if p.cache != nil {
    config.Cache = p.cache
    config.CacheTTL = p.cacheTTL
}
```

### .rr.yaml

```yaml
http:
  middleware:
    mesi:
      cache_backend: memory
      cache_size: 5000
      cache_ttl: "60s"
```

## Acceptance criteria

- [ ] **Tests** — Unit test cache init in `Init()`
- [ ] **Tests** — Unit test: absent → no cache
- [ ] **Docs** — Add to README, per-worker-pool scope
- [ ] **Functional tests** — Integration test: cache hit/miss, TTL expiry
- [ ] **Changelog** — Entry
