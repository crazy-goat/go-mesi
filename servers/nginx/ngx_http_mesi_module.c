#include <ngx_config.h>
#include <ngx_core.h>
#include <ngx_http.h>

#include "libgomesi.h"

#ifndef LIB_GOMESI_PATH
#define LIB_GOMESI_PATH "/usr/lib/libgomesi.so"
#endif

typedef struct {
  ngx_flag_t enable_mesi;
} ngx_http_mesi_loc_conf_t;

typedef struct {
  ngx_str_t accumulated;
  ngx_flag_t done;
} ngx_http_html_head_filter_ctx_t;

static ngx_http_output_header_filter_pt ngx_http_next_header_filter;
static ngx_http_output_body_filter_pt ngx_http_next_body_filter;

static ngx_int_t ngx_http_html_mesi_head_filter(ngx_http_request_t *r);
static ngx_int_t ngx_http_html_mesi_body_filter(ngx_http_request_t *r,
                                                ngx_chain_t *in);
static ngx_str_t parse(ngx_str_t input, ngx_http_request_t *r);
static ngx_int_t ngx_http_html_head_filter_init(ngx_conf_t *cf);

static void ngx_http_mesi_thread_exit(ngx_cycle_t *cycle);
static ngx_int_t ngx_http_mesi_thread_init(ngx_cycle_t *cycle);

static ngx_int_t ngx_test_content_compression(ngx_http_request_t *r);
static ngx_int_t ngx_test_is_html(ngx_http_request_t *r);

static void *ngx_http_mesi_create_loc_conf(ngx_conf_t *cf);
static char *ngx_http_mesi_merge_loc_conf(ngx_conf_t *cf, void *parent,
                                          void *child);

typedef char *(*ParseFunc)(char *, int, char *);
static void *go_module = NULL;
ParseFunc EsiParse = NULL;

static ngx_command_t ngx_http_mesi_commands[] = {
    {ngx_string("enable_mesi"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
     ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, enable_mesi), NULL},
    ngx_null_command};

static ngx_http_module_t ngx_http_html_head_filter_module_ctx = {
    NULL,                           /* preconfiguration */
    ngx_http_html_head_filter_init, /* postconfiguration */
    NULL,                           /* create main configuration */
    NULL,                           /* init main configuration */
    NULL,                           /* create server configuration */
    NULL,                           /* merge server configuration */
    ngx_http_mesi_create_loc_conf,  /* create location configuration */
    ngx_http_mesi_merge_loc_conf    /* merge location configuration */
};

ngx_module_t ngx_http_mesi_module = {
    NGX_MODULE_V1,
    &ngx_http_html_head_filter_module_ctx, /* module context */
    ngx_http_mesi_commands,                /* module directives */
    NGX_HTTP_MODULE,                       /* module type */
    NULL,                                  /* init master */
    NULL,                                  /* init module */
    ngx_http_mesi_thread_init,             /* init process */
    NULL,                                  /* init thread */
    NULL,                                  /* exit thread */
    ngx_http_mesi_thread_exit,             /* exit process */
    NULL,                                  /* exit master */
    NGX_MODULE_V1_PADDING};

