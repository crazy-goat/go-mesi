# [php-extension] Add `block_private_ips` to `mesi\parse_with_config()`

## Problem

Old `Parse` does NOT set `BlockPrivateIPs` (defaults to `false` struct zero-value). **NO SSRF protection** — `http://169.254.169.254/` metadata endpoints reachable.

## Proposed solution

### PHP API

```php
$config = ['block_private_ips' => true];  // default MUST be true
$result = mesi\parse_with_config($input, $config);
```

### Default: `true` (BREAKING from old `parse()` which had no protection)

```c
zend_bool block_private = 1;  // default ON (security hardening)
if (config) {
    zv = zend_hash_str_find(Z_ARRVAL_P(config), "block_private_ips", sizeof("block_private_ips")-1);
    if (zv) {
        block_private = zend_is_true(zv);
    }
}
```

## Acceptance criteria

- [ ] **Tests** — `true` → include to `http://127.0.0.1/` blocked
- [ ] **Tests** — `false` → include to `http://127.0.0.1/` succeeds
- [ ] **Tests** — absent → default `true` (security hardening)
- [ ] **Docs** — Add to README with BREAKING note: old `parse()` has NO protection, new default is `true`
- [ ] **Docs** — Security advisory about upgrading from `parse()` to `parse_with_config()`
- [ ] **Changelog** — Entry with BREAKING CHANGE
