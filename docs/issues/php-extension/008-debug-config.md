# [php-extension] Add `debug` to `mesi\parse_with_config()`

## Problem

`EsiParserConfig.Debug` not configurable. No debug logging from PHP.

## Proposed solution

### PHP API

```php
$config = ['debug' => true];
```

## Acceptance criteria

- [ ] **Tests** — PHPT: `true` → debug messages on stderr
- [ ] **Tests** — PHPT: `false` / absent → silent
- [ ] **Docs** — Add to README, note output to stderr
- [ ] **Changelog** — Entry
