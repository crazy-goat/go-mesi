#include "httpd.h"
#include "http_config.h"
#include "http_protocol.h"
#include "http_request.h"
#include "apr_strings.h"

#include "libgomesi.h"

module AP_MODULE_DECLARE_DATA mod_mesi_module;

typedef struct {
    int enable_mesi;
} mesi_config;

static void *create_server_config(apr_pool_t *p, server_rec *s) {
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    conf->enable_mesi = 0;
    return conf;
}

static void *merge_server_config(apr_pool_t *p, void *basev, void *addv) {
    mesi_config *base = (mesi_config *) basev;
    mesi_config *add = (mesi_config *) addv;
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    conf->enable_mesi = (add->enable_mesi != 0) ? add->enable_mesi : base->enable_mesi;
    return conf;
}

static void *create_dir_config(apr_pool_t *p, char *dir) {
    return NULL;
}

static void *merge_dir_config(apr_pool_t *p, void *basev, void *addv) {
    return NULL;
}

static const char *set_enable_mesi(cmd_parms *cmd, void *cfg, int flag) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mod_mesi_module);
    conf->enable_mesi = flag;
    return NULL;
}

static int mesi_request_handler(request_rec *r) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(r->server->module_config, &mod_mesi_module);
    if (conf->enable_mesi) {
        apr_table_set(r->headers_in, "Surrogate-Capability", "ESI/1.0");
    }
    return DECLINED;
}

static int mesi_response_filter(ap_filter_t *f, apr_bucket_brigade *bb) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(f->r->server->module_config, &mod_mesi_module);
    if (!conf->enable_mesi) {
        return ap_pass_brigade(f->next, bb);
    }

    if (!f->r->content_type || strncmp(f->r->content_type, "text/html", 9) != 0 || f->r->status > 400) {
        return ap_pass_brigade(f->next, bb);
    }

    apr_bucket *b;
    for (b = APR_BRIGADE_FIRST(bb); b != APR_BRIGADE_SENTINEL(bb); b = APR_BUCKET_NEXT(b)) {
        if (APR_BUCKET_IS_EOS(b)) {
            continue;
        }

        const char *data;
        apr_size_t len;
        if (apr_bucket_read(b, &data, &len, APR_BLOCK_READ) == APR_SUCCESS) {
            char *html = strdup(data);
            char *esi = Parse(html, 5, "http://127.0.0.1/");
            free(html);

            apr_bucket *new_bucket = apr_bucket_pool_create(esi, strlen(esi), f->r->pool, f->c->bucket_alloc);
            APR_BUCKET_INSERT_BEFORE(b, new_bucket);
            apr_bucket_delete(b);
            free(esi);
        }
    }
    return ap_pass_brigade(f->next, bb);
}

static void register_hooks(apr_pool_t *p) {
    ap_hook_post_read_request(mesi_request_handler, NULL, NULL, APR_HOOK_MIDDLE);
    ap_register_output_filter("MESI_RESPONSE", mesi_response_filter, NULL, AP_FTYPE_RESOURCE);
}

static const command_rec mesi_directives[] = {
    AP_INIT_FLAG("EnableMesi", set_enable_mesi, NULL, RSRC_CONF, "Enable or disable the Mesi module"),
    {NULL}
};

module AP_MODULE_DECLARE_DATA mod_mesi_module = {
    STANDARD20_MODULE_STUFF,
    create_dir_config,
    merge_dir_config,
    create_server_config,
    merge_server_config,
    mesi_directives,
    register_hooks
};