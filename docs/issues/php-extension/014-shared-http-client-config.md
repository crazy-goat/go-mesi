# [php-extension] Add `shared_http_client` to `mesi\parse_with_config()`

## Problem

`EsiParserConfig.HTTPClient` is `nil` — each include creates fresh `http.Client`. No TCP connection reuse.

**Constraint**: PHP calls `parse()` synchronously. Shared client must persist across calls via libgomesi's `InitHTTPClient`.

## Proposed solution

### PHP API

```php
$config = ['shared_http_client' => true];
```

### libgomesi InitHTTPClient (dependency)

```c
// In PHP_MINIT:
InitHTTPClient(1);  // 1 = block private IPs
```

### ParseJson

```go
if cfg.SharedHTTPClient && sharedClient != nil {
    client := *sharedClient
    client.Timeout = config.Timeout
    config.HTTPClient = &client
}
```

## Acceptance criteria

- [ ] **Tests** — PHPT: shared client → reduced TCP connections
- [ ] **Docs** — Add to README
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Blocked on libgomesi `InitHTTPClient`
