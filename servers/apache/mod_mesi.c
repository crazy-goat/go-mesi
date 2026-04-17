#include "httpd.h"
#include "http_config.h"
#include "http_protocol.h"
#include "http_log.h"
#include "ap_config.h"
#include "apr_strings.h"
#include "apr_lib.h"
#include "apr_buckets.h"
#include "util_filter.h"

#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>

#ifndef LIB_GOMESI_PATH
#define LIB_GOMESI_PATH "/usr/lib/libgomesi.so"
#endif

typedef struct mesi_config {
    int enabled;
    const char *lib_path;
} mesi_config;

typedef char *(*ParseFunc)(char *, int, char *);

static void *go_module = NULL;
static ParseFunc EsiParse = NULL;
static apr_thread_mutex_t *parse_mutex = NULL;

module AP_MODULE_DECLARE_DATA mesi_module;

static void *mesi_create_dir_config(apr_pool_t *p, char *dir)
{
    mesi_config *cfg = apr_pcalloc(p, sizeof(mesi_config));
    cfg->enabled = 0;
    cfg->lib_path = LIB_GOMESI_PATH;
    return cfg;
}

static void *mesi_merge_dir_config(apr_pool_t *p, void *parent, void *child)
{
    mesi_config *prev = parent;
    mesi_config *conf = child;
    mesi_config *merged = apr_pcalloc(p, sizeof(mesi_config));
    
    merged->enabled = (conf->enabled != 0) ? conf->enabled : prev->enabled;
    merged->lib_path = (conf->lib_path != NULL) ? conf->lib_path : prev->lib_path;
    
    return merged;
}

static const char *set_mesi_enable(cmd_parms *cmd, void *mconfig, int arg)
{
    mesi_config *cfg = (mesi_config *)mconfig;
    cfg->enabled = arg;
    return NULL;
}

static const char *set_mesi_lib_path(cmd_parms *cmd, void *mconfig, const char *arg)
{
    mesi_config *cfg = (mesi_config *)mconfig;
    cfg->lib_path = arg;
    return NULL;
}

static const command_rec mesi_cmds[] = {
    AP_INIT_FLAG("MesiEnable", set_mesi_enable, NULL, ACCESS_CONF,
                 "Enable or disable ESI processing"),
    AP_INIT_TAKE1("MesiLibPath", set_mesi_lib_path, NULL, ACCESS_CONF,
                  "Path to libgomesi.so"),
    {NULL}
};

static int is_html_content(request_rec *r)
{
    const char *ct = apr_table_get(r->headers_out, "Content-Type");
    if (ct == NULL) {
        return 0;
    }
    return (strcasestr(ct, "text/html") != NULL);
}

static int is_compressed(request_rec *r)
{
    const char *ce = apr_table_get(r->headers_out, "Content-Encoding");
    if (ce == NULL || strlen(ce) == 0) {
        return 0;
    }
    return 1;
}

static char *build_base_url(request_rec *r, apr_pool_t *pool)
{
    const char *scheme = ap_http_scheme(r);
    const char *host = ap_get_server_name(r);
    apr_port_t port = ap_get_server_port(r);
    
    int default_port = (strcmp(scheme, "https") == 0) ? 443 : 80;
    
    if (port != default_port) {
        return apr_psprintf(pool, "%s://%s:%d/", scheme, host, port);
    }
    return apr_psprintf(pool, "%s://%s/", scheme, host);
}

typedef struct mesi_filter_ctx {
    apr_bucket_brigade *bb;
    int done;
} mesi_filter_ctx;

