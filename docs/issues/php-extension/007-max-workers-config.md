# [php-extension] Add `max_workers` to `mesi\parse_with_config()`

## Problem

`EsiParserConfig.MaxWorkers` (0 = `runtime.NumCPU()*4`) not configurable.

## Proposed solution

### PHP API

```php
$config = ['max_workers' => 8];
```

## Acceptance criteria

- [ ] **Tests** — PHPT: value set → config JSON includes it
- [ ] **Tests** — PHPT: 0 → library default
- [ ] **Docs** — Add to README
- [ ] **Changelog** — Entry
