#include "httpd.h"
#include "http_config.h"
#include "http_protocol.h"
#include "http_request.h"
#include "http_core.h"
#include "http_log.h"
#include "util_filter.h"
#include "apr_strings.h"

#include <dlfcn.h>
#include <string.h>

#ifndef LIB_GOMESI_PATH
#define LIB_GOMESI_PATH "/usr/lib/libgomesi.so"
#endif

typedef char *(*ParseFunc)(char *, int, char *);
typedef char *(*ParseWithConfigFunc)(char *, int, char *, char *, int);
typedef void (*FreeFunc)(char *);

static void *go_module = NULL;
static ParseFunc EsiParse = NULL;
static ParseWithConfigFunc EsiParseWithConfig = NULL;
static FreeFunc EsiFreeString = NULL;

typedef struct {
    apr_bucket_brigade *bb;
} response_filter_ctx;

module AP_MODULE_DECLARE_DATA mesi_module;

typedef struct {
    int enable_mesi;
    apr_array_header_t *allowed_hosts;
    int block_private_ips;  // -1=unset, 0=off, 1=on
} mesi_config;

static void *create_server_config(apr_pool_t *p, server_rec *s) {
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    conf->enable_mesi = 0;
    conf->allowed_hosts = apr_array_make(p, 4, sizeof(const char *));
    conf->block_private_ips = -1;  // -1 = unset, default will be applied in filter
    return conf;
}

static void *merge_server_config(apr_pool_t *p, void *basev, void *addv) {
    mesi_config *base = (mesi_config *) basev;
    mesi_config *add = (mesi_config *) addv;
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    conf->enable_mesi = (add->enable_mesi != 0) ? add->enable_mesi : base->enable_mesi;
    conf->allowed_hosts = (add->allowed_hosts->nelts > 0) ? add->allowed_hosts : base->allowed_hosts;
    conf->block_private_ips = (add->block_private_ips != -1) ? add->block_private_ips : base->block_private_ips;
    return conf;
}

static apr_status_t mesi_child_cleanup(void *data) {
    if (go_module) {
        dlclose(go_module);
        go_module = NULL;
    }
    EsiParse = NULL;
    EsiParseWithConfig = NULL;
    EsiFreeString = NULL;
    return APR_SUCCESS;
}

static void mesi_child_init(apr_pool_t *p, server_rec *s) {
    // RTLD_GLOBAL is required for Go's runtime (signal handlers, etc.)
    // Without it, Go's runtime initialization may fail or behave incorrectly
    go_module = dlopen(LIB_GOMESI_PATH, RTLD_NOW | RTLD_GLOBAL);
    if (!go_module) {
        char *err = dlerror();
        ap_log_error(APLOG_MARK, APLOG_ERR, 0, s,
                     "mesi: dlopen(%s) failed: %s", LIB_GOMESI_PATH, err ? err : "(unknown error)");
        return;
    }

    EsiParse = (ParseFunc)dlsym(go_module, "Parse");
    EsiParseWithConfig = (ParseWithConfigFunc)dlsym(go_module, "ParseWithConfig");
    EsiFreeString = (FreeFunc)dlsym(go_module, "FreeString");

    // Require at least one parse function and FreeString to avoid memory leaks
    if ((!EsiParse && !EsiParseWithConfig) || !EsiFreeString) {
        char *err = dlerror();
        ap_log_error(APLOG_MARK, APLOG_ERR, 0, s,
                     "mesi: dlsym failed: %s", err ? err : "(unknown error)");
        dlclose(go_module);
        go_module = NULL;
        EsiParse = NULL;
        EsiParseWithConfig = NULL;
        EsiFreeString = NULL;
        return;
    }

    apr_pool_cleanup_register(p, NULL, mesi_child_cleanup, apr_pool_cleanup_null);
}

static void *create_dir_config(apr_pool_t *p, char *dir) {
    return NULL;
}

static void *merge_dir_config(apr_pool_t *p, void *basev, void *addv) {
    return NULL;
}

static const char *set_enable_mesi(cmd_parms *cmd, void *cfg, int flag) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    conf->enable_mesi = flag;
    return NULL;
}

static const char *set_allowed_hosts(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    const char *host;
    while (*arg) {
        // Skip whitespace (space, tab)
        while (*arg && (*arg == ' ' || *arg == '\t')) arg++;
        host = arg;
        // Find end of token (space or tab)
        while (*arg && *arg != ' ' && *arg != '\t') arg++;
        if (host != arg) {
            const char **new_host = apr_array_push(conf->allowed_hosts);
            *new_host = apr_pstrndup(cmd->pool, host, arg - host);
        }
    }
    return NULL;
}

