# [php-extension] Add `include_error_marker` to `mesi\parse_with_config()`

## Problem

`EsiParserConfig.IncludeErrorMarker` always empty — failed includes invisible.

## Proposed solution

### PHP API

```php
$config = ['include_error_marker' => '<!-- esi error -->'];
```

## Acceptance criteria

- [ ] **Tests** — PHPT: marker set → renders for failed include
- [ ] **Tests** — PHPT: `onerror="continue"` → marker NOT rendered
- [ ] **Tests** — PHPT: absent → silent
- [ ] **Docs** — Add with security warning
- [ ] **Changelog** — Entry
