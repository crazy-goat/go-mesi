# [php-extension] Add `allow_private_ips_for_allowed_hosts` to `mesi\parse_with_config()`

## Problem

When `block_private_ips: true` and `allowed_hosts` lists internal hosts, includes are still blocked at dial level.

## Proposed solution

### PHP API

```php
$config = [
    'block_private_ips' => true,
    'allowed_hosts' => ['backend.internal'],
    'allow_private_ips_for_allowed_hosts' => true,
];
```

## Acceptance criteria

- [ ] **Tests** — PHPT: bypass works for listed host, not for unlisted
- [ ] **Tests** — absent → `false`
- [ ] **Docs** — Add with DNS-control security warning
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked on core `ssrf.go` bypass
