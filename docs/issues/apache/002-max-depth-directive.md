# [apache] Add `MesiMaxDepth` directive

## Problem

`servers/apache/mod_mesi.c:301` hardcodes max depth to 5:

```c
esi = EsiParseWithConfig(html, 5, base_url, allowed_hosts_str, block_private);
```

Same hardcoded value in the `EsiParse` fallback path (line 322). Operators cannot change ESI nesting depth.

## Impact

- Pages with nesting > 5 silently drop innermost includes.
- Simple deployments (depth=1) pay the full parse cost.
- No way to increase depth for deeply nested applications.

## Context

Apache already has a pattern for `mesi_config` extension (3 fields, each with setter + merge). `MesiMaxDepth` follows the same pattern.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    int enable_mesi;
    apr_array_header_t *allowed_hosts;
    int block_private_ips;
    const char *include_error_marker;
    int max_depth;  // -1=unset
} mesi_config;
```

### 2. Add directive setter

```c
static const char *set_max_depth(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int val = atoi(arg);
    if (val < 0) {
        return "MesiMaxDepth must be a non-negative integer";
    }
    conf->max_depth = val;
    return NULL;
}
```

### 3. Default and merge

```c
conf->max_depth = -1;  // unset in create_server_config

// In merge:
conf->max_depth = (add->max_depth != -1) ? add->max_depth : base->max_depth;
// Final default in filter:
int depth = (conf->max_depth != -1) ? conf->max_depth : 5;
```

### 4. Use in filter

```c
int depth = (conf->max_depth != -1) ? conf->max_depth : 5;
esi = EsiParseWithConfig(html, depth, base_url, allowed_hosts_str, block_private);
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `MesiMaxDepth 3`, `MesiMaxDepth 0`, `MesiMaxDepth 100`
- [ ] **Tests** — Unit test: `MesiMaxDepth -1` → error
- [ ] **Tests** — Unit test merge: child sets 3, parent unset → 3
- [ ] **Tests** — Unit test merge: child unset, parent sets 10 → 10
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Apache integration test:
  - `MesiMaxDepth 1` → 2-level nested include: inner NOT processed
  - `MesiMaxDepth 5` → 2-level nested: both processed
  - Default (unset) → depth 5 (backward compat)
- [ ] **Changelog** — Entry
