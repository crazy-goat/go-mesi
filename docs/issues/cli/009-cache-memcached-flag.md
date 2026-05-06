# [cli] Add `-cacheBackend memcached` flag

## Problem

`mesi.NewMemcachedCache` not available via CLI flags.

## Proposed solution

### Flags

```go
var cacheMemcachedServers = flag.String("cacheMemcachedServers", "",
    "Comma-separated Memcached servers (host:port)")
```

### Map

```go
case "memcached":
    servers := strings.Split(*cacheMemcachedServers, ",")
    if *cacheMemcachedServers == "" {
        log.Fatal("cacheMemcachedServers required for memcached backend")
    }
    config.Cache = mesi.NewMemcachedCache(servers...)
    config.CacheTTL = *cacheTTL
```

### Usage

```bash
mesi-cli -cacheBackend memcached -cacheTTL 120s -cacheMemcachedServers "10.0.0.1:11211,10.0.0.2:11211" -url https://example.com/
```

## Acceptance criteria

- [ ] **Tests** — Unit test: comma-split → `[]string`, empty → error
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Memcached container: cache hit across invocations
- [ ] **Changelog** — Entry
