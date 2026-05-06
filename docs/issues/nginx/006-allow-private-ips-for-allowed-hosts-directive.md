# [nginx] Add `allow_private_ips_for_allowed_hosts` directive

## Problem

`mesi.EsiParserConfig.AllowPrivateIPsForAllowedHosts` (`mesi/config.go:23-30`) allows trusted hosts (those in `AllowedHosts`) to bypass the `BlockPrivateIPs` check. When nginx is deployed in a service mesh or internal network where ESI includes legitimately target internal services on private IPs, the operator faces a choice:

- `block_private_ips on` (secure default) → blocks ALL private IP includes, including trusted internal services
- `block_private_ips off` (permissive) → allows ALL private IP includes, including potentially attacker-controlled ones

Neither option is correct. The operator needs to allow private IPs **only** for hosts they trust (DNS-controlled by the operator).

## Impact

- Operators in private networks / service meshes cannot securely use ESI to include from internal services.
- The choice between "block all" and "allow all" is a false dichotomy — operators need granular control.
- Without this feature, operators may disable SSRF protection entirely (choosing `block_private_ips off`), creating a security gap.

## Context

`mesi/config.go:23-30`:

```go
// AllowPrivateIPsForAllowedHosts allows hosts in AllowedHosts to bypass
// the BlockPrivateIPs check.
//
// SECURITY WARNING: This creates a potential SSRF vector if an attacker
// can control DNS for a host in AllowedHosts. Only use in trusted environments.
//
// Default: false (private IPs always blocked regardless of AllowedHosts).
AllowPrivateIPsForAllowedHosts bool
```

**Note**: As of writing, this field is defined in `EsiParserConfig` but the logic in `mesi/ssrf.go` does NOT yet implement the bypass. The `ssrf.go` code checks `AllowedHosts` (hostname level) and `BlockPrivateIPs` (dial level) independently. The `AllowPrivateIPsForAllowedHosts` bypass must be implemented in core first.

This issue covers exposing the field in nginx config. The core logic implementation is a dependency.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    ngx_flag_t enable_mesi;
    ngx_int_t  max_depth;
    ngx_int_t  timeout_seconds;
    ngx_flag_t parse_on_header;
    ngx_flag_t block_private_ips;
    ngx_str_t  allowed_hosts;
    ngx_flag_t allow_private_ips_for_allowed;
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_allow_private_ips_for_allowed"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
 ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, allow_private_ips_for_allowed), NULL},
```

### 3. Default and merge

```c
ngx_conf_merge_value(conf->allow_private_ips_for_allowed, prev->allow_private_ips_for_allowed, 0);  // default OFF
```

### 4. Pass to libgomesi

The existing `ParseWithConfig` does NOT accept this field. Either:

**Option A**: Extend `ParseWithConfig` to add `allowPrivateForAllowed` int parameter (6th param):

```c
char* EsiParseWithConfig6(char* input, int maxDepth, char* defaultUrl,
    char* allowedHosts, int blockPrivateIPs, int allowPrivateForAllowed);
```

**Option B**: `ParseJson` approach — naturally handles boolean fields without parameter creep:

```c
snprintf(json, sizeof(json),
    "{\"blockPrivateIPs\":%s,\"allowedHosts\":[%s],\"allowPrivateIPsForAllowedHosts\":%s}",
    bp, escaped_hosts, lcf->allow_private_ips_for_allowed ? "true" : "false");
```

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_block_private_ips on;
    mesi_allowed_hosts backend.internal api.trusted.org;
    mesi_allow_private_ips_for_allowed on;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive: `on` / `off`
- [ ] **Tests** — Unit test default: unset → `off` (secure default)
- [ ] **Tests** — Unit test field propagation to `ParseJson` / `ParseWithConfigExtended`
- [ ] **Docs** — Add directive to README with security warning about DNS-control assumption
- [ ] **Functional tests** — nginx integration test:
  - `allow_private_ips_for_allowed on` + `allowed_hosts backend` + include to `http://10.0.0.1/` (private IP, DNS pointing to `backend`) → include succeeds (bypassed for trusted host)
  - `allow_private_ips_for_allowed off` + same config → include to `http://10.0.0.1/` blocked
  - `allow_private_ips_for_allowed on` + host NOT in `allowed_hosts` → private IP still blocked
  - Verify that `allow_private_ips_for_allowed on` does not bypass `BlockPrivateIPs` for hosts NOT in `allowed_hosts`
- [ ] **Changelog** — Entry in project changelog
- [ ] **Dependency** — Verify core `ssrf.go` implements the bypass logic before marking this issue as done

## Notes

- **Depends on**: core SSRF implementation of `AllowPrivateIPsForAllowedHosts` in `mesi/ssrf.go`. Mark this issue as blocked until that is resolved.
- Security warning in nginx docs: this feature trusts DNS resolution for hosts in `allowed_hosts`. If attacker controls DNS for a listed host, they can redirect it to a private IP and bypass SSRF protection. Only use with internal DNS (Consul, Kubernetes DNS, /etc/hosts).
- The directive name `mesi_allow_private_ips_for_allowed` is long but self-documenting. nginx directives follow `underscore_lowercase` convention.
- This feature only has effect when BOTH `mesi_block_private_ips on` AND `mesi_allowed_hosts ...` are set. Document this interaction.
