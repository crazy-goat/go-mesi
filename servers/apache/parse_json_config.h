/*
 * ParseJson config blob rendering for mod_mesi.
 *
 * Lives in its own header (and not in mod_mesi.c) so the C unit tests in
 * test_directives.c exercise the real blob assembly instead of a copy: the
 * builder is pure APR string handling and needs no Apache request/server
 * types. mod_mesi.c fills mesi_parse_json_config from its mesi_config and
 * calls mesi_build_parse_json_config().
 *
 * The blob contract (libgomesi ParseJson entry point):
 *   - maxDepth / defaultUrl / allowedHosts / blockPrivateIPs /
 *     allowPrivateIPsForAllowedHosts are always present.
 *   - timeoutSeconds (seconds), maxResponseSize (bytes),
 *     maxConcurrentRequests, maxWorkers (counts), debug (true/false),
 *     parseOnHeader (true/false), includeErrorMarker, cacheKeyTemplate and
 *     requestCtx are rendered only when their directive is configured
 *     (sentinel -1 for the integer fields, NULL/empty for the strings), so
 *     an unset directive keeps the documented positional-path default
 *     Go-side (#167, #169, #170, #171, #173, #172).
 *   - requestCtx is only meaningful with a cache key template (it populates
 *     the template placeholders) and is passed through verbatim as
 *     pre-rendered JSON.
 *
 * The optional parts are concatenated with apr_pstrcat, not rendered through
 * one apr_psprintf with a fixed placeholder count: a %s/argument mismatch
 * silently drops the trailing argument (that regressed #165 — the dropped
 * part was requestCtx, so cache key templates fell back to URL-only keys and
 * leaked one language's cached body into another). pstrcat's NULL-terminated
 * argument list makes that class of bug impossible.
 */
#ifndef MOD_MESI_PARSE_JSON_CONFIG_H
#define MOD_MESI_PARSE_JSON_CONFIG_H

#include <string.h>

#include <apr_pools.h>
#include <apr_strings.h>

typedef struct {
    /* Always rendered. */
    int depth;
    const char *base_url;
    const char *allowed_hosts;
    int block_private_ips;
    int allow_private_for_allowed;
    /* Only rendered when cache_key_template is configured. Pre-rendered
     * JSON, NULL or "" when build_request_ctx_json produced nothing. */
    const char *request_ctx_json;
    /* Optional directives; -1 / NULL / "" mean "unset, keep the default". */
    int timeout_seconds;
    apr_off_t max_response_size;
    int max_concurrent_requests;
    int max_workers;
    int debug;
    int parse_on_header;
    const char *include_error_marker;
    const char *cache_key_template;
} mesi_parse_json_config;

/* mesi_json_string renders s as a quoted, JSON-escaped string for the
 * ParseJson config blob: '"' / '\\' escaped, bytes < 0x20 as \u00XX.
 * Sized for the worst case (6 output bytes per input byte + quotes + NUL)
 * because APR pools have no realloc. */
static const char *mesi_json_string(apr_pool_t *pool, const char *s) {
    if (!s) {
        s = "";
    }
    apr_size_t len = strlen(s);
    char *buf = apr_palloc(pool, len * 6 + 3);
    char *w = buf;
    *w++ = '"';
    for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
        if (*p == '"') {
            *w++ = '\\';
            *w++ = '"';
        } else if (*p == '\\') {
            *w++ = '\\';
            *w++ = '\\';
        } else if (*p < 0x20) {
            static const char hex[] = "0123456789abcdef";
            *w++ = '\\';
            *w++ = 'u';
            *w++ = '0';
            *w++ = '0';
            *w++ = hex[(*p >> 4) & 0xf];
            *w++ = hex[*p & 0xf];
        } else {
            *w++ = (char)*p;
        }
    }
    *w++ = '"';
    *w = '\0';
    return buf;
}

/* mesi_build_parse_json_config renders cfg into the JSON blob accepted by
 * libgomesi's ParseJson entry point. */
static const char *mesi_build_parse_json_config(apr_pool_t *pool,
                                                const mesi_parse_json_config *cfg) {
    const char *timeout_part = "";
    const char *mrs_part = "";
    const char *maxcr_part = "";
    const char *maxw_part = "";
    const char *debug_part = "";
    const char *poh_part = "";
    const char *marker_part = "";
    const char *tmpl_part = "";
    const char *ctx_part = "";
    if (cfg->timeout_seconds != -1) {
        timeout_part = apr_psprintf(pool, ",\"timeoutSeconds\":%d",
                                    cfg->timeout_seconds);
    }
    if (cfg->max_response_size != -1) {
        mrs_part = apr_psprintf(pool, ",\"maxResponseSize\":%" APR_INT64_T_FMT,
                                (apr_int64_t)cfg->max_response_size);
    }
    if (cfg->max_concurrent_requests != -1) {
        maxcr_part = apr_psprintf(pool, ",\"maxConcurrentRequests\":%d",
                                  cfg->max_concurrent_requests);
    }
    if (cfg->max_workers != -1) {
        maxw_part = apr_psprintf(pool, ",\"maxWorkers\":%d",
                                 cfg->max_workers);
    }
    if (cfg->debug != -1) {
        debug_part = apr_psprintf(pool, ",\"debug\":%s",
                                  cfg->debug ? "true" : "false");
    }
    if (cfg->parse_on_header != -1) {
        poh_part = apr_psprintf(pool, ",\"parseOnHeader\":%s",
                                cfg->parse_on_header ? "true" : "false");
    }
    if (cfg->include_error_marker && cfg->include_error_marker[0] != '\0') {
        marker_part = apr_psprintf(pool, ",\"includeErrorMarker\":%s",
                                   mesi_json_string(pool, cfg->include_error_marker));
    }
    if (cfg->cache_key_template && cfg->cache_key_template[0] != '\0') {
        tmpl_part = apr_psprintf(pool, ",\"cacheKeyTemplate\":%s",
                                 mesi_json_string(pool, cfg->cache_key_template));
        if (cfg->request_ctx_json && cfg->request_ctx_json[0] != '\0') {
            ctx_part = apr_psprintf(pool, ",\"requestCtx\":%s", cfg->request_ctx_json);
        }
    }
    return apr_pstrcat(pool,
                       "{\"maxDepth\":", apr_psprintf(pool, "%d", cfg->depth),
                       ",\"defaultUrl\":", mesi_json_string(pool, cfg->base_url),
                       ",\"allowedHosts\":", mesi_json_string(pool, cfg->allowed_hosts),
                       ",\"blockPrivateIPs\":", cfg->block_private_ips ? "true" : "false",
                       ",\"allowPrivateIPsForAllowedHosts\":", cfg->allow_private_for_allowed ? "true" : "false",
                       timeout_part,
                       mrs_part,
                       maxcr_part,
                       maxw_part,
                       debug_part,
                       poh_part,
                       marker_part,
                       tmpl_part,
                       ctx_part,
                       "}",
                       NULL);
}

#endif /* MOD_MESI_PARSE_JSON_CONFIG_H */