static ngx_int_t ngx_http_html_mesi_head_filter(ngx_http_request_t *r) {
  ngx_http_mesi_loc_conf_t *lcf =
      ngx_http_get_module_loc_conf(r, ngx_http_mesi_module);
  if (!lcf->enable_mesi) {
    return ngx_http_next_header_filter(r);
  }

  ngx_http_html_head_filter_ctx_t *ctx;
  ngx_table_elt_t *h;

  if (r->header_only || r->headers_out.content_length_n == 0) {
    ngx_log_debug0(NGX_LOG_DEBUG_HTTP, r->connection->log, 0,
                   "[mESI head filter]: header only, invalid content length");

    return ngx_http_next_header_filter(r);
  }

  if (ngx_test_content_compression(r) == 1) {
    ngx_log_debug0(NGX_LOG_DEBUG_HTTP, r->connection->log, 0,
                   "[mESI head filter]: compression enabled");
    return ngx_http_next_header_filter(r);
  }

  if (ngx_test_is_html(r) == 0) {
    ngx_log_debug0(NGX_LOG_DEBUG_HTTP, r->connection->log, 0,
                   "[mESI head filter]: content type not html");
    return ngx_http_next_header_filter(r);
  }

  if (r->headers_out.status > NGX_HTTP_BAD_REQUEST) {
    ngx_log_debug0(NGX_LOG_DEBUG_HTTP, r->connection->log, 0,
                   "[mESI head filter]: error status code");
    return ngx_http_next_header_filter(r);
  }

  h = ngx_list_push(&r->headers_out.headers);
  if (h == NULL) {
    ngx_log_debug0(NGX_LOG_DEBUG_HTTP, r->connection->log, 0,
                   "[mESI head filter]: failed to add ");
    return ngx_http_next_header_filter(r);
  }

  h->hash = 1;
  ngx_str_set(&h->key, "Surrogate-Capability");
  ngx_str_set(&h->value, "ESI/1.0");

  ctx = ngx_http_get_module_ctx(r, ngx_http_mesi_module);
  if (ctx == NULL) {
    ctx = ngx_pcalloc(r->pool, sizeof(ngx_http_html_head_filter_ctx_t));
    if (ctx == NULL) {
      return NGX_ERROR;
    }
    ctx->accumulated.len = 0;
    ctx->accumulated.data = NULL;
    ctx->done = 0;
    ngx_http_set_ctx(r, ctx, ngx_http_mesi_module);
  }

  if (r == r->main) { /* Main request */

    ngx_http_clear_content_length(r);
    ngx_http_weak_etag(r);
  }

  return ngx_http_next_header_filter(r);
}

static ngx_int_t ngx_http_html_mesi_body_filter(ngx_http_request_t *r,
                                                ngx_chain_t *in) {
  ngx_http_mesi_loc_conf_t *lcf =
      ngx_http_get_module_loc_conf(r, ngx_http_mesi_module);

  if (!lcf->enable_mesi) {
    return ngx_http_next_body_filter(r, in);
  }

  ngx_http_html_head_filter_ctx_t *ctx;
  ctx = ngx_http_get_module_ctx(r, ngx_http_mesi_module);
  if (ctx == NULL || go_module == NULL) {
    return ngx_http_next_body_filter(r, in);
  }

  ngx_chain_t *cl;
  ngx_buf_t *buf;

  for (cl = in; cl; cl = cl->next) {
    buf = cl->buf;
    if (ngx_buf_size(buf) > 0 && !ctx->done) {
      size_t old_len = ctx->accumulated.len;
      size_t new_len = old_len + ngx_buf_size(buf);

      u_char *new_data = ngx_palloc(r->pool, new_len);
      if (new_data == NULL) {
        return NGX_ERROR;
      }

      if (ctx->accumulated.data) {
        ngx_memcpy(new_data, ctx->accumulated.data, old_len);
      }
      ngx_memcpy(new_data + old_len, buf->pos, ngx_buf_size(buf));

      ctx->accumulated.data = new_data;
      ctx->accumulated.len = new_len;
    }

    if (buf->last_buf && !ctx->done) {
      ctx->done = 1;
      ngx_str_t parsed = parse(ctx->accumulated, r);

      ngx_chain_t *out = ngx_alloc_chain_link(r->pool);
      if (out == NULL) {
        return NGX_ERROR;
      }

      ngx_buf_t *b = ngx_pcalloc(r->pool, sizeof(ngx_buf_t));
      if (b == NULL) {
        return NGX_ERROR;
      }

      r->headers_out.content_length_n = parsed.len;

      b->pos = parsed.data;
      b->last = parsed.data + parsed.len;
      b->memory = 1;
      b->last_buf = 1;

      out->buf = b;
      out->next = NULL;

      return ngx_http_next_body_filter(r, out);
    }
  }

  return NGX_OK;
}

static char *ngx_str_to_cstr(ngx_str_t *input, ngx_pool_t *pool) {
  char *cstr = ngx_palloc(pool, input->len + 1);
  if (cstr == NULL) {
    return NULL;
  }
  ngx_memcpy(cstr, input->data, input->len);
  cstr[input->len] = '\0';
  return cstr;
}

