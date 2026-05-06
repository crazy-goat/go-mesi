# [cli] Add `-cacheBackend memory` flag

## Problem

`EsiParserConfig.Cache` is `nil` — no caching. Repeated invocations re-fetch all includes. `mesi.NewMemoryCache` provides in-memory LRU cache (per-invocation, not persistent).

## Proposed solution

### Flags

```go
var cacheBackend = flag.String("cacheBackend", "",
    "Cache backend: memory, redis, memcached (default: off)")
var cacheSize    = flag.Int("cacheSize", 10000,
    "Max cache entries for memory backend")
var cacheTTL     = flag.Duration("cacheTTL", 0,
    "Cache TTL (e.g. 30s, 5m)")
```

### Map in main()

```go
switch *cacheBackend {
case "":
    // no cache
case "memory":
    config.Cache = mesi.NewMemoryCache(*cacheSize)
    config.CacheTTL = *cacheTTL
default:
    log.Fatalf("unknown cache backend: %s", *cacheBackend)
}
```

### Usage

```bash
mesi-cli -cacheBackend memory -cacheSize 5000 -cacheTTL 60s input.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: backend selection, TTL parsing
- [ ] **Tests** — Unit test: absent → no cache
- [ ] **Docs** — Add to README, note per-invocation scope
- [ ] **Functional tests** — Page with multiple duplicate includes → first fetched, subsequent cached within same invocation
- [ ] **Changelog** — Entry
