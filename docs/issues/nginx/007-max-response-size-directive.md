# [nginx] Add `max_response_size` directive

## Problem

`mesi.EsiParserConfig.MaxResponseSize` (`mesi/config.go:31`) limits the total response body size of an ESI include fetch. Default: 10 MB (10*1024*1024). When exceeded, the include is rejected.

```go
MaxResponseSize int64  // 0 = unlimited, default 10MB
```

The nginx module has no way to control this. The 10 MB default is inherited from the library but:
- Operators who want a tighter limit (e.g., 1 MB for includes expected to be small) cannot set it.
- Operators with legitimate large includes (>10 MB) cannot raise or disable the limit.
- There is no warning when a response exceeds the limit — the include silently produces empty/error output.

## Impact

- Large ESI responses are silently truncated with no diagnostic.
- Operators are unaware of the 10 MB ceiling until a production response hits it.
- No mechanism to disable the limit for known-large backend responses.

## Context

The `MaxResponseSize` limit is enforced at the Go level in `mesi/fetch.go` (the HTTP fetch logic for `<esi:include>`). It works regardless of whether the call comes from Go or CGo — the library handles it transparently.

Current `ParseWithConfig` signature does NOT accept `MaxResponseSize`. It must be passed via:
- Extended `ParseWithConfig` function (parameter explosion, 7th+ param)
- `ParseJson` (recommended — no parameter limit)

```go
// In ParseJson:
var cfg struct {
    MaxResponseSize int64 `json:"maxResponseSize"`
    // ...
}
json.Unmarshal([]byte(jsonConfig), &cfg)
```

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    off_t       max_response_size;  // bytes, 0 = unlimited
} ngx_http_mesi_loc_conf_t;
```

`ngx_int_t` (32-bit on 32-bit systems) is insufficient for values above 2 GB. Use `off_t` (typically `int64_t` on 64-bit nginx) or `ngx_int_t` if accepting that 10 MB is a practical ceiling.

### 2. Add directive

Accept bytes or human-readable sizes (e.g., `10m`, `1g`):

```c
static char *ngx_conf_set_mesi_max_response_size(ngx_conf_t *cf, ngx_command_t *cmd, void *conf) {
    ngx_http_mesi_loc_conf_t *lcf = conf;
    ngx_str_t *value = cf->args->elts;
    ssize_t size = ngx_parse_size(&value[1]);
    if (size == NGX_ERROR) {
        ngx_conf_log_error(NGX_LOG_EMERG, cf, 0, "invalid size \"%V\"", &value[1]);
        return NGX_CONF_ERROR;
    }
    lcf->max_response_size = size;
    return NGX_CONF_OK;
}

// Directive:
{ngx_string("mesi_max_response_size"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_mesi_max_response_size, 0, 0, NULL},
```

### 3. Default and merge

```c
if (conf->max_response_size == 0) {
    conf->max_response_size = prev->max_response_size;
}
// Factory default: 10 * 1024 * 1024 (10 MB) or 0 (unlimited — library default)
```

### 4. Pass to libgomesi

```c
// ParseJson:
snprintf(json, sizeof(json),
    "{\"maxResponseSize\":%lld}", (long long)lcf->max_response_size);
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: `10m` (10485760 bytes), `1g`, `0` (unlimited), `-1` (invalid)
- [ ] **Tests** — Unit test `ngx_parse_size` conversion: verify correct byte values
- [ ] **Tests** — Unit test 0 value → "unlimited" in JSON
- [ ] **Docs** — Add `mesi_max_response_size` to README with format, default (10 MB), behavior on limit exceeded
- [ ] **Functional tests** — nginx integration test:
  - `max_response_size 100` → include pointing to backend returning 200-byte body → include rejected/truncated
  - `max_response_size 1m` → include returning 500 KB body → include succeeds
  - `max_response_size 0` (unlimited) → include returning 50 MB body → succeeds (within available memory)
  - Verify that rejection does NOT crash nginx worker
  - Verify include rejection produces log message at appropriate level
- [ ] **Changelog** — Entry in project changelog

## Notes

- `ngx_parse_size` returns `ssize_t` which is architecture-dependent but practically `int64_t`. Map to `int64` for `EsiParserConfig.MaxResponseSize`.
- The 10 MB default in `CreateDefaultConfig()` is reasonable. Keep it as the nginx default unless there's a strong reason to differ.
- 0 = unlimited: document the risk. A single `<esi:include>` pointing to an unbounded backend could exhaust nginx worker memory.
- The limit is per-SINGLE-include, not total response size. If a page has 10 includes each under the limit, total can exceed it. Document this.
- `ngx_parse_size` supports `k`, `m`, `g` suffixes (case-insensitive). Example: `mesi_max_response_size 20m` = 20 megabytes.