static ngx_str_t parse(ngx_str_t input, ngx_http_request_t *r) {
  ngx_str_t output;

  ngx_str_t scheme = r->schema;
  ngx_str_t host = r->headers_in.host->value;
  size_t len = scheme.len + sizeof("://") - 1 + host.len + sizeof("/") - 1;

  ngx_str_t base_url;
  base_url.len = len;
  base_url.data = ngx_pnalloc(r->pool, len + 1);
  if (base_url.data == NULL) {
    return output;
  }

  ngx_snprintf(base_url.data, len + 1, "%V://%V/", &scheme, &host);

  char *message = EsiParse(ngx_str_to_cstr(&input, r->pool), 5,
                           ngx_str_to_cstr(&base_url, r->pool));

  output.len = ngx_strlen(message);
  output.data = ngx_palloc(r->pool, output.len);
  if (output.data == NULL) {
    free(message);
    output.len = 0;
    return output;
  }
  ngx_memcpy(output.data, message, output.len);
  output.data[output.len] = '\0';
  free(message);
  return output;
}

static ngx_int_t ngx_test_is_html(ngx_http_request_t *r) {

  if (r->headers_out.content_type.len == 0) {
    return 0;
  }

  ngx_str_t content_len = {
      .len = r->headers_out.content_type.len,
      .data = ngx_pcalloc(r->pool,
                          sizeof(u_char) * r->headers_out.content_type.len)};

  if (content_len.data == NULL) {
    return 0;
  }

  ngx_strlow(content_len.data, r->headers_out.content_type.data,
             content_len.len);

  if (ngx_strnstr(content_len.data, "text/html",
                  r->headers_out.content_type.len) != NULL) {
    return 1;
  }

  return 0;
}

static ngx_int_t ngx_test_content_compression(ngx_http_request_t *r) {
  if (r->headers_out.content_encoding == NULL ||
      r->headers_out.content_encoding->value.len == 0) {
    return 0;
  }

  return 1;
}

static ngx_int_t ngx_http_html_head_filter_init(ngx_conf_t *cf) {
  ngx_http_next_header_filter = ngx_http_top_header_filter;
  ngx_http_top_header_filter = ngx_http_html_mesi_head_filter;

  ngx_http_next_body_filter = ngx_http_top_body_filter;
  ngx_http_top_body_filter = ngx_http_html_mesi_body_filter;

  return NGX_OK;
}

static ngx_int_t ngx_http_mesi_thread_init(ngx_cycle_t *cycle) {
  char *error;
  go_module = dlopen(LIB_GOMESI_PATH, RTLD_NOW);

  if (!go_module) {
    dlerror();
    return NGX_ERROR;
  }

  EsiParse = (ParseFunc)dlsym(go_module, "Parse");

  error = dlerror();
  if (error != NULL) {
    ngx_log_error(NGX_LOG_ERR, cycle->log, 0,
                  "Error executing Parse from libgomesi: %s", error);

    return NGX_ERROR;
  }

  return NGX_OK;
}

static void ngx_http_mesi_thread_exit(ngx_cycle_t *cycle) {
  if (go_module) {
    dlclose(go_module);
    go_module = NULL;
  }
}

static void *ngx_http_mesi_create_loc_conf(ngx_conf_t *cf) {
  ngx_http_mesi_loc_conf_t *conf;
  conf = ngx_pcalloc(cf->pool, sizeof(ngx_http_mesi_loc_conf_t));
  if (conf == NULL) {
    return NULL;
  }
  conf->enable_mesi = NGX_CONF_UNSET;
  return conf;
}

static char *ngx_http_mesi_merge_loc_conf(ngx_conf_t *cf, void *parent,
                                          void *child) {
  ngx_http_mesi_loc_conf_t *prev = parent;
  ngx_http_mesi_loc_conf_t *conf = child;
  ngx_conf_merge_value(conf->enable_mesi, prev->enable_mesi, 0);
  return NGX_CONF_OK;
}