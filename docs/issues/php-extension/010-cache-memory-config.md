# [php-extension] Add `cache_backend memory` to `mesi\parse_with_config()`

## Problem

`EsiParserConfig.Cache` is `nil` — no caching.

**Constraint**: Cache is ephemeral — each `parse_with_config()` call creates a fresh `EsiParserConfig`. For caching to work, libgomesi must maintain a shared cache instance.

## Proposed solution

### PHP API

```php
$config = ['cache_backend' => 'memory', 'cache_size' => 5000, 'cache_ttl' => 60];
```

### libgomesi persistent cache (dependency)

Requires `InitCache` export from libgomesi, called once per process:

```c
// In PHP_MINIT:
InitCache("memory", 5000, 30);
```

### ParseJson with cache reference

```go
// In ParseJson: if sharedCache != nil, use it in EsiParserConfig
```

## Acceptance criteria

- [ ] **Tests** — PHPT: cache hits within TTL
- [ ] **Tests** — PHPT: TTL expiry → cache miss
- [ ] **Docs** — Add to README with per-process scope
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked on libgomesi `InitCache`
