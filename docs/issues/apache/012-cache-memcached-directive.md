# [apache] Add Memcached cache backend (`MesiCacheBackend memcached`)

## Problem

`mesi.NewMemcachedCache(servers ...string)` provides Memcached-backed caching with consistent hashing across servers. Apache has no directive.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    apr_array_header_t *cache_memcached_servers;  // "host:port" strings
} mesi_config;
```

### 2. Add directive

```c
static const char *set_cache_memcached_servers(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = ap_get_module_config(cmd->server->module_config, &mesi_module);
    // Parse space-separated host:port list (same pattern as set_allowed_hosts)
    const char *host;
    while (*arg) {
        while (*arg && (*arg == ' ' || *arg == '\t')) arg++;
        host = arg;
        while (*arg && *arg != ' ' && *arg != '\t') arg++;
        if (host != arg) {
            const char **srv = apr_array_push(conf->cache_memcached_servers);
            *srv = apr_pstrndup(cmd->pool, host, arg - host);
        }
    }
    return NULL;
}
```

Directive:
```c
AP_INIT_RAW_ARGS("MesiCacheMemcachedServers", set_cache_memcached_servers, NULL, RSRC_CONF,
    "Space-separated list of Memcached servers (host:port)"),
```

### Apache config

```apache
MesiCacheBackend memcached
MesiCacheTTL 120
MesiCacheMemcachedServers 10.0.0.1:11211 10.0.0.2:11211
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: single, multiple servers
- [ ] **Tests** — Unit test empty server list → error or fallback
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Integration test with Memcached container:
  - Two requests → second hits cache
  - TTL expiry test
  - Multiple servers → consistent hashing distributes entries
- [ ] **Changelog** — Entry

## Notes

- Memcached 1 MB value limit. Larger include responses cannot be cached.
- Server list uses space separator (consistent with `MesiAllowedHosts`).
- `apr_array_push` pattern from existing `set_allowed_hosts`.
- Default port 11211 if omitted. Recommend explicit port for clarity.
