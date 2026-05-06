# [nginx] Add `parse_on_header` directive

## Problem

`mesi.EsiParserConfig.ParseOnHeader` (`mesi/config.go:15`) controls whether ESI processing is conditioned on the presence of a `Surrogate-Control: content="ESI/1.0"` or `Edge-Control` header in the backend response. When `false` (default), ESI is always processed for HTML content regardless of header presence.

The nginx module simply does not expose this field. All HTML responses are processed unconditionally (after Content-Type and status checks in `ngx_http_html_mesi_head_filter` at line 97-107).

## Impact

- Backends cannot opt out of ESI processing per-response.
- Mixed ESI/non-ESI backends sharing the same nginx location have no control mechanism.
- Administrators managing CDN-origin relationships where origins signal ESI intent via headers cannot honor those signals.

## Context

Unlike other config fields, `ParseOnHeader` affects the head filter logic, not just the `EsiParserConfig` passed to `Parse`. When enabled:

1. In `ngx_http_html_mesi_head_filter`, check for `Surrogate-Control` or `Edge-Control` header in the response
2. Only set up the accumulation context and `Surrogate-Capability` header if ESI processing is signaled
3. Pass `ParseOnHeader: true` to `EsiParserConfig`

Current head filter already inspects response headers (Content-Type, Content-Encoding, status). Adding a `Surrogate-Control` check is a small extension.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    ngx_flag_t enable_mesi;
    ngx_int_t  max_depth;
    ngx_int_t  timeout_seconds;
    ngx_flag_t parse_on_header;
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_parse_on_header"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
 ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, parse_on_header), NULL},
```

### 3. Default and merge

```c
ngx_conf_merge_value(conf->parse_on_header, prev->parse_on_header, 0);  // default off
```

### 4. Check in head filter

```c
static ngx_int_t ngx_http_html_mesi_head_filter(ngx_http_request_t *r) {
    ngx_http_mesi_loc_conf_t *lcf =
        ngx_http_get_module_loc_conf(r, ngx_http_mesi_module);
    if (!lcf->enable_mesi) {
        return ngx_http_next_header_filter(r);
    }

    // ParseOnHeader check — before other checks
    if (lcf->parse_on_header) {
        ngx_table_elt_t *sc = r->headers_out.surrogate_control;  // nginx 1.23+
        // Alternative: iterate headers_out.headers list
        if (!has_esi_header(r)) {  // helper function
            return ngx_http_next_header_filter(r);
        }
    }

    // ... existing Content-Type, status, compression checks ...
}
```

### 5. Helper: detect ESI header

```c
static ngx_int_t has_esi_header(ngx_http_request_t *r) {
    ngx_list_part_t *part = &r->headers_out.headers.part;
    ngx_table_elt_t *h = part->elts;
    ngx_uint_t i;

    for (i = 0; /* void */; i++) {
        if (i >= part->nelts) {
            if (part->next == NULL) break;
            part = part->next;
            h = part->elts;
            i = 0;
        }

        if (h[i].hash == 0) continue;

        // Check Surrogate-Control or Edge-Control
        if (h[i].key.len == sizeof("Surrogate-Control") - 1 &&
            ngx_strncasecmp(h[i].key.data, (u_char *)"Surrogate-Control",
                           sizeof("Surrogate-Control") - 1) == 0) {
            if (ngx_strstr(h[i].value.data, "ESI/1.0") ||
                ngx_strstr(h[i].value.data, "ESI")) {
                return 1;
            }
        }
    }
    return 0;
}
```

### 6. Pass to libgomesi

```c
// In ParseJson or ParseWithConfigExtended
"parseOnHeader": true/false
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive: `mesi_parse_on_header on`, `mesi_parse_on_header off`
- [ ] **Tests** — Unit test `has_esi_header` with:
  - `Surrogate-Control: content="ESI/1.0"` → 1
  - `Edge-Control: content=ESI/1.0` → 1
  - No ESI header → 0
  - `Surrogate-Control: max-age=60` (no ESI marker) → 0
- [ ] **Tests** — Unit test default: unset → off (ESI always processed)
- [ ] **Docs** — Add `mesi_parse_on_header` to README with description and interaction with backend headers
- [ ] **Functional tests** — nginx integration test:
  - `parse_on_header on` + backend returns no ESI header → ESI NOT processed (raw tags in output)
  - `parse_on_header on` + backend returns `Surrogate-Control: content="ESI/1.0"` → ESI processed
  - `parse_on_header off` + backend returns no ESI header → ESI still processed (backward compatible)
  - `parse_on_header on` + `Surrogate-Control` with `ESI` substring → processed (case-insensitive check)
- [ ] **Changelog** — Entry in project changelog

## Notes

- nginx has `ngx_http_parse_header_line` but no direct accessor for response surrogate-control. Must iterate `r->headers_out.headers` list.
- nginx 1.23+ added `r->headers_out.surrogate_control` as a first-class field. Check nginx version compatibility. Use list iteration for broader compatibility (nginx 1.18+ is the module's baseline).
- `Case-insensitive` header matching: `ngx_strncasecmp` for key, `ngx_strstr` for value (which is case-sensitive — spec says `ESI/1.0` is the canonical value).
- For `ParseJson`, the `parseOnHeader` field maps to `EsiParserConfig.ParseOnHeader` which controls how the Go parser interprets the header. The C-side check (in head filter) determines whether to enter the accumulation path at all. These are redundant but complementary: C-side check avoids unnecessary body buffering.
- Consider what happens when `parse_on_header on` but the backend returns the ESI header on a non-HTML resource → the head filter's Content-Type check catches this and skips ESI anyway.