static const char *set_block_private_ips(cmd_parms *cmd, void *cfg, int flag) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    conf->block_private_ips = flag;
    return NULL;
}

static int mesi_request_handler(request_rec *r) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(r->server->module_config, &mesi_module);
    if (conf->enable_mesi) {
        apr_table_set(r->headers_out, "Surrogate-Capability", "ESI/1.0");
        ap_add_output_filter("MESI_RESPONSE", NULL, r, r->connection);
    }
    return DECLINED;
}

static char *build_base_url(request_rec *r, apr_pool_t *pool) {
    const char *scheme = ap_http_scheme(r);
    const char *host = r->server->server_hostname
                        ? r->server->server_hostname
                        : ap_get_server_name(r);
    // Use canonical port from server config, not client-supplied
    apr_port_t port = r->server->port ? r->server->port : ap_get_server_port(r);
    
    if (!host || !*host) {
        host = "localhost";
    }
    
    int default_port = (strcmp(scheme, "https") == 0) ? 443 : 80;
    
    if (port != default_port) {
        return apr_psprintf(pool, "%s://%s:%d/", scheme, host, port);
    }
    return apr_psprintf(pool, "%s://%s/", scheme, host);
}

static int is_html_content(const char *ct) {
    if (!ct) return 0;
    // Skip leading whitespace (OWS per RFC 7230 §3.2.6)
    while (*ct == ' ' || *ct == '\t') ct++;
    // Case-insensitive media-type comparison (RFC 9110 §8.3.1)
    if (strncasecmp(ct, "text/html", 9) != 0) return 0;
    char delim = ct[9];
    // Must be followed by delimiter, parameter separator, or end-of-string
    return delim == '\0' || delim == ';' || delim == ' ' || delim == '\t'
           || delim == '\r' || delim == '\n';
}

