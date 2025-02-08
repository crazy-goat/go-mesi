#include <ngx_config.h>
#include <ngx_core.h>
#include <ngx_http.h>

#include "libgomesi.h"

static ngx_int_t mesi_request_handler(ngx_http_request_t *r);

static ngx_int_t mesi_header_filter(ngx_http_request_t *r);
static ngx_int_t mesi_body_filter(ngx_http_request_t *r, ngx_chain_t *in);

static void* mesi_create_loc_conf(ngx_conf_t *cf);
static char* mesi_merge_loc_conf(ngx_conf_t *cf, void *parent, void *child);
static ngx_int_t mesi_init(ngx_conf_t *cf);

typedef struct {
    ngx_flag_t  enable;
} ngx_http_mesi_loc_conf_t;

typedef struct {
    ngx_flag_t done;
} ngx_http_mesi_ctx_t;


static ngx_command_t ngx_http_mesi_commands[] = {

    {
        ngx_string("mesi"),
        NGX_HTTP_LOC_CONF|NGX_CONF_FLAG,
        ngx_conf_set_flag_slot,
        NGX_HTTP_LOC_CONF_OFFSET,
        offsetof(ngx_http_mesi_loc_conf_t, enable),
        NULL
    },

    ngx_null_command
};


static ngx_http_module_t  ngx_http_mesi_module_ctx = {
    NULL,
    mesi_init,

    NULL,
    NULL,

    NULL,
    NULL,

    mesi_create_loc_conf,
    mesi_merge_loc_conf
};


ngx_module_t  ngx_http_mesi_module = {
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


static void *
mesi_create_loc_conf(ngx_conf_t *cf)
{
    ngx_http_mesi_loc_conf_t  *conf;
    conf = ngx_pcalloc(cf->pool, sizeof(ngx_http_mesi_loc_conf_t));
    if (conf == NULL) {
        return NULL;
    }
    conf->enable = NGX_CONF_UNSET;
    return conf;
}

static char *
mesi_merge_loc_conf(ngx_conf_t *cf, void *parent, void *child)
{
    ngx_http_mesi_loc_conf_t *prev = parent;
    ngx_http_mesi_loc_conf_t *conf = child;

    ngx_conf_merge_value(conf->enable, prev->enable, 0);

    return NGX_CONF_OK;
}


static ngx_int_t
mesi_request_handler(ngx_http_request_t *r)
{
    ngx_table_elt_t *h = ngx_list_push(&r->headers_in.headers);
    if (h == NULL) {
        return NGX_ERROR;
    }

    h->hash = 1;
    ngx_str_set(&h->key, "Surrogate-Capability");
    ngx_str_set(&h->value, "ESI/1.0");

    return NGX_DECLINED;
}


static ngx_http_output_header_filter_pt ngx_http_next_header_filter;
static ngx_http_output_body_filter_pt   ngx_http_next_body_filter;

static ngx_int_t
mesi_header_filter(ngx_http_request_t *r)
{
    ngx_http_mesi_loc_conf_t *conf = ngx_http_get_module_loc_conf(r, ngx_http_mesi_module);
    if (!conf->enable) {
        return ngx_http_next_header_filter(r);
    }

    if (r->headers_out.content_type.len < sizeof("text/html") - 1 ||
        ngx_strncasecmp(r->headers_out.content_type.data, (u_char *)"text/html", 9) != 0)
    {
        return ngx_http_next_header_filter(r);
    }

    r->headers_out.content_length_n = -1;

    ngx_http_mesi_ctx_t *ctx = ngx_pcalloc(r->pool, sizeof(ngx_http_mesi_ctx_t));
    if (ctx == NULL) {
        return NGX_ERROR;
    }
    ctx->done = 0;

    ngx_http_set_ctx(r, ctx, ngx_http_mesi_module);

    return ngx_http_next_header_filter(r);
}


static ngx_int_t
mesi_body_filter(ngx_http_request_t *r, ngx_chain_t *in)
{
    ngx_http_mesi_loc_conf_t *conf = ngx_http_get_module_loc_conf(r, ngx_http_mesi_module);
    if (!conf->enable) {
        return ngx_http_next_body_filter(r, in);
    }

    ngx_http_mesi_ctx_t *ctx = ngx_http_get_module_ctx(r, ngx_http_mesi_module);
    if (ctx == NULL) {
        return ngx_http_next_body_filter(r, in);
    }

    if (ctx->done) {
        return ngx_http_next_body_filter(r, in);
    }

    size_t total_size = 0;
    ngx_chain_t *cl;
    ngx_buf_t   *b;

    for (cl = in; cl; cl = cl->next) {
        b = cl->buf;
        if (!b->in_file) {
            total_size += (b->last - b->pos);
        } else {
        }
    }

    if (total_size == 0) {
        ctx->done = 1;
        return ngx_http_next_body_filter(r, in);
    }

    char *raw_body = ngx_palloc(r->pool, total_size + 1);
    if (raw_body == NULL) {
        return NGX_ERROR;
    }

    size_t copied = 0;
    for (cl = in; cl; cl = cl->next) {
        b = cl->buf;
        if (!b->in_file) {
            size_t len = (b->last - b->pos);
            ngx_memcpy(raw_body + copied, b->pos, len);
            copied += len;
        }
    }
    raw_body[copied] = '\0';

    int maxDepth = 10;
    char *defaultUrl = "https://example.com";

    char *parsed_result = Parse(raw_body, maxDepth, defaultUrl);
    if (parsed_result == NULL) {
        return NGX_ERROR;
    }

    size_t parsed_len = ngx_strlen(parsed_result);

    ngx_buf_t *out_buf = ngx_create_temp_buf(r->pool, parsed_len);
    if (out_buf == NULL) {
        FreeString(parsed_result);
        return NGX_ERROR;
    }

    ngx_memcpy(out_buf->pos, parsed_result, parsed_len);
    out_buf->last = out_buf->pos + parsed_len;
    out_buf->last_buf = 1;

    FreeString(parsed_result);

    ngx_chain_t *out_chain = ngx_alloc_chain_link(r->pool);
    if (out_chain == NULL) {
        return NGX_ERROR;
    }

    out_chain->buf = out_buf;
    out_chain->next = NULL;

    ctx->done = 1;

    return ngx_http_next_body_filter(r, out_chain);
}


static ngx_int_t
mesi_init(ngx_conf_t *cf)
{
    ngx_http_core_main_conf_t *cmcf;
    cmcf = ngx_http_conf_get_module_main_conf(cf, ngx_http_core_module);

    ngx_http_handler_pt *h = ngx_array_push(&cmcf->phases[NGX_HTTP_REWRITE_PHASE].handlers);
    if (h == NULL) {
        return NGX_ERROR;
    }
    *h = mesi_request_handler;

    ngx_http_next_header_filter = ngx_http_top_header_filter;
    ngx_http_top_header_filter = mesi_header_filter;

    ngx_http_next_body_filter = ngx_http_top_body_filter;
    ngx_http_top_body_filter = mesi_body_filter;

    return NGX_OK;
}
