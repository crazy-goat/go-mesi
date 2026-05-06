# [php-extension] Add `allowed_hosts` to `mesi\parse_with_config()`

## Problem

No host allowlisting. Every `<esi:include>` URL passes unconditionally.

## Proposed solution

### PHP API

```php
$config = ['allowed_hosts' => ['backend.internal', 'cdn.example.com']];
$result = mesi\parse_with_config($input, $config);
```

### C implementation

```c
// Build JSON array from PHP indexed array
smart_str json = {0};
smart_str_appends(&json, "{\"allowedHosts\":[");
zval *hosts = zend_hash_str_find(Z_ARRVAL_P(config), "allowed_hosts", ...);
if (hosts && Z_TYPE_P(hosts) == IS_ARRAY) {
    zval *val;
    zend_bool first = 1;
    ZEND_HASH_FOREACH_VAL(Z_ARRVAL_P(hosts), val) {
        if (!first) smart_str_appendc(&json, ',');
        smart_str_appendc(&json, '"');
        // append escaped host string
        smart_str_appendc(&json, '"');
        first = 0;
    } ZEND_HASH_FOREACH_END();
}
smart_str_appends(&json, "]}");
```

## Acceptance criteria

- [ ] **Tests** — PHPT test: `allowed_hosts: [backend]` → include from `backend` works
- [ ] **Tests** — PHPT test: include from `evil.com` → blocked
- [ ] **Tests** — PHPT test: absent → no restriction
- [ ] **Docs** — Add to README
- [ ] **Changelog** — Entry
