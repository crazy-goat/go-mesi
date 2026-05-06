# [apache] Add `MesiIncludeErrorMarker` directive

## Problem

`servers/apache/mod_mesi.c:301` calls `EsiParseWithConfig` which does not pass `IncludeErrorMarker`:

```c
esi = EsiParseWithConfig(html, 5, base_url, allowed_hosts_str, block_private);
```

When an `<esi:include>` fails (timeout, connection refused, blocked by SSRF) and the tag has no `onerror="continue"` and no fallback body, the parser produces empty output. There is no visible indication in the rendered HTML that an include failed.

## Impact

- Silent include failures — content disappears from pages with no trace.
- Operators troubleshooting broken includes cannot detect them in rendered output.
- No staging/production toggle for showing/hiding include errors.

## Context

`mesi/config.go:39-43`:

```go
IncludeErrorMarker string  // rendered for failed includes (when no onerror/fallback)
```

Current `ParseWithConfig` (libgomesi:76-96) does NOT accept this field. Must be passed via:
- `ParseWithConfigExtended` (add more params)
- `ParseJson` (recommended — no CGo parameter limit)

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    int enable_mesi;
    apr_array_header_t *allowed_hosts;
    int block_private_ips;       // -1=unset, 0=off, 1=on
    const char *include_error_marker;  // NULL or "" = silent
} mesi_config;
```

### 2. Add directive

```c
static const char *set_include_error_marker(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    conf->include_error_marker = arg;
    return NULL;
}
```

Directive entry:
```c
AP_INIT_TAKE1("MesiIncludeErrorMarker", set_include_error_marker, NULL, RSRC_CONF,
    "Marker string rendered for failed ESI includes (e.g., '<!-- esi error -->')"),
```

### 3. Default in create_server_config

```c
conf->include_error_marker = NULL;  // NULL = silent
```

### 4. Merge

```c
conf->include_error_marker = (add->include_error_marker) ? add->include_error_marker : base->include_error_marker;
```

### 5. Pass to libgomesi

Via `ParseJson`:

```c
char json_cfg[2048];
apr_snprintf(json_cfg, sizeof(json_cfg),
    "{\"maxDepth\":5,\"defaultUrl\":\"%s\",\"allowedHosts\":\"%s\","
    "\"blockPrivateIPs\":%s,\"includeErrorMarker\":\"%s\"}",
    escaped_url, escaped_hosts,
    block_private ? "true" : "false",
    conf->include_error_marker ? conf->include_error_marker : "");
```

### Apache config example

```apache
EnableMesi On
MesiIncludeErrorMarker "<!-- ESI include failed -->"
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: valid string, empty string, no argument (error)
- [ ] **Tests** — Unit test merge: child sets, parent NULL → child's value
- [ ] **Tests** — Unit test merge: child NULL, parent sets → parent's value
- [ ] **Tests** — Unit test JSON escaping (quotes, backslashes in marker string)
- [ ] **Docs** — Add `MesiIncludeErrorMarker` to `servers/apache/README.md` directive reference
- [ ] **Docs** — Security warning: marker must NOT contain error details, URLs, or internal IPs
- [ ] **Functional tests** — Apache integration test:
  - `MesiIncludeErrorMarker "<!-- err -->"` → failed include renders `<!-- err -->` in HTML
  - No marker set → failed include produces empty output (backward compat)
  - Marker with `onerror="continue"` on tag → marker NOT rendered (onerror takes priority)
  - Marker with fallback body inside `<esi:include>` → marker NOT rendered (fallback used)
- [ ] **Changelog** — Entry in project changelog

## Notes

- Apache uses `apr_pstrdup` for string copies from config. The directive setter receives `arg` from Apache's config pool — it's already allocated. Store the pointer directly.
- JSON string escaping for the marker: need to escape `"`, `\`, and control characters. Use a helper `apr_json_escape(pool, str)` or inline the escaping.
- `AP_INIT_TAKE1` requires exactly one argument. For empty marker, use `MesiIncludeErrorMarker ""` (explicit empty) or omit the directive entirely (NULL = silent).
- This directive is `RSRC_CONF` (server-level only), matching existing Apache directives. Per-directory override is not supported.
