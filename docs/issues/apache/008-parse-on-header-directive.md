# [apache] Add `MesiParseOnHeader` directive

## Problem

`EsiParserConfig.ParseOnHeader` controls whether ESI is processed only when the backend response contains a `Surrogate-Control: content="ESI/1.0"` header. When `false` (default), all HTML responses are parsed regardless.

The Apache filter at `mod_mesi.c:168-178` checks `is_html_content()` and status, but does NOT check for surrogate headers. Backends cannot opt out of ESI processing per-response.

## Impact

- Mixed ESI/non-ESI backends sharing the same Apache server have no per-response control.
- Backend-driven ESI opt-in (via `Surrogate-Control` response header) is not honored.

## Context

The filter already inspects `f->r->content_type` and `f->r->status`. Adding a surrogate header check is a small extension to the filter function.

The header check should happen in `mesi_response_filter` BEFORE body accumulation starts, to avoid buffering responses that won't be processed.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    int parse_on_header;  // -1=unset, 0=off, 1=on
} mesi_config;
```

### 2. Add directive

```c
AP_INIT_FLAG("MesiParseOnHeader", set_parse_on_header, NULL, RSRC_CONF,
    "Only process ESI when backend sets Surrogate-Control header (default: Off)"),
```

### 3. Check in filter

```c
static int mesi_response_filter(ap_filter_t *f, apr_bucket_brigade *bb) {
    mesi_config *conf = ap_get_module_config(f->r->server->module_config, &mesi_module);
    if (!conf->enable_mesi) return ap_pass_brigade(f->next, bb);

    // ParseOnHeader check — BEFORE body buffering
    int parse_on = (conf->parse_on_header != -1) ? conf->parse_on_header : 0;
    if (parse_on) {
        const char *sc = apr_table_get(f->r->headers_out, "Surrogate-Control");
        if (!sc || !strstr(sc, "ESI")) {
            ap_remove_output_filter(f);
            return ap_pass_brigade(f->next, bb);
        }
    }

    // ... existing content_type, status checks ...
}
```

### Apache config

```apache
EnableMesi On
MesiParseOnHeader On
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `On` / `Off`, default `Off`
- [ ] **Tests** — Unit test surrogate header check: `Surrogate-Control: content="ESI/1.0"` → match
- [ ] **Tests** — Unit test: `Surrogate-Control: max-age=60` (no ESI marker) → no match
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Apache integration test:
  - `MesiParseOnHeader On` + backend returns no ESI header → ESI NOT processed
  - `MesiParseOnHeader On` + backend returns `Surrogate-Control: content="ESI/1.0"` → ESI processed
  - `MesiParseOnHeader Off` → ESI always processed for HTML (backward compat)
- [ ] **Changelog** — Entry

## Notes

- `apr_table_get` returns `NULL` if header not found. Check for NULL before `strstr`.
- The check uses simple `strstr(sc, "ESI")` substring match. Spec requires `content="ESI/1.0"` parameter. Substring is more lenient but acceptable.
- The `parse_on_header` flag must also be passed to `EsiParserConfig` via JSON so the Go side respects it for edge cases.
- Perf: header check avoids body buffering when ESI is not signaled. This is the main value of this feature.
