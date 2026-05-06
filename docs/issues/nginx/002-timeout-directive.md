# [nginx] Add `timeout` directive

## Problem

`libgomesi/libgomesi.go:58` hardcodes `Timeout` to 30 seconds in `Parse`:

```go
config := mesi.EsiParserConfig{
    DefaultUrl: goDefaultUrl,
    MaxDepth:   uint(goMaxDepth),
    Timeout:    30 * time.Second,  // hardcoded
}
```

The nginx module calls `EsiParse()` which uses this hardcoded value. Operators cannot tune timeout for their backend SLAs. A backend that responds in 200ms still has the full 30s window in case of failure; a backend that needs 45s for a complex include times out unnecessarily.

## Impact

- No timeout tuning — 30s is a one-size-fits-none default.
- Backends with known SLOs (e.g., 500ms p99) cannot enforce tighter timeouts.
- Backends with legitimate slow responses (>30s) cannot raise the limit.
- No timeout monitoring — nginx has no insight into how often includes time out.

## Context

`mesi.EsiParserConfig.Timeout` is `time.Duration`. Currently passed through libgomesi as hardcoded. To make it configurable from nginx, the libgomesi call must accept a timeout parameter.

**Prerequisite**: libgomesi must expose timeout in its CGo API. This requires either:
- Extending `ParseWithConfig` to include `timeoutSeconds` parameter
- Adding a new `ParseJson` function that accepts full JSON config

This issue assumes the libgomesi prerequisite is resolved.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    ngx_flag_t enable_mesi;
    ngx_int_t  max_depth;
    ngx_msec_t timeout;
    // or: ngx_int_t timeout_seconds;
} ngx_http_mesi_loc_conf_t;
```

nginx uses `ngx_msec_t` for milliseconds internally. The directive should accept nginx time syntax (`30s`, `1m`, `500ms`) and convert to seconds (or milliseconds) for the Go side.

### 2. Add directive (seconds as integer)

Simplest approach — integer seconds, matching the available `ngx_conf_set_*_slot` functions:

```c
{ngx_string("mesi_timeout"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, timeout_seconds), NULL},
```

Or use a custom parser for nginx time values:

```c
static char *ngx_conf_set_mesi_timeout(ngx_conf_t *cf, ngx_command_t *cmd, void *conf) {
    ngx_http_mesi_loc_conf_t *lcf = conf;
    ngx_str_t *value = cf->args->elts;

    if (ngx_parse_time(&value[1], 1)) {  // parse "30s" → milliseconds
        lcf->timeout = ngx_parse_time(&value[1], 1);
        return NGX_CONF_OK;
    }
    ngx_conf_log_error(NGX_LOG_EMERG, cf, 0, "invalid timeout \"%V\"", &value[1]);
    return NGX_CONF_ERROR;
}
```

### 3. Default and merge

```c
ngx_conf_merge_value(conf->timeout_seconds, prev->timeout_seconds, 30);
```

### 4. Pass to libgomesi

```c
// Option A: ParseWithConfigExtended (adds timeoutSeconds param)
char *message = EsiParseWithConfigExtended(html, max_depth, base_url,
    allowed_hosts, block_private, timeout_seconds, ...);

// Option B: ParseJson
char json[512];
snprintf(json, sizeof(json),
    "{\"maxDepth\":%d,\"timeout\":%llu,\"defaultUrl\":\"%s\"}",
    max_depth, (unsigned long long)timeout_seconds * 1e9, base_url);
char *message = EsiParseJson(html, json);
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: `mesi_timeout 30` (valid seconds), `mesi_timeout 0` (valid=no timeout), `mesi_timeout -5` (invalid)
- [ ] **Tests** — Unit test timeout value propagation to `ParseJson` JSON
- [ ] **Tests** — Unit test default: unset → 30 seconds
- [ ] **Docs** — Add `mesi_timeout` to README directive reference
- [ ] **Docs** — Document behavior: timeout 0 means no timeout (dangerous, warn about unbounded goroutines)
- [ ] **Functional tests** — nginx integration test:
  - `mesi_timeout 2` → backend include that sleeps 5s → response arrives within 2s with include error/empty
  - `mesi_timeout 30` → backend include that responds in 10s → succeeds
  - `mesi_timeout 0` → no timeout (verify slow include is waited for indefinitely, or up to nginx's own timeout)
- [ ] **Changelog** — Entry in project changelog

## Notes

- nginx time parsing: `ngx_parse_time` parses durations with units (`s`, `m`, `h`, `d`, `w`, `M`, `y`). The result is in milliseconds. This requires a custom setter (not `ngx_conf_set_num_slot`).
- Simpler alternative: accept integer seconds only via `ngx_conf_set_num_slot` + field type `ngx_int_t timeout_seconds`. Less flexible but matches existing nginx directive patterns.
- `time.Duration` in Go is nanoseconds. Multiply seconds by 1e9 when building JSON for `ParseJson`.
- nginx has its own proxy timeouts (`proxy_read_timeout`, etc.) that may interact. The ESI timeout applies to individual `<esi:include>` subrequests, not the overall response. Document this distinction.
- 0 = unlimited timeout is the `EsiParserConfig` default. Warn operators that this can cause goroutine leaks if backends hang.
