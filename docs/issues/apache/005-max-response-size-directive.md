# [apache] Add `MesiMaxResponseSize` directive

## Problem

`mesi.EsiParserConfig.MaxResponseSize` (default 10 MB) limits the response body size of `<esi:include>` fetches. Apache's `ParseWithConfig` call at `mod_mesi.c:301` does not pass this field — the 10 MB default is implicit.

- Operators who want stricter limits (e.g., 1 MB for includes expected to be small) cannot set them.
- Operators with large include responses (>10 MB) get silent truncation.
- The 10 MB ceiling is invisible to operators until a production response hits it.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    apr_off_t max_response_size;  // bytes, -1 = unset (use default 10 MB)
} mesi_config;
```

`apr_off_t` is Apache's off_t equivalent — typically `int64_t` on 64-bit.

### 2. Add directive

```c
static const char *set_max_response_size(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    apr_off_t val;
    if (apr_strtoff(&val, arg, NULL, 10) != APR_SUCCESS || val < 0) {
        return "MesiMaxResponseSize must be a non-negative integer (bytes)";
    }
    conf->max_response_size = val;
    return NULL;
}
```

Directive:
```c
AP_INIT_TAKE1("MesiMaxResponseSize", set_max_response_size, NULL, RSRC_CONF,
    "Maximum ESI include response body size in bytes (default: 10485760, 0 = unlimited)"),
```

### 3. Default and merge

```c
conf->max_response_size = -1;  // unset → default 10 MB

// Merge:
conf->max_response_size = (add->max_response_size != -1) ? add->max_response_size : base->max_response_size;
```

### Apache config

```apache
MesiMaxResponseSize 1048576  # 1 MB
```

## Acceptance criteria

- [ ] **Tests** — Unit test: valid bytes, 0 (unlimited), negative (error)
- [ ] **Tests** — Unit test: apr_strtoff conversion
- [ ] **Tests** — Unit test merge
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Apache integration test:
  - `MesiMaxResponseSize 100` → include returning 200 bytes → rejected
  - `MesiMaxResponseSize 1048576` → include returning 500 KB → succeeds
  - `MesiMaxResponseSize 0` → unlimited → 50 MB include succeeds
- [ ] **Changelog** — Entry

## Notes

- Default 10 MB matches `CreateDefaultConfig()` in `mesi/config.go:94`.
- 0 = unlimited: risk of memory exhaustion from large includes.
- The limit is per-SINGLE-include, not total page size.
- `apr_strtoff` returns `APR_SUCCESS` on valid conversion. Check both return value and range.
