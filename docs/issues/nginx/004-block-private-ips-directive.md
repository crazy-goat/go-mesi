# [nginx] Add `block_private_ips` directive (SSRF enable/disable)

## Problem

`libgomesi/libgomesi.go:51-61` — the `Parse` function used by nginx does NOT set `BlockPrivateIPs`:

```go
func Parse(input *C.char, maxDepth C.int, defaultUrl *C.char) *C.char {
    config := mesi.EsiParserConfig{
        DefaultUrl: goDefaultUrl,
        MaxDepth:   uint(goMaxDepth),
        Timeout:    30 * time.Second,
        // BlockPrivateIPs NOT SET → defaults to false
    }
}
```

The nginx module at `ngx_http_mesi_module.c:236` calls `EsiParse` (which maps to `Parse`). **No SSRF protection is active**. An attacker controlling an `<esi:include>` tag can target:

- `http://127.0.0.1/` (localhost)
- `http://169.254.169.254/latest/meta-data/` (AWS metadata)
- `http://10.0.0.1/` (internal networks)
- Any RFC 1918 / CGNAT / benchmark / documentation IP

This is the single most critical security gap in the nginx integration.

## Impact

- **High-severity SSRF vector** — unlike Apache and Go servers which default `BlockPrivateIPs: true`, nginx has no protection.
- Cloud metadata services are reachable via ESI includes.
- Internal services behind nginx are reachable from external requests.
- The `allowed_hosts` directive (separate issue) is useless without IP-level blocking as a defense-in-depth measure.

## Context

`mesi.ssrf.go:93-110` — `safeDialer` checks `config.BlockPrivateIPs` at TCP dial time. When `true`, connections to private/reserved IPs are rejected with `ErrSSRFBlocked`.

`mesi/config.go:93` — `CreateDefaultConfig()` sets `BlockPrivateIPs: true`.

**Prerequisite**: The nginx module must migrate from `Parse` to `ParseWithConfig` (or `ParseJson`) which accepts `blockPrivateIPs` as a parameter.

`ParseWithConfig` already supports `blockPrivateIPs` (line 76):

```c
char* EsiParseWithConfig(char* input, int maxDepth, char* defaultUrl,
                         char* allowedHosts, int blockPrivateIPs);
```

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    ngx_flag_t enable_mesi;
    ngx_int_t  max_depth;
    ngx_int_t  timeout_seconds;
    ngx_flag_t parse_on_header;
    ngx_flag_t block_private_ips;
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_block_private_ips"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
 ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, block_private_ips), NULL},
```

### 3. Default and merge

```c
ngx_conf_merge_value(conf->block_private_ips, prev->block_private_ips, 1);  // default ON
```

**Critical**: default is `1` (on). This is a behavior change from the current implicit `off` (zero-value). Existing deployments with ESI includes to private IPs will break after upgrade. Document this prominently in changelog.

### 4. Migrate to ParseWithConfig

Instead of `EsiParse` (3-arg), load and call `EsiParseWithConfig` (5-arg):

```c
// New function pointer type
typedef char *(*ParseWithConfigFunc)(char *, int, char *, char *, int);
static ParseWithConfigFunc EsiParseWithConfig = NULL;

// Load in init
EsiParseWithConfig = (ParseWithConfigFunc)dlsym(go_module, "ParseWithConfig");

// Call in parse()
char *message = EsiParseWithConfig(
    ngx_str_to_cstr(&input, r->pool),
    (int)lcf->max_depth,
    ngx_str_to_cstr(&base_url, r->pool),
    "",  // allowedHosts — added in separate issue
    lcf->block_private_ips
);
```

### Alternative: ParseJson

If `ParseJson` is implemented in libgomesi (recommended for concurrent directive additions):

```c
char json[1024];
snprintf(json, sizeof(json),
    "{\"maxDepth\":%d,\"defaultUrl\":\"%s\",\"blockPrivateIPs\":%s,\"timeout\":%llu}",
    max_depth, base_url,
    lcf->block_private_ips ? "true" : "false",
    (unsigned long long)timeout_ns);
char *message = EsiParseJson(html, json);
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive: `mesi_block_private_ips on`, `mesi_block_private_ips off`
- [ ] **Tests** — Unit test default: unset → `on` (security-hardened default)
- [ ] **Tests** — Unit test that `ParseWithConfig` is loaded and called correctly from nginx
- [ ] **Tests** — Verify `ParseWithConfig(html, 5, url, "", 1)` blocks private IP access at Go level
- [ ] **Docs** — Add `mesi_block_private_ips` to README directive reference with security implications
- [ ] **Docs** — Document breaking change: upgrade from unchecked SSRF to blocked-by-default
- [ ] **Functional tests** — nginx integration test:
  - `block_private_ips on` (default) → `<esi:include src="http://127.0.0.1:8000/test.html">` → include blocked, error rendered
  - `block_private_ips on` → `<esi:include src="http://169.254.169.254/">` → blocked
  - `block_private_ips on` → `<esi:include src="http://10.0.0.1/">` → blocked
  - `block_private_ips off` → `<esi:include src="http://127.0.0.1:8000/test.html">` → include succeeds
  - Verify blocked includes do NOT crash nginx worker
  - Verify error log contains SSRF blocking event (at appropriate log level)
- [ ] **Changelog** — Entry in project changelog with **BREAKING CHANGE** warning
- [ ] **Security** — Confirm that `ParseWithConfig` is loaded (not silently falling back to old `Parse` which has no blocking)

## Notes

- **BREAKING CHANGE**: Current behavior implicitly allows private IPs (zero-value `false` for `BlockPrivateIPs`). New default is `on`. Operators with intentional private-IP includes must explicitly set `mesi_block_private_ips off`.
- nginx still needs to load both `Parse` (backward compat) and `ParseWithConfig`. The existing `EsiParse` can be deprecated but kept.
- `ParseWithConfig` also accepts `allowedHosts` (4th param). Pass `""` (empty string) for now — the AllowedHosts directive is a separate issue. `libgomesi:83` handles empty string correctly (yields empty `[]string`).
- If using the `ParseJson` approach, `blockPrivateIPs` boolean serializes naturally to JSON `true`/`false`.
- The `EsiParseWithConfig` C string result must be freed with `free()` after copying to nginx pool memory (line 242 of current code already does this).
- Consider emitting a warning-level log message when an include is blocked: `ngx_log_error(NGX_LOG_WARN, r->connection->log, 0, "mESI: blocked ESI include to private IP: %s", blocked_url);`. This requires capturing the blocked URL — may need a separate issue.
