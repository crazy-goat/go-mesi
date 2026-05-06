# [php-extension] Add `cache_key_template` to `mesi\parse_with_config()`

## Problem

`CacheKeyFunc` is Go function pointer — cannot be passed from PHP. URL-only cache key caches incorrectly for header/cookie-dependent includes.

## Proposed solution

Template-based, same as all other servers:

### PHP API

```php
$config = [
    'cache_backend' => 'redis',
    'cache_key_template' => 'mesi:${url}:${header:Accept-Language}',
];
```

Template evaluation happens in libgomesi's `buildCacheKey()`. ParseJson receives the template string and wraps it in a Go closure.

## Acceptance criteria

- [ ] **Tests** — PHPT: `${url}`, `${header:X}`, `${cookie:Y}` substitution
- [ ] **Docs** — Document template syntax
- [ ] **Changelog** — Entry
