#include <ngx_config.h>
#include <ngx_core.h>
#include <ngx_http.h>

typedef struct {
    ngx_flag_t enable_mesi;
} ngx_http_mesi_loc_conf_t;

static ngx_int_t ngx_http_mesi_handler(ngx_http_request_t *r);
static ngx_int_t ngx_http_mesi_init(ngx_conf_t *cf);
static void* ngx_http_mesi_create_loc_conf(ngx_conf_t *cf);
static char* ngx_http_mesi_merge_loc_conf(ngx_conf_t *cf, void* parent, void* child);

static ngx_command_t ngx_http_mesi_commands[] = {
    {
        ngx_string("enable_mesi"),
        NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
        ngx_conf_set_flag_slot,
        NGX_HTTP_LOC_CONF_OFFSET,
        offsetof(ngx_http_mesi_loc_conf_t, enable_mesi),
        NULL
    },
    ngx_null_command
};

static ngx_http_module_t ngx_http_mesi_module_ctx = {
    NULL,
    ngx_http_mesi_init,
    NULL,
    NULL,
    NULL,
    NULL,
    ngx_http_mesi_create_loc_conf,
    ngx_http_mesi_merge_loc_conf
};

ngx_module_t ngx_http_mesi_module = {
    NGX_MODULE_V1,
    &ngx_http_mesi_module_ctx,
    ngx_http_mesi_commands,
    NGX_HTTP_MODULE,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    NGX_MODULE_V1_PADDING
};

static ngx_int_t ngx_http_mesi_handler(ngx_http_request_t *r)
{
    ngx_http_mesi_loc_conf_t* lcf = ngx_http_get_module_loc_conf(r, ngx_http_mesi_module);
    if (!lcf->enable_mesi) {
        return NGX_DECLINED;
    }
    ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "MESI module handler called");
    ngx_str_t response = ngx_string("Hello, World!\n");
    r->headers_out.status = NGX_HTTP_OK;
    r->headers_out.content_length_n = response.len;
    ngx_http_send_header(r);
    ngx_buf_t* b = ngx_create_temp_buf(r->pool, response.len);
    if (b == NULL) {
        return NGX_HTTP_INTERNAL_SERVER_ERROR;
    }
    ngx_memcpy(b->pos, response.data, response.len);
    b->last = b->pos + response.len;
    b->last_buf = 1;
    ngx_chain_t out = { b, NULL };
    return ngx_http_output_filter(r, &out);
}

static ngx_int_t ngx_http_mesi_init(ngx_conf_t *cf)
{
    ngx_log_error(NGX_LOG_ERR, cf->log, 0, "MESI module init called");
    ngx_http_handler_pt* h;
    ngx_http_core_main_conf_t* cmcf;
    cmcf = ngx_http_conf_get_module_main_conf(cf, ngx_http_core_module);
    h = ngx_array_push(&cmcf->phases[NGX_HTTP_CONTENT_PHASE].handlers);
    if (h == NULL) {
        return NGX_ERROR;
    }
    *h = ngx_http_mesi_handler;
    return NGX_OK;
}

static void* ngx_http_mesi_create_loc_conf(ngx_conf_t *cf)
{
    ngx_http_mesi_loc_conf_t* conf;
    conf = ngx_pcalloc(cf->pool, sizeof(ngx_http_mesi_loc_conf_t));
    if (conf == NULL) {
        return NULL;
    }
    conf->enable_mesi = NGX_CONF_UNSET;
    return conf;
}

static char* ngx_http_mesi_merge_loc_conf(ngx_conf_t *cf, void* parent, void* child)
{
    ngx_http_mesi_loc_conf_t* prev = parent;
    ngx_http_mesi_loc_conf_t* conf = child;
    ngx_conf_merge_value(conf->enable_mesi, prev->enable_mesi, 0);
    return NGX_CONF_OK;
}