static apr_status_t mesi_output_filter(ap_filter_t *f, apr_bucket_brigade *bb)
{
    mesi_config *cfg = ap_get_module_config(f->r->per_dir_config, &mesi_module);
    
    if (!cfg->enabled || go_module == NULL || EsiParse == NULL) {
        return ap_pass_brigade(f->next, bb);
    }
    
    if (f->r->header_only || f->r->status >= HTTP_BAD_REQUEST) {
        return ap_pass_brigade(f->next, bb);
    }
    
    if (!is_html_content(f->r) || is_compressed(f->r)) {
        return ap_pass_brigade(f->next, bb);
    }
    
    mesi_filter_ctx *ctx = f->ctx;
    if (ctx == NULL) {
        ctx = apr_pcalloc(f->r->pool, sizeof(mesi_filter_ctx));
        ctx->bb = apr_brigade_create(f->r->pool, f->c->bucket_alloc);
        ctx->done = 0;
        f->ctx = ctx;
        
        apr_table_set(f->r->headers_out, "Surrogate-Capability", "ESI/1.0");
        apr_table_unset(f->r->headers_out, "Content-Length");
    }
    
    if (ctx->done) {
        return ap_pass_brigade(f->next, bb);
    }
    
    APR_BRIGADE_CONCAT(ctx->bb, bb);
    
    apr_bucket *e = NULL;
    for (e = APR_BRIGADE_LAST(ctx->bb); e != APR_BRIGADE_SENTINEL(ctx->bb); e = APR_BUCKET_PREV(e)) {
        if (APR_BUCKET_IS_EOS(e)) {
            ctx->done = 1;
            apr_bucket_delete(e);
            break;
        }
    }
    
    if (!ctx->done) {
        return APR_SUCCESS;
    }
    
    apr_off_t length = 0;
    apr_brigade_length(ctx->bb, 1, &length);
    
    char *input = apr_palloc(f->r->pool, length + 1);
    char *ptr = input;
    
    for (e = APR_BRIGADE_FIRST(ctx->bb); e != APR_BRIGADE_SENTINEL(ctx->bb); e = APR_BUCKET_NEXT(e)) {
        const char *data;
        apr_size_t len;
        apr_bucket_read(e, &data, &len, APR_BLOCK_READ);
        memcpy(ptr, data, len);
        ptr += len;
    }
    *ptr = '\0';
    
    char *base_url = build_base_url(f->r, f->r->pool);
    
    char *output = NULL;
    
#ifdef APR_HAS_THREADS
    if (parse_mutex) {
        apr_thread_mutex_lock(parse_mutex);
    }
#endif
    
    output = EsiParse(input, 5, base_url);
    
#ifdef APR_HAS_THREADS
    if (parse_mutex) {
        apr_thread_mutex_unlock(parse_mutex);
    }
#endif
    
    if (output == NULL) {
        output = input;
    }
    
    apr_size_t output_len = strlen(output);
    
    apr_brigade_cleanup(ctx->bb);
    
    e = apr_bucket_pool_create(output, output_len, f->r->pool, f->c->bucket_alloc);
    APR_BRIGADE_INSERT_TAIL(ctx->bb, e);
    
    e = apr_bucket_eos_create(f->c->bucket_alloc);
    APR_BRIGADE_INSERT_TAIL(ctx->bb, e);
    
    f->r->headers_out.content_length = output_len;
    apr_table_set(f->r->headers_out, "Content-Length", apr_psprintf(f->r->pool, "%" APR_SIZE_T_FMT, output_len));
    
    return ap_pass_brigade(f->next, ctx->bb);
}

static int mesi_post_config(apr_pool_t *pconf, apr_pool_t *plog, apr_pool_t *ptemp, server_rec *s)
{
    void *data = NULL;
    const char *userdata_key = "mesi_post_config";
    
    apr_pool_userdata_get(&data, userdata_key, s->process->pconf);
    
    if (data == NULL) {
        apr_pool_userdata_set((const void *)1, userdata_key, apr_pool_cleanup_null, s->process->pconf);
        return OK;
    }
    
    mesi_config *cfg = ap_get_module_config(s->module_config, &mesi_module);
    const char *lib_path = (cfg && cfg->lib_path) ? cfg->lib_path : LIB_GOMESI_PATH;
    
    go_module = dlopen(lib_path, RTLD_NOW | RTLD_GLOBAL);
    if (go_module == NULL) {
        ap_log_error(APLOG_MARK, APLOG_ERR, 0, s, "mesi: Failed to load %s: %s", lib_path, dlerror());
        return HTTP_INTERNAL_SERVER_ERROR;
    }
    
    EsiParse = (ParseFunc)dlsym(go_module, "Parse");
    if (EsiParse == NULL) {
        ap_log_error(APLOG_MARK, APLOG_ERR, 0, s, "mesi: Failed to find Parse symbol: %s", dlerror());
        dlclose(go_module);
        go_module = NULL;
        return HTTP_INTERNAL_SERVER_ERROR;
    }
    
#ifdef APR_HAS_THREADS
    apr_thread_mutex_create(&parse_mutex, APR_THREAD_MUTEX_DEFAULT, pconf);
#endif
    
    ap_log_error(APLOG_MARK, APLOG_INFO, 0, s, "mesi: Successfully loaded %s", lib_path);
    
    return OK;
}

static void mesi_register_hooks(apr_pool_t *p)
{
    ap_hook_post_config(mesi_post_config, NULL, NULL, APR_HOOK_MIDDLE);
    ap_register_output_filter("MESI", mesi_output_filter, NULL, AP_FTYPE_RESOURCE);
}

AP_DECLARE_MODULE(mesi) = {
    STANDARD20_MODULE_STUFF,
    mesi_create_dir_config,
    mesi_merge_dir_config,
    NULL,
    NULL,
    mesi_cmds,
    mesi_register_hooks
};
