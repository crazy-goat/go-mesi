# [nginx] Add `debug` directive

## Problem

`mesi.EsiParserConfig.Debug` (`mesi/config.go:37`) enables debug logging through the Go logger:

```go
Debug bool  // Enable debug logging
```

When `true`, the parser emits debug-level logs for include resolution, fetch timing, depth tracking, and error conditions. The nginx module has no way to enable this — no directive, no log bridge.

When `false` (default), the parser is completely silent. Operators troubleshooting ESI processing issues have no visibility into:
- Which includes were resolved
- HTTP status codes of include fetches
- Timeout and depth-limit enforcement
- URL resolution (relative → absolute)

## Impact

- "Silent failure" problem — when an ESI include returns 500, produces empty content, or times out, the nginx operator has zero diagnostics.
- Debugging requires modifying and recompiling the module or libgomesi.
- No production-safe way to temporarily enable diagnostics for a specific location.

## Context

The `Debug` field controls `mesi.EsiParserConfig.getLogger()` (`mesi/config.go:62-70`):

```go
func (c EsiParserConfig) getLogger() Logger {
    if c.Logger != nil { return c.Logger }
    if c.Debug { return DefaultLoggerNew() }
    return discardLogger
}
```

`DefaultLoggerNew()` writes to stderr via `log.New(os.Stderr, "[mesi] ", log.LstdFlags)`. In the CGo context, stderr goes to nginx's stderr (which is typically captured in nginx error log or journald).

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    ngx_flag_t  debug;
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_debug"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
 ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, debug), NULL},
```

### 3. Default and merge

```c
ngx_conf_merge_value(conf->debug, prev->debug, 0);  // default off
```

### 4. Pass to libgomesi

```json
{"debug": true}
```

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_debug on;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive: `on` / `off`
- [ ] **Tests** — Unit test default: unset → `off`
- [ ] **Tests** — Unit test that `debug: true` propagates to `ParseJson` config
- [ ] **Docs** — Add directive to README with note about output location (stderr → nginx error log)
- [ ] **Docs** — Document that debug output goes to Go stderr, visible in nginx error log if `error_log stderr;`
- [ ] **Functional tests** — nginx integration test:
  - `debug on` with a page containing ESI includes → verify debug messages appear in nginx error log (or container stdout)
  - `debug off` → no debug output
  - `debug on` + failed include → include fetch error messages appear in debug output
- [ ] **Changelog** — Entry in project changelog

## Notes

- Go's `log.Default()` writes to `os.Stderr`. In the nginx process, this goes to fd 2, which nginx typically opens to its error log or inherits from the parent process. Verify that debug output is accessible without redirecting nginx's stderr to a file.
- Alternative: bridge Go logs to nginx's native logging. This requires writing a Go `Logger` implementation that calls back to C, which calls `ngx_log_error`. This is complex and should be a separate enhancement issue.
- `Debug: true` should NOT be used in production long-term due to stderr volume. Document this.
- Debug output includes URLs of include fetches. This is a potential information leak if error logs are shared. Warn in docs.
