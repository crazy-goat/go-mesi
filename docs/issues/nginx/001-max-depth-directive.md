# [nginx] Add `max_depth` directive

## Problem

`servers/nginx/ngx_http_mesi_module.c:236` hardcodes max depth to 5:

```c
char *message = EsiParse(ngx_str_to_cstr(&input, r->pool), 5,
                         ngx_str_to_cstr(&base_url, r->pool));
```

The operator cannot change the ESI nesting limit. A page with 6 levels of nested `<esi:include>` (e.g., header > nav > menu > item > badge > icon) silently drops the innermost includes with no error or log message.

## Impact

- Operators cannot adapt depth to their application's ESI nesting needs.
- Deeper nesting is silently truncated — no warning, no error marker.
- Operators with simpler needs (depth=1 or 2) pay full parse cost for deeper nesting they don't use.

## Context

`mesi.EsiParserConfig.MaxDepth` accepts `uint`. Default is 5 (matching the hardcoded value). The nginx module must pass this through libgomesi.

Current nginx config struct (`ngx_http_mesi_loc_conf_t`) has only `enable_mesi`. This issue adds the first additional field — establishing the pattern for all subsequent directives.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    ngx_flag_t enable_mesi;
    ngx_int_t  max_depth;
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_max_depth"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, max_depth), NULL},
```

### 3. Default and merge

```c
static void *ngx_http_mesi_create_loc_conf(ngx_conf_t *cf) {
    ngx_http_mesi_loc_conf_t *conf;
    conf = ngx_pcalloc(cf->pool, sizeof(ngx_http_mesi_loc_conf_t));
    if (conf == NULL) return NULL;
    conf->enable_mesi = NGX_CONF_UNSET;
    conf->max_depth = NGX_CONF_UNSET;
    return conf;
}

static char *ngx_http_mesi_merge_loc_conf(ngx_conf_t *cf, void *parent, void *child) {
    // ...
    ngx_conf_merge_value(conf->max_depth, prev->max_depth, 5);
    return NGX_CONF_OK;
}
```

### 4. Use in parse()

```c
static ngx_str_t parse(ngx_str_t input, ngx_http_request_t *r) {
    ngx_http_mesi_loc_conf_t *lcf =
        ngx_http_get_module_loc_conf(r, ngx_http_mesi_module);

    int max_depth = (int)lcf->max_depth;

    char *message = EsiParse(ngx_str_to_cstr(&input, r->pool), max_depth,
                             ngx_str_to_cstr(&base_url, r->pool));
    // ...
}
```

**Note**: `EsiParse` is the legacy 3-arg function. The libgomesi migration (to `ParseWithConfig` or `ParseJson`) is a prerequisite tracked separately. Until that migration, only `maxDepth` can be threaded through the existing 3-arg function.

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_max_depth 3;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive parsing: `mesi_max_depth 0` (valid), `mesi_max_depth -1` (invalid, reject), `mesi_max_depth abc` (invalid)
- [ ] **Tests** — Unit test merge: parent sets 10, child unsets → child gets parent's 10
- [ ] **Tests** — Unit test merge: parent unsets, child sets 3 → 3
- [ ] **Docs** — Add `mesi_max_depth` to `servers/nginx/README.md` directive reference with type, default, description
- [ ] **Docs** — Document behavior: depth 0 means no ESI processing (passthrough)
- [ ] **Functional tests** — nginx integration test:
  - `mesi_max_depth 1` → page with 2-level nested include: inner include NOT processed (verify in output)
  - `mesi_max_depth 5` (default) → same page: both levels processed
  - `mesi_max_depth 0` → page with includes: ESI not processed, raw `<esi:include>` tags in output
- [ ] **Changelog** — Entry in project changelog
- [ ] **Backward compatibility** — Omitting `mesi_max_depth` must default to 5 (identical to current behavior)

## Notes

- `ngx_conf_set_num_slot` stores the directive value into the struct field at the given offset. Works for `ngx_int_t` fields.
- `NGX_CONF_UNSET` is nginx's sentinel for "not configured". Must be checked in merge; `ngx_conf_merge_value` handles this.
- For `max_depth: 0`, the `mesi.EsiParserConfig.CanGoDeeper()` returns false, effectively skipping all `<esi:include>` processing. Document this as a "disable ESI for nested includes" mode.
- This is the simplest directive addition — establishes the config struct extension pattern for all subsequent nginx issues.
