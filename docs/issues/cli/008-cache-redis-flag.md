# [cli] Add `-cacheBackend redis` flag

## Problem

`mesi.NewRedisCache` not available via CLI flags.

## Proposed solution

### Additional flags

```go
var cacheRedisAddr     = flag.String("cacheRedisAddr", "localhost:6379", "Redis address")
var cacheRedisPassword = flag.String("cacheRedisPassword", "", "Redis password")
var cacheRedisDB       = flag.Int("cacheRedisDb", 0, "Redis database number")
```

### Map

```go
case "redis":
    config.Cache = mesi.NewRedisCache(*cacheRedisAddr, *cacheRedisPassword, *cacheRedisDB)
    config.CacheTTL = *cacheTTL
```

### Usage

```bash
mesi-cli -cacheBackend redis -cacheTTL 120s -cacheRedisAddr 10.0.0.5:6379 -cacheRedisDb 2 -url https://example.com/
```

## Acceptance criteria

- [ ] **Tests** — Unit test: flag defaults, backend selection
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Redis container: second invocation with same URL → cache hit from Redis
- [ ] **Changelog** — Entry
