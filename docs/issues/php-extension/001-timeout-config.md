# [php-extension] Add `timeout` to `mesi\parse_with_config()`

## Problem

`libgomesi/libgomesi.go:58` — `Parse` hardcodes `Timeout` to 30 seconds. Not configurable from PHP.

## Context

PHP extension calls `Parse(input, max_depth, default_url)` (`php-ext/mesi.c:42`). Requires new `parse_with_config()` function accepting a config array, and new `ParseJson` export from libgomesi.

## Proposed solution

### PHP API

```php
$config = ['timeout' => 10];  // seconds
$result = mesi\parse_with_config($input, $config);
```

### C implementation

```c
// In ZEND_FUNCTION(parse_with_config):
zend_long timeout = 30;  // default
if (config) {
    zval *zv = zend_hash_str_find(Z_ARRVAL_P(config), "timeout", sizeof("timeout")-1);
    if (zv && Z_TYPE_P(zv) == IS_LONG) {
        timeout = Z_LVAL_P(zv);
    }
}
// Build JSON: {"timeout": 10000000000}
snprintf(json, len, "{\"timeout\":%lld}", (long long)timeout * 1000000000LL);
char *result = ParseJson(input, json);
```

## Acceptance criteria

- [ ] **Tests** — PHPT test: `timeout: 2` + slow include → timeout at 2s
- [ ] **Tests** — PHPT test: `timeout: 30` + normal include → success
- [ ] **Tests** — PHPT test: absent → default 30s
- [ ] **Docs** — Add to `php-ext/README.md`
- [ ] **Changelog** — Entry

## Notes

- Depends on libgomesi `ParseJson` function.
- Timeout in seconds (int), converted to nanoseconds for Go.
- Old `mesi\parse()` keeps 30s hardcoded timeout (backward compat).
