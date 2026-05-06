# [php-extension] Add `max_concurrent_requests` to `mesi\parse_with_config()`

## Problem

`EsiParserConfig.MaxConcurrentRequests` (0 = unlimited) not configurable.

**Constraint**: PHP calls `parse()` synchronously from a single PHP worker thread. The concurrency limit applies within one `MESIParse` call (goroutine pool in Go). It works in CGo context — Go goroutines are transparent.

## Proposed solution

### PHP API

```php
$config = ['max_concurrent_requests' => 5];
```

## Acceptance criteria

- [ ] **Tests** — PHPT: 20 includes with limit 3 → funneled
- [ ] **Tests** — PHPT: 0 → unlimited
- [ ] **Docs** — Add to README
- [ ] **Changelog** — Entry

## Notes

- Limit is per-`parse_with_config()` call, not global across PHP requests.
