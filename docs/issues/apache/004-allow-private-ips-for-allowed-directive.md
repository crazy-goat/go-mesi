# [apache] Add `MesiAllowPrivateIPsForAllowedHosts` directive

## Problem

Apache already supports `MesiAllowedHosts` and `MesiBlockPrivateIPs` via `ParseWithConfig`. However, `AllowPrivateIPsForAllowedHosts` is missing.

When an operator has both:
- `MesiBlockPrivateIPs On` (block private IPs)
- `MesiAllowedHosts backend.internal` (trusted internal host on private IP)

The include to `backend.internal` (resolving to `10.0.0.5`) is STILL blocked because `BlockPrivateIPs` runs at the dial level regardless of AllowedHosts membership. The operator must choose between security (block all private IPs) and functionality (turn off SSRF protection entirely).

## Impact

- Trusted internal hosts on private IPs are unreachable with SSRF protection enabled.
- Operators in service meshes must disable SSRF entirely, opening the door to attacks.
- No granular control — "all or nothing" for private IP access.

## Context

`mesi/config.go:23-30` defines the field. Core SSRF logic in `mesi/ssrf.go` must implement the bypass (currently TODO). This issue covers the Apache directive; the core logic implementation is a dependency.

`ParseWithConfig` does NOT accept this parameter. Must use `ParseJson` or extended function.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    int allow_private_ips_for_allowed;  // -1=unset, 0=off, 1=on
} mesi_config;
```

### 2. Add directive

```c
AP_INIT_FLAG("MesiAllowPrivateIPsForAllowedHosts", set_allow_private_for_allowed, NULL, RSRC_CONF,
    "Allow private IP access for hosts in MesiAllowedHosts (default: Off)"),
```

### 3. Default and merge

```c
conf->allow_private_ips_for_allowed = -1;  // unset

// Merge:
conf->allow_private_ips_for_allowed = (add->allow_private_ips_for_allowed != -1)
    ? add->allow_private_ips_for_allowed : base->allow_private_ips_for_allowed;
// Final: -1 → 0 (off)
```

### Apache config

```apache
EnableMesi On
MesiBlockPrivateIPs On
MesiAllowedHosts backend.internal api.trusted.org
MesiAllowPrivateIPsForAllowedHosts On
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `On` / `Off`, default `Off`
- [ ] **Tests** — Unit test merge
- [ ] **Docs** — Add directive to README with security warning (DNS-control assumption)
- [ ] **Docs** — Document interaction: only effective when BOTH `MesiBlockPrivateIPs On` AND `MesiAllowedHosts` set
- [ ] **Functional tests** — Apache integration test:
  - `On` + `AllowedHosts backend` + include to `http://10.0.0.5/` (private IP, DNS → backend) → succeeds
  - `Off` + same config → include blocked by BlockPrivateIPs
  - `On` + host NOT in AllowedHosts → private IP still blocked
- [ ] **Changelog** — Entry
- [ ] **Dependency** — Mark blocked until core `ssrf.go` implements the bypass logic

## Notes

- Security: this trusts DNS for hosts in `MesiAllowedHosts`. Only use with internal DNS (Consul, Kubernetes DNS, /etc/hosts).
- The `-1` unset sentinel follows the existing `block_private_ips` pattern (line 40, 47, 57, 286).
