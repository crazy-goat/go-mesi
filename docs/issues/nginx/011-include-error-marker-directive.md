# [nginx] Add `include_error_marker` directive

## Problem

`mesi.EsiParserConfig.IncludeErrorMarker` (`mesi/config.go:39-43`) is a string rendered in place of a failed `<esi:include>` when:

1. `onerror="continue"` is NOT set on the tag
2. No fallback content exists inside the `<esi:include>` body

When empty (default), failed includes produce **empty output** — the `<esi:include>` tag and its body are silently replaced with nothing. There is no indication in the rendered HTML that an include failed.

The nginx module has no directive for this field. Failed includes are invisible to developers, QA, and monitoring tools.

## Impact

- "Ghost includes" — content disappears from pages with no trace.
- Developers debugging broken includes have no DOM-level indication.
- Monitoring tools inspecting rendered HTML cannot detect failed includes.
- The existing `onerror="continue"` and fallback-body mechanisms cover some cases, but when those aren't used, failures are silent.

## Context

`mesi/config.go:39-43`:

```go
// IncludeErrorMarker is rendered in place of a failed include when no
// onerror="continue" and no fallback <esi:include> body is present.
// Default: "" (silent). Set to something like "<!-- esi error -->" for
// debugging, but NEVER include the raw error — see security advisory.
IncludeErrorMarker string
```

The security advisory is important: the marker must not leak internal details (error messages, URLs, IPs) to clients.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    ngx_str_t   include_error_marker;
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_include_error_marker"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, include_error_marker), NULL},
```

### 3. Default and merge

```c
if (conf->include_error_marker.len == 0) {
    conf->include_error_marker = prev->include_error_marker;
}
// Default: empty string (silent)
```

### 4. Pass to libgomesi

```json
{"includeErrorMarker": "<!-- esi error -->"}
```

Ensure proper JSON escaping of the marker string (quote characters, backslashes).

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_include_error_marker "<!-- ESI include failed -->";
}
```

For HTML comments (most common use case):

```nginx
mesi_include_error_marker <!-- ESI include failed -->;
```

Note: nginx directive values with spaces need quoting. Without spaces, quotes are optional.

## Acceptance criteria

- [ ] **Tests** — Unit test directive: empty string, `"<!-- esi error -->"`, string with special chars
- [ ] **Tests** — Unit test JSON escaping of marker string (double quotes → `\"`, backslashes → `\\`)
- [ ] **Tests** — Unit test that empty string → `"includeErrorMarker":""` in JSON (no effect)
- [ ] **Docs** — Add directive to README with security warning: marker must NOT contain sensitive info
- [ ] **Docs** — Document that marker is rendered ONLY when `onerror="continue"` is absent AND no fallback body exists
- [ ] **Functional tests** — nginx integration test:
  - `include_error_marker "<!-- esi error -->"` → failed include renders the marker in HTML output
  - No marker set → failed include produces empty output (backward compatible)
  - `include_error_marker` + `onerror="continue"` on the tag → marker NOT rendered (onerror takes priority)
  - `include_error_marker` + fallback body inside `<esi:include>` → marker NOT rendered (fallback body used)
  - Marker string with JSON-special chars (`"` `\` `/`) → correct in output (JSON escaped properly)
- [ ] **Changelog** — Entry in project changelog

## Notes

- The marker is a plain string inserted into HTML output. Common choices: `<!-- esi error -->` (invisible HTML comment), `[ESI ERROR]` (visible placeholder), or empty for production silence.
- Security: never use `IncludeErrorMarker` to include the original error message. Error messages may contain URLs, internal IPs, or stack traces. The marker must be a static string configured by the operator.
- nginx directive parsing: `ngx_conf_set_str_slot` stores the raw string including quotes if present. The marker string should be the raw value without enclosing quotes in the final JSON.
- For `ParseJson`, the marker string needs standard JSON string escaping. A helper function `ngx_str_json_escape` can handle this.
