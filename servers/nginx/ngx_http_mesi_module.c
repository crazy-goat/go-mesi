#include <ngx_config.h>
#include <ngx_core.h>
#include <ngx_http.h>

#include "libgomesi.h"

#ifndef LIB_GOMESI_PATH
#define LIB_GOMESI_PATH "/usr/lib/libgomesi.a"
#endif

/* Struktura przechowująca całą zawartość odpowiedzi */
typedef struct {
  ngx_str_t accumulated; /* Bufor na cały HTML */
  ngx_flag_t done;       /* Flaga sygnalizująca zakończenie zbierania */
} ngx_http_html_head_filter_ctx_t;

static ngx_http_output_header_filter_pt ngx_http_next_header_filter;
static ngx_http_output_body_filter_pt ngx_http_next_body_filter;

static ngx_int_t ngx_http_html_head_header_filter(ngx_http_request_t *r);
static ngx_int_t ngx_http_html_head_body_filter(ngx_http_request_t *r,
                                                ngx_chain_t *in);
static ngx_str_t parse(ngx_str_t input, ngx_http_request_t *r);
static ngx_int_t ngx_http_html_head_filter_init(ngx_conf_t *cf);

typedef char *(*ParseFunc)(char *, int, char *);

/* Struktura modułu */
static ngx_http_module_t ngx_http_html_head_filter_module_ctx = {
    NULL,                           /* preconfiguration */
    ngx_http_html_head_filter_init, /* postconfiguration */
    NULL,                           /* create main configuration */
    NULL,                           /* init main configuration */
    NULL,                           /* create server configuration */
    NULL,                           /* merge server configuration */
    NULL,                           /* create location configuration */
    NULL                            /* merge location configuration */
};

ngx_module_t ngx_http_mesi_module = {
    NGX_MODULE_V1,
    &ngx_http_html_head_filter_module_ctx, /* module context */
    NULL,                                  /* module directives */
    NGX_HTTP_MODULE,                       /* module type */
    NULL,                                  /* init master */
    NULL,                                  /* init module */
    NULL,                                  /* init process */
    NULL,                                  /* init thread */
    NULL,                                  /* exit thread */
    NULL,                                  /* exit process */
    NULL,                                  /* exit master */
    NGX_MODULE_V1_PADDING};

/* Filtr nagłówków */
static ngx_int_t ngx_http_html_head_header_filter(ngx_http_request_t *r) {
  ngx_http_html_head_filter_ctx_t *ctx;

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

/* Filtr ciała odpowiedzi */
static ngx_int_t ngx_http_html_head_body_filter(ngx_http_request_t *r,
                                                ngx_chain_t *in) {
  ngx_http_html_head_filter_ctx_t *ctx;
  ctx = ngx_http_get_module_ctx(r, ngx_http_mesi_module);
  if (ctx == NULL) {
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
      /* Aktualizacja Content-Length */
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

/* Konwersja ngx_str_t na NULL-terminated char* */
static char *ngx_str_to_cstr(ngx_str_t *input, ngx_pool_t *pool) {
  char *cstr = ngx_palloc(pool, input->len + 1);
  if (cstr == NULL) {
    return NULL;
  }
  ngx_memcpy(cstr, input->data, input->len);
  cstr[input->len] = '\0';
  return cstr;
}

/* Funkcja parse(), która na razie tylko zwraca kopię danych */
static ngx_str_t parse(ngx_str_t input, ngx_http_request_t *r) {
  ngx_str_t output;
  char *error;
  ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "------------Before parse");
  void *go_module = dlopen(LIB_GOMESI_PATH, RTLD_LAZY);
  if (!go_module) {
    ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                  "------------Error loading");
    output.len = 0;
    return output;
  }
  dlerror(); // Wyczyszczenie błędów
  ParseFunc Parse = (ParseFunc)dlsym(go_module, "Parse");

  if ((error = dlerror()) != NULL) {
    fprintf(stderr, "Błąd ładowania funkcji: %s\n", error);
    dlclose(go_module);
    output.len = 0;
    return output;
  }
  ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                "------------After loading");
  const char *message = Parse(ngx_str_to_cstr(&input, r->pool), 5, "");
  output.len = ngx_strlen(message);
  output.data = ngx_palloc(r->pool, output.len);
  if (output.data == NULL) {
    output.len = 0;
    return output;
  }
  ngx_memcpy(output.data, message, output.len);
  output.data[output.len] = '\0';

  return output;
}

/* Rejestracja filtrów */
static ngx_int_t ngx_http_html_head_filter_init(ngx_conf_t *cf) {
  ngx_http_next_header_filter = ngx_http_top_header_filter;
  ngx_http_top_header_filter = ngx_http_html_head_header_filter;

  ngx_http_next_body_filter = ngx_http_top_body_filter;
  ngx_http_top_body_filter = ngx_http_html_head_body_filter;

  return NGX_OK;
}