static int mesi_response_filter(ap_filter_t *f, apr_bucket_brigade *bb) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(f->r->server->module_config, &mesi_module);
    if (!conf->enable_mesi) {
        return ap_pass_brigade(f->next, bb);
    }

    if (!is_html_content(f->r->content_type) || f->r->status >= 400) {
        ap_remove_output_filter(f);
        return ap_pass_brigade(f->next, bb);
    }

    response_filter_ctx *ctx = f->ctx;
    if (!ctx) {
        ctx = apr_pcalloc(f->r->pool, sizeof(*ctx));
        ctx->bb = apr_brigade_create(f->r->pool, f->c->bucket_alloc);
        f->ctx = ctx;
    }

    // Move all buckets from the incoming brigade to our accumulation brigade.
    // Track whether we've seen the end-of-stream (EOS) marker.
    int seen_eos = 0;
    apr_bucket *b;
    while ((b = APR_BRIGADE_FIRST(bb)) != APR_BRIGADE_SENTINEL(bb)) {
        if (APR_BUCKET_IS_EOS(b)) {
            seen_eos = 1;
            apr_bucket_delete(b);
            continue;
        }
        APR_BUCKET_REMOVE(b);
        APR_BRIGADE_INSERT_TAIL(ctx->bb, b);
    }

    if (!seen_eos) {
        return APR_SUCCESS;  // Not the last brigade — wait for more data
    }

    // Flatten the accumulated body into a single NUL-terminated string.
    // If flattening fails, pass through raw data without ESI processing.
    apr_size_t len = 0;
    char *html = NULL;
    int flatten_ok = 0;

    if (apr_brigade_length(ctx->bb, 1, &len) == APR_SUCCESS && len > 0) {
        html = apr_palloc(f->r->pool, len + 1);
        apr_size_t copied = len;
        if (apr_brigade_flatten(ctx->bb, html, &copied) == APR_SUCCESS) {
            html[copied] = '\0';
            flatten_ok = 1;
        }
    }

    if (!flatten_ok) {
        // Empty body or flatten failed — pass through without parsing
        APR_BRIGADE_INSERT_TAIL(ctx->bb, apr_bucket_eos_create(ctx->bb->bucket_alloc));
        if (html && len > 0) {
            ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                "mesi: failed to flatten response body, skipping ESI processing");
        }
        return ap_pass_brigade(f->next, ctx->bb);
    }

    // Build allowed_hosts string from config
    char *allowed_hosts_str = "";
    if (conf->allowed_hosts && conf->allowed_hosts->nelts > 0) {
        apr_array_header_t *arr = conf->allowed_hosts;
        const char **hosts = (const char **)arr->elts;
        allowed_hosts_str = apr_pstrdup(f->r->pool, hosts[0]);
        for (int i = 1; i < arr->nelts; i++) {
            allowed_hosts_str = apr_pstrcat(f->r->pool, allowed_hosts_str, " ", hosts[i], NULL);
        }
    }

    int block_private = (conf->block_private_ips != -1) ? conf->block_private_ips : 1;

    if (!EsiParse && !EsiParseWithConfig) {
        ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, f->r, "mesi: libgomesi not loaded");
        apr_brigade_cleanup(ctx->bb);
        b = apr_bucket_pool_create(html, strlen(html), f->r->pool, ctx->bb->bucket_alloc);
        APR_BRIGADE_INSERT_TAIL(ctx->bb, b);
        APR_BRIGADE_INSERT_TAIL(ctx->bb, apr_bucket_eos_create(ctx->bb->bucket_alloc));
        return ap_pass_brigade(f->next, ctx->bb);
    }

    char *base_url = build_base_url(f->r, f->r->pool);
    char *esi = NULL;

    if (EsiParseWithConfig) {
        esi = EsiParseWithConfig(html, 5, base_url, allowed_hosts_str, block_private);
    } else {
        // ParseWithConfig not available - check if security features are configured
        int has_security_config = (conf->allowed_hosts && conf->allowed_hosts->nelts > 0)
                               || (conf->block_private_ips != -1 && conf->block_private_ips == 1);

        if (has_security_config) {
            ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, f->r,
                "mesi: ParseWithConfig not found but security directives are configured. "
                "SSRF protection disabled! Upgrade libgomesi.so or remove MesiAllowedHosts/MesiBlockPrivateIPs directives.");
            apr_brigade_cleanup(ctx->bb);
            b = apr_bucket_pool_create(html, strlen(html), f->r->pool, ctx->bb->bucket_alloc);
            APR_BRIGADE_INSERT_TAIL(ctx->bb, b);
            APR_BRIGADE_INSERT_TAIL(ctx->bb, apr_bucket_eos_create(ctx->bb->bucket_alloc));
            return ap_pass_brigade(f->next, ctx->bb);
        }

        ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
            "mesi: ParseWithConfig not found, falling back to Parse (no SSRF protection)");

        if (EsiParse) {
            esi = EsiParse(html, 5, base_url);
        }
    }

    apr_brigade_cleanup(ctx->bb);

    char *output;
    if (esi) {
        output = apr_pstrdup(f->r->pool, esi);
        if (EsiFreeString) {
            EsiFreeString(esi);
        }
    } else {
        output = html;
    }

    b = apr_bucket_pool_create(output, strlen(output), f->r->pool, ctx->bb->bucket_alloc);
    APR_BRIGADE_INSERT_TAIL(ctx->bb, b);
    APR_BRIGADE_INSERT_TAIL(ctx->bb, apr_bucket_eos_create(ctx->bb->bucket_alloc));

    apr_table_unset(f->r->headers_out, "Content-Length");
    return ap_pass_brigade(f->next, ctx->bb);
}

static void register_hooks(apr_pool_t *p) {
    ap_hook_child_init(mesi_child_init, NULL, NULL, APR_HOOK_MIDDLE);
    ap_hook_post_read_request(mesi_request_handler, NULL, NULL, APR_HOOK_MIDDLE);
    ap_register_output_filter("MESI_RESPONSE", mesi_response_filter, NULL, AP_FTYPE_CONTENT_SET);
}

static const command_rec mesi_directives[] = {
    AP_INIT_FLAG("EnableMesi", set_enable_mesi, NULL, RSRC_CONF, "Enable or disable the Mesi module"),
    AP_INIT_RAW_ARGS("MesiAllowedHosts", set_allowed_hosts, NULL, RSRC_CONF, "Space-separated list of allowed hostnames for ESI includes"),
    AP_INIT_FLAG("MesiBlockPrivateIPs", set_block_private_ips, NULL, RSRC_CONF, "Enable or disable private IP blocking (default: On)"),
    {NULL}
};

module AP_MODULE_DECLARE_DATA mesi_module = {
    STANDARD20_MODULE_STUFF,
    create_dir_config,
    merge_dir_config,
    create_server_config,
    merge_server_config,
    mesi_directives,
    register_hooks
};
