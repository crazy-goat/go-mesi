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
typedef void (*FreeFunc)(char *);

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
    apr_port_t port = ap_get_server_port(r);

    if (!host || !*host) {
        host = "localhost";
    }

    int default_port = (strcmp(scheme, "https") == 0) ? 443 : 80;

    if (port != default_port) {
        return apr_psprintf(pool, "%s://%s:%d/", scheme, host, port);
    }
    return apr_psprintf(pool, "%s://%s/", scheme, host);
}

static int mesi_response_filter(ap_filter_t *f, apr_bucket_brigade *bb) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(f->r->server->module_config, &mesi_module);
    if (!conf->enable_mesi) {
        return ap_pass_brigade(f->next, bb);
    }

    if (!f->r->content_type || strncmp(f->r->content_type, "text/html", 9) != 0 || f->r->status > 400) {
        return ap_pass_brigade(f->next, bb);
    }

    apr_bucket *b;
    char *html = NULL;
    response_filter_ctx *ctx;

    ctx = f->ctx;
    if (!ctx) {
        ctx = apr_pcalloc(f->r->pool, sizeof(response_filter_ctx));
        f->ctx = ctx;
    }

    for (b = APR_BRIGADE_FIRST(bb); b != APR_BRIGADE_SENTINEL(bb); b = APR_BUCKET_NEXT(b)) {
        const char *data;
        apr_size_t data_len;

        if (apr_bucket_read(b, &data, &data_len, APR_BLOCK_READ) == APR_SUCCESS) {
            if (!html) {
                html = apr_pstrndup(f->r->pool, data, data_len);
            } else {
                html = apr_pstrcat(f->r->pool, html, data, NULL);
            }
        }
    }

    if (!html) {
        return ap_pass_brigade(f->next, bb);
    }

    void *go_module = dlopen(LIB_GOMESI_PATH, RTLD_NOW);
    if (!go_module) {
        ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, f->r, "mesi: Failed to load %s: %s", LIB_GOMESI_PATH, dlerror());
        return ap_pass_brigade(f->next, bb);
    }

    ParseFunc EsiParse = (ParseFunc)dlsym(go_module, "Parse");
    FreeFunc FreeString = (FreeFunc)dlsym(go_module, "FreeString");
    
    if (!EsiParse) {
        ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, f->r, "mesi: Failed to find Parse symbol: %s", dlerror());
        dlclose(go_module);
        return ap_pass_brigade(f->next, bb);
    }

    char *base_url = build_base_url(f->r, f->r->pool);
    char *esi = EsiParse(html, 5, base_url);
    
    dlclose(go_module);

    if (!esi) {
        esi = html;
    }

    apr_brigade_cleanup(bb);
    b = apr_bucket_pool_create(esi, strlen(esi), f->r->pool, bb->bucket_alloc);
    APR_BRIGADE_INSERT_TAIL(bb, b);
    APR_BRIGADE_INSERT_TAIL(bb, apr_bucket_eos_create(bb->bucket_alloc));

    return ap_pass_brigade(f->next, bb);
}

static void register_hooks(apr_pool_t *p) {
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
