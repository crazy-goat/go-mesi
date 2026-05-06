# [php-extension] Add `max_response_size` to `mesi\parse_with_config()`

## Problem

`EsiParserConfig.MaxResponseSize` (default 10 MB) not configurable.

## Proposed solution

### PHP API

```php
$config = ['max_response_size' => 1048576];  // bytes
// 0 = unlimited
```

## Acceptance criteria

- [ ] **Tests** — PHPT: 100 byte limit → 200 byte include rejected
- [ ] **Tests** — PHPT: 0 → unlimited
- [ ] **Tests** — PHPT: absent → default 10 MB
- [ ] **Docs** — Add to README
- [ ] **Changelog** — Entry
