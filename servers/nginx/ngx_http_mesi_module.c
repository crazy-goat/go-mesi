#include <ngx_config.h>
#include <ngx_core.h>
#include <ngx_http.h>

#ifndef LIB_GOMESI_PATH
#define LIB_GOMESI_PATH "/usr/lib/libgomesi.so"
#endif

// Mirrors Apache's MESI_MAX_CACHE_KEY_TEMPLATE: bounds the template so a
// runaway config value cannot drive unbounded allocations (the template is
// copied into per-request C strings and evaluated per cache lookup).
// Defense-in-depth: nginx's own config parser caps a single quoted
// argument at ~4090 bytes ("too long parameter"), tighter than this
// module-level check — the module cap keeps nginx consistent with the
// Apache/PHP-extension 4096 limit and stays the active guard if the
// parser limit ever changes.
#define MESI_MAX_CACHE_KEY_TEMPLATE 4096

// Global ESI nesting depth (#180). Matches mesi.MaxMaxDepth (10,000) /
// Apache MESI_MAX_MAX_DEPTH / Caddy max_depth: values outside
// [0, MESI_MAX_MAX_DEPTH] are rejected at config load so a typo can
// never silently become an unbounded nesting limit (libgomesi's Parse*
// would reject it anyway since #414 — this guard keeps the failure at
// `nginx -t` where the operator sees it).
#define MESI_MAX_MAX_DEPTH 10000
// Default when mesi_max_depth is unset — matches the previously
// hardcoded literal 5 in parse() and mesi.EsiParserConfig.MaxDepth's
// default (backward compatible).
#define MESI_DEFAULT_MAX_DEPTH 5

// Global per-include ESI fetch budget in seconds (#184). Matches
// libgomesi's config.MaxTimeoutSeconds (86400) / Apache
// MESI_MAX_TIMEOUT_SECONDS / php-ext `timeout`: values outside
// [1, ...] are rejected at config load, so a typo can never silently
// become a broken budget — 0 would make EVERY include fail immediately
// with ErrTimeBudgetExceeded (mesi/fetch.go) instead of "no timeout",
// and anything above 24h is a unit-confusion misconfiguration.
#define MESI_MAX_TIMEOUT_SECONDS 86400
// Effective budget when mesi_timeout is unset — 30s, libgomesi's
// historical hardcoded value (config.DefaultTimeoutSeconds). The C
// side stores the unset sentinel (NGX_CONF_UNSET) instead of this
// literal: the sentinel keeps the legacy positional path byte-identical
// (Go-side defaultParseTimeout() applies the 30s there) and lets
// parse() distinguish "configured" (routes through ParseJson) from
// "unset". Keep this macro in sync with
// libgomesi/internal/config/timeout.go — it is used in the
// stale-libgomesi warning so the message and the contract cannot drift.
#define MESI_DEFAULT_TIMEOUT_SECONDS 30

// Per-include response body cap in bytes (#208). Matches libgomesi's
// config.MaxMaxResponseSize / Apache MESI_MAX_MAX_RESPONSE_SIZE /
// php-ext `max_response_size` / CLI `-max-response-size`: values
// outside [0, MESI_MAX_MAX_RESPONSE_SIZE] are rejected at config
// load. The upper bound is math.MaxInt64 - 1 — the core computes
// MaxResponseSize + 1 for its io.LimitReader bound (mesi/fetch.go);
// at MaxInt64 that wraps negative and the include would silently
// render an empty body instead of failing (the #448 wrap). 0 is a
// LEGITIMATE configured value ("unlimited" — the core only limits
// when MaxResponseSize > 0), so the unset sentinel must stay
// distinguishable from it (see the merge comment below).
#define MESI_MAX_MAX_RESPONSE_SIZE ((off_t)9223372036854775806LL)

// Per-parse cap on concurrent <esi:include> HTTP fetches (#214). Matches
// libgomesi's config.MaxMaxConcurrentRequests / Apache
// MESI_MAX_MAX_CONCURRENT_REQUESTS / php-ext `max_concurrent_requests`
// / CLI `-max-concurrent-requests`: values outside
// [0, MESI_MAX_MAX_CONCURRENT_REQUESTS] are rejected at config load.
// The upper bound is 999999999 — neither core nor Caddy caps the
// value, so the bound is derived from the transport (a C `int` parsed
// by the shared strict 9-digit parsers), the largest value every
// platform can represent without a wrap (the #170 rationale). 0 is a
// LEGITIMATE configured value ("unlimited" — the core only installs
// the admission-control semaphore when MaxConcurrentRequests > 0,
// mesi/parser.go), so the unset sentinel must stay distinguishable
// from it (see the merge comment below).
#define MESI_MAX_MAX_CONCURRENT_REQUESTS 999999999

typedef struct {
  ngx_flag_t enable_mesi;
  ngx_int_t  max_depth;      // ESI nesting depth (#180): NGX_CONF_UNSET
                             // until merged (default 5), explicit 0 =
                             // passthrough (no ESI fetch), range
                             // [0, MESI_MAX_MAX_DEPTH] validated by the
                             // directive setter
  ngx_int_t  timeout_seconds;// Global per-include fetch budget in
                             // seconds (#184): NGX_CONF_UNSET =
                             // unset (legacy positional path, effective
                             // 30s applied Go-side), a stored value is
                             // in [1, MESI_MAX_TIMEOUT_SECONDS]
                             // validated by the directive setter —
                             // a stored value routes the parse through
                             // libgomesi ParseJson
  off_t      max_response_size; // Per-include response body cap in
                             // bytes (#208): NGX_CONF_UNSET =
                             // unset (legacy positional path, effective
                             // "unlimited" applied Go-side), a stored
                             // value is in [0,
                             // MESI_MAX_MAX_RESPONSE_SIZE] validated
                             // by the directive setter — a stored
                             // value routes the parse through
                             // libgomesi ParseJson; 0 ("unlimited")
                             // IS storable, which is why the sentinel
                             // is -1 and not 0
  ngx_int_t  max_concurrent_requests; // Per-parse cap on concurrent
                             // <esi:include> HTTP fetches (#214):
                             // NGX_CONF_UNSET = unset (legacy
                             // positional path, effective
                             // "unlimited" applied Go-side), a stored
                             // value is in [0,
                             // MESI_MAX_MAX_CONCURRENT_REQUESTS]
                             // validated by the directive setter — a
                             // stored value routes the parse through
                             // libgomesi ParseJson; 0 ("unlimited")
                             // IS storable, which is why the sentinel
                             // is -1 and not 0
  ngx_str_t  cache_backend;  // "" (off), "memory", "redis", "memcached"
  ngx_int_t  cache_size;     // max entries for memory cache
  ngx_int_t  cache_ttl;      // TTL in seconds
  ngx_str_t  cache_memcached_servers;  // space-separated "host:port host:port"
  ngx_str_t  cache_redis_addr;         // e.g. "localhost:6379"
  ngx_str_t  cache_redis_password;
  ngx_int_t  cache_redis_db;           // Redis database number (0-15)
  ngx_flag_t block_private_ips;        // SSRF: block private/reserved IPs (default ON)
  ngx_str_t  allowed_hosts;  // space-separated host whitelist ("" = no restriction)
  ngx_flag_t allow_private_ips_for_allowed;  // private-IP bypass for allowed_hosts (default OFF)
  ngx_str_t  cache_key_template;  // "" = URL-only DefaultCacheKey (backward compat)
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
static char *ngx_http_mesi_set_max_depth(ngx_conf_t *cf, ngx_command_t *cmd,
                                         void *conf);
static char *ngx_http_mesi_set_timeout(ngx_conf_t *cf, ngx_command_t *cmd,
                                       void *conf);
static char *ngx_http_mesi_set_max_response_size(ngx_conf_t *cf,
                                                 ngx_command_t *cmd,
                                                 void *conf);
static char *ngx_http_mesi_set_max_concurrent_requests(ngx_conf_t *cf,
                                                       ngx_command_t *cmd,
                                                       void *conf);

typedef char *(*ParseFunc)(char *, int, char *);
typedef char *(*ParseWithConfigFunc)(char *, int, char *, char *, int);
typedef char *(*ParseWithConfigExFunc)(char *, int, char *, char *, int, int);
typedef char *(*ParseWithConfigCtxFunc)(char *, int, char *, char *, int, int, char *, char *);
typedef char *(*ParseJsonFunc)(char *, char *);
typedef int (*InitCacheFunc)(char *, int, int);
typedef int (*InitCacheWithConfigFunc)(char *, int, int, char *);
typedef void (*FreeCacheFunc)(void);

static void *go_module = NULL;
static ParseFunc EsiParse = NULL;
static ParseWithConfigFunc EsiParseWithConfig = NULL;
static ParseWithConfigExFunc EsiParseWithConfigEx = NULL;
static ParseWithConfigCtxFunc EsiParseWithConfigCtx = NULL;
static ParseJsonFunc EsiParseJson = NULL;
static InitCacheFunc EsiInitCache = NULL;
static InitCacheWithConfigFunc EsiInitCacheWithConfig = NULL;
static FreeCacheFunc EsiFreeCache = NULL;
static ngx_flag_t cache_initialized = 0;
static ngx_str_t cache_last_backend = ngx_null_string;

static ngx_command_t ngx_http_mesi_commands[] = {
    {ngx_string("enable_mesi"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
     ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, enable_mesi), NULL},

    // Global ESI nesting depth (#180). Custom setter instead of
    // ngx_conf_set_num_slot: the stock slot setter only runs ngx_atoi,
    // which has no upper bound — a typo like 10001 would pass
    // `nginx -t` and reach libgomesi. See ngx_http_mesi_set_max_depth
    // below.
    {ngx_string("mesi_max_depth"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_http_mesi_set_max_depth, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, max_depth), NULL},

    // Global per-include fetch budget in seconds (#184). Custom setter
    // instead of ngx_conf_set_num_slot: the stock slot setter only runs
    // ngx_atoi, which has no lower or upper bound — `mesi_timeout 0`
    // would pass `nginx -t` and make every include fail immediately
    // (ErrTimeBudgetExceeded), `mesi_timeout 86401` would reach
    // libgomesi. See ngx_http_mesi_set_timeout below.
    {ngx_string("mesi_timeout"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_http_mesi_set_timeout, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, timeout_seconds), NULL},

    // Per-include response body cap in bytes (#208). Custom setter
    // instead of ngx_conf_set_size_slot / ngx_conf_set_off_slot: the
    // issue sketched ngx_parse_size (k/m/g suffixes), but every other
    // platform landed plain-integer bytes with a strict digit parser
    // (Apache parse_nonneg_off, php-ext IS_LONG, CLI int64) — the
    // cross-platform grammar wins, and the stock size slot setters
    // have no upper bound against MESI_MAX_MAX_RESPONSE_SIZE either
    // (MaxInt64 would reach libgomesi and wrap its +1 LimitReader
    // bound). See ngx_http_mesi_set_max_response_size below.
    {ngx_string("mesi_max_response_size"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_http_mesi_set_max_response_size, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, max_response_size), NULL},

    // Per-parse cap on concurrent <esi:include> HTTP fetches (#214).
    // Custom setter instead of ngx_conf_set_num_slot (the issue's
    // sketch): the stock slot setter only runs ngx_atoi, which has no
    // upper bound — `mesi_max_concurrent_requests 1000000000` would
    // pass `nginx -t` and reach libgomesi, diverging from every other
    // platform's [0, 999999999] range. See
    // ngx_http_mesi_set_max_concurrent_requests below.
    {ngx_string("mesi_max_concurrent_requests"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_http_mesi_set_max_concurrent_requests, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, max_concurrent_requests), NULL},

    {ngx_string("mesi_cache_backend"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, cache_backend), NULL},

    {ngx_string("mesi_cache_size"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, cache_size), NULL},

    {ngx_string("mesi_cache_ttl"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, cache_ttl), NULL},

    {ngx_string("mesi_cache_memcached_servers"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, cache_memcached_servers), NULL},

    {ngx_string("mesi_cache_redis_addr"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, cache_redis_addr), NULL},

    {ngx_string("mesi_cache_redis_password"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, cache_redis_password), NULL},

    {ngx_string("mesi_cache_redis_db"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
      ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
      offsetof(ngx_http_mesi_loc_conf_t, cache_redis_db), NULL},

    {ngx_string("mesi_block_private_ips"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
     ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, block_private_ips), NULL},

    {ngx_string("mesi_allowed_hosts"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, allowed_hosts), NULL},

    {ngx_string("mesi_allow_private_ips_for_allowed"), NGX_HTTP_LOC_CONF | NGX_CONF_FLAG,
     ngx_conf_set_flag_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, allow_private_ips_for_allowed), NULL},

    {ngx_string("mesi_cache_key_template"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
     ngx_conf_set_str_slot, NGX_HTTP_LOC_CONF_OFFSET,
     offsetof(ngx_http_mesi_loc_conf_t, cache_key_template), NULL},

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

  if (lcf->allowed_hosts.len > 0 && EsiParseWithConfig == NULL) {
    ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                  "mesi: ParseWithConfig unavailable in libgomesi — "
                  "mesi_allowed_hosts cannot be enforced; returning HTTP 500 "
                  "(fail closed)");
    r->headers_out.status = NGX_HTTP_INTERNAL_SERVER_ERROR;
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

  // in == NULL is the writer's wake-up/flush pass (ngx_http_writer calls
  // the output chain with NULL data to push whatever r->out still holds).
  // It MUST be forwarded: the write filter below owns r->out, and
  // swallowing it here left every response whose first write did not
  // drain the chain (bigger than sendfile_max_chunk's 2 MB per-pass
  // limit, or a slow/partial-drain client) hanging with data pending
  // until send_timeout killed the connection — discovered while proving
  // #208's 10 MB / 50 MB fixtures, present on main since the module's
  // creation. A non-NULL chain without last_buf is deliberately NOT
  // forwarded: its bytes are already accumulated into ctx above and
  // passing them on would duplicate them in the response.
  if (in == NULL) {
    return ngx_http_next_body_filter(r, NULL);
  }

  return NGX_OK;
}

static char *ngx_str_to_cstr(ngx_str_t *input, ngx_pool_t *pool);
static size_t ngx_http_mesi_unicode_space(const u_char *p, size_t len);
static char *build_memcached_config_json(ngx_http_mesi_loc_conf_t *lcf, ngx_pool_t *pool);
static char *build_redis_config_json(ngx_http_mesi_loc_conf_t *lcf, ngx_pool_t *pool);
static char *build_request_ctx_json(ngx_http_request_t *r, ngx_str_t *template,
                                    ngx_pool_t *pool);
static ngx_int_t ngx_http_mesi_contains(ngx_str_t *haystack, const u_char *needle);
static size_t mesi_json_escaped_len(const u_char *s, size_t len);
static void mesi_json_write_escaped(u_char **w, const u_char *s, size_t len);
static void mesi_json_append_str(u_char **w, const u_char *s, size_t len);

// build_memcached_config_json constructs a JSON blob for
// libgomesi.InitCacheWithConfig("memcached", ...). The JSON is
// {"servers":["host:port","host:port",...]}.
// When no servers are configured, returns {"servers":[]} so libgomesi
// produces a deterministic "servers required" error instead of silently
// defaulting to localhost:11211.
static char *build_memcached_config_json(ngx_http_mesi_loc_conf_t *lcf, ngx_pool_t *pool) {
    if (lcf->cache_memcached_servers.len == 0) {
        char *empty = ngx_palloc(pool, sizeof("{\"servers\":[]}"));
        if (empty == NULL) return NULL;
        ngx_memcpy(empty, "{\"servers\":[]}", sizeof("{\"servers\":[]}"));
        return empty;
    }

    // Copy the string so we can tokenise it.
    char *servers = ngx_str_to_cstr(&lcf->cache_memcached_servers, pool);
    if (servers == NULL) return NULL;

    // First pass: count tokens.
    int ntok = 0;
    int in_token = 0;
    for (char *p = servers; *p; p++) {
        if (*p == ' ') {
            in_token = 0;
        } else if (!in_token) {
            in_token = 1;
            ntok++;
        }
    }

    // Second pass: measure total JSON size.
    // Prefix: {"servers":[  (12 bytes)
    // Each token: "escaped",  (worst case: tokenlen*2 + 3)
    // Suffix: ]}  (2 bytes) + NUL (1 byte)
    size_t total = 12 + 2 + 1;  // prefix + ]} + NUL
    if (ntok > 1) total += (size_t)(ntok - 1);  // commas between tokens

    // Restore pointer for second pass.
    char *q = servers;
    int ti;
    for (ti = 0; ti < ntok; ti++) {
        // Skip leading spaces.
        while (*q == ' ') q++;
        char *start = q;
        while (*q && *q != ' ') q++;
        // Measure worst-case escaped length.
        size_t tlen = (size_t)(q - start);
        total += tlen * 2 + 2;  // worst-case escaped w/ quotes
    }

    char *buf = ngx_palloc(pool, total);
    if (buf == NULL) return NULL;

    char *w = buf;
    memcpy(w, "{\"servers\":[", 12); w += 12;

    // Reset q for third pass (write).
    q = servers;
    for (ti = 0; ti < ntok; ti++) {
        while (*q == ' ') q++;
        char *start = q;
        while (*q && *q != ' ') q++;
        if (ti > 0) *w++ = ',';
        *w++ = '"';
        for (char *r = start; r < q; r++) {
            if (*r == '"' || *r == '\\') *w++ = '\\';
            *w++ = *r;
        }
        *w++ = '"';
    }

    *w++ = ']';
    *w++ = '}';
    *w = '\0';
    return buf;
}

// build_redis_config_json constructs a JSON blob for
// libgomesi.InitCacheWithConfig("redis", ...). The JSON is
// {"redisAddr":"host:port","redisPassword":"…","redisDB":N}.
// When addr is empty, defaults to "localhost:6379" (libgomesi default).
static char *build_redis_config_json(ngx_http_mesi_loc_conf_t *lcf, ngx_pool_t *pool) {
    char *addr = lcf->cache_redis_addr.len > 0
        ? ngx_str_to_cstr(&lcf->cache_redis_addr, pool)
        : "localhost:6379";
    char *password = lcf->cache_redis_password.len > 0
        ? ngx_str_to_cstr(&lcf->cache_redis_password, pool)
        : "";

    // Measure: {"redisAddr":"...","redisPassword":"...","redisDB":N}
    // addr and password are escaped (worst case: double the length).
    size_t addr_len = ngx_strlen(addr);
    size_t pwd_len = ngx_strlen(password);
    size_t escaped_addr_len = addr_len * 2;
    size_t escaped_pwd_len = pwd_len * 2;

    // Fixed overhead (excluding escaped addr/pwd):
    //   {"redisAddr":"  = 14
    //   ","redisPassword":" = 19
    //   ","redisDB":  = 12
    //   <max 2 digits for 0..15> = 2
    //   } = 1
    //   NUL = 1
    // Total = 14 + 19 + 12 + 2 + 1 + 1 = 49
    size_t total = 49 + escaped_addr_len + escaped_pwd_len;

    char *buf = ngx_palloc(pool, total);
    if (buf == NULL) return NULL;

    char *w = buf;
    memcpy(w, "{\"redisAddr\":\"", 14); w += 14;

    // Escape addr
    for (char *r = addr; *r; r++) {
        if (*r == '"' || *r == '\\') *w++ = '\\';
        *w++ = *r;
    }

    memcpy(w, "\",\"redisPassword\":\"", 19); w += 19;

    // Escape password
    for (char *r = password; *r; r++) {
        if (*r == '"' || *r == '\\') *w++ = '\\';
        *w++ = *r;
    }

    // Append redisDB
    ngx_int_t db = lcf->cache_redis_db;
    if (db < 0) db = 0;
    int len = snprintf(w, total - (size_t)(w - buf), "\",\"redisDB\":%d}", (int)db);
    if (len < 0) return NULL;
    w += len;
    *w = '\0';

    return buf;
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

// JSON escaping for the request context — mirrors the Apache
// (mod_mesi.c json_escape_append) and php-ext (mesi_json_append_escape)
// helpers so every C platform serialises the context identically:
// '"' and '\' are backslash-escaped, control bytes < 0x20 become
// \u00XX. Bytes >= 0x20 (including UTF-8 sequences and DEL) pass through
// verbatim — Go's encoding/json accepts them.
static size_t mesi_json_escaped_len(const u_char *s, size_t len) {
  size_t n = 0;
  for (size_t i = 0; i < len; i++) {
    if (s[i] == '"' || s[i] == '\\') {
      n += 2;
    } else if (s[i] < 0x20) {
      n += 6;
    } else {
      n++;
    }
  }
  return n;
}

static void mesi_json_write_escaped(u_char **w, const u_char *s, size_t len) {
  static const u_char hex[] = "0123456789abcdef";
  for (size_t i = 0; i < len; i++) {
    if (s[i] == '"' || s[i] == '\\') {
      *(*w)++ = '\\';
      *(*w)++ = s[i];
    } else if (s[i] < 0x20) {
      *(*w)++ = '\\';
      *(*w)++ = 'u';
      *(*w)++ = '0';
      *(*w)++ = '0';
      *(*w)++ = hex[(s[i] >> 4) & 0xf];
      *(*w)++ = hex[s[i] & 0xf];
    } else {
      *(*w)++ = s[i];
    }
  }
}

// Append a complete JSON string literal (quotes + escaped body).
static void mesi_json_append_str(u_char **w, const u_char *s, size_t len) {
  *(*w)++ = '"';
  mesi_json_write_escaped(w, s, len);
  *(*w)++ = '"';
}

// Decimal rendering for the non-negative ngx_int_t values carried in
// the ParseJson config blob (#184). Both values are range-validated
// before they get here (depth by the directive setter + the parse()
// guard, timeout by the directive setter + the parse() guard), so no
// sign handling is needed and v == 0 renders as a single "0".
static size_t mesi_json_uint_len(ngx_int_t v) {
  size_t n = 1;
  while (v >= 10) {
    v /= 10;
    n++;
  }
  return n;
}

static void mesi_json_write_uint(u_char **w, ngx_int_t v) {
  u_char digits[20];  // ngx_int_t is at most 64-bit: 19 digits + sign
  size_t n = 0;
  do {
    digits[n++] = (u_char)('0' + v % 10);
    v /= 10;
  } while (v > 0);
  while (n > 0) {
    *(*w)++ = digits[--n];
  }
}

// Decimal rendering for the non-negative off_t carried in the
// ParseJson config blob (#208, mesi_max_response_size). The value is
// range-validated to [0, MESI_MAX_MAX_RESPONSE_SIZE] by the directive
// setter at config load and again by the defense-in-depth guard at
// the top of parse() before this runs, so no sign handling is needed
// and v == 0 renders as a single "0" (the documented "unlimited"
// value). off_t rather than ngx_int_t: ngx_int_t is 32-bit on 32-bit
// nginx builds and cannot express byte counts above 2 GB.
static size_t mesi_json_off_len(off_t v) {
  size_t n = 1;
  while (v >= 10) {
    v /= 10;
    n++;
  }
  return n;
}

static void mesi_json_write_off(u_char **w, off_t v) {
  u_char digits[20];  // off_t is at most 64-bit: 19 digits + sign
  size_t n = 0;
  do {
    digits[n++] = (u_char)('0' + v % 10);
    v /= 10;
  } while (v > 0);
  while (n > 0) {
    *(*w)++ = digits[--n];
  }
}

// Bounded substring search for non-NUL-terminated ngx_str_t data.
// ngx_strnstr() delegates to ngx_strncmp(), which may read past the len
// boundary when a partial match starts near the end; this helper never
// touches bytes outside [data, data + len).
static ngx_int_t ngx_http_mesi_contains(ngx_str_t *haystack,
                                        const u_char *needle) {
  size_t nlen = ngx_strlen(needle);
  size_t i;
  if (haystack->len < nlen) {
    return 0;
  }
  for (i = 0; i + nlen <= haystack->len; i++) {
    if (ngx_memcmp(haystack->data + i, needle, nlen) == 0) {
      return 1;
    }
  }
  return 0;
}

// build_request_ctx_json serialises the incoming request's headers and
// cookies into the shared request-context format consumed by libgomesi
// ParseWithConfigCtx / mesi.BuildCacheKey:
//   {"headers":{"Name":"value"},"cookies":[{"name":"n","value":"v"}]}
// Header lookup on the Go side is case-insensitive (BuildCacheKey tries
// canonical/lower/upper forms), so nginx's lowercased header names work
// with any placeholder capitalisation. The Cookie header is excluded from
// the headers map and its value is tokenised into the cookies array —
// passing it in both would duplicate cookies on the Go side
// (http.Request.AddCookie appends to the same Header["Cookie"] entry).
//
// When the template contains no ${header:…}/${cookie:…} placeholder the
// context cannot influence the key, so "" is returned and libgomesi uses
// a dummy request — the per-request JSON build is skipped entirely
// (same optimisation as Apache's build_request_ctx_json).
//
// Two passes over the header list (measure, then write) keep the
// allocation exact: nginx pools have no realloc, so the Apache
// grow-and-copy pattern would leak pool memory on every growth.
static char *build_request_ctx_json(ngx_http_request_t *r, ngx_str_t *template,
                                    ngx_pool_t *pool) {
  if (template == NULL || template->len == 0) {
    return "";
  }
  int needs_ctx = ngx_http_mesi_contains(template, (u_char *)"${header:") ||
                  ngx_http_mesi_contains(template, (u_char *)"${cookie:");
  if (!needs_ctx) {
    return "";
  }

  // Pass 1: measure the exact JSON size.
  //   {"headers":{                       12 bytes
  //   per header:  "k":"v"                5 fixed bytes (4 quotes + ':')
  //                                      (+1 comma when not first)
  //   },"cookies":[                      13 bytes
  //   per cookie:  {"name":"n","value":"v"}
  //                                      22 fixed bytes: '{"name":"'
  //                                      (9, incl. the opening name
  //                                      quote) + '","value":"' (11,
  //                                      incl. both quote pairs around
  //                                      the comma+key) + closing value
  //                                      quote (1) + '}' (1)
  //                                      (+1 comma when not first)
  //   ]}                                 2 bytes
  //   + NUL                             1 byte
  size_t total = 12 + 13 + 2 + 1;
  ngx_uint_t n_headers = 0, n_cookies = 0;

  ngx_list_part_t *part = &r->headers_in.headers.part;
  ngx_table_elt_t *header = part->elts;
  ngx_uint_t i;
  for (i = 0; /* void */; i++) {
    if (i >= part->nelts) {
      if (part->next == NULL) {
        break;
      }
      part = part->next;
      header = part->elts;
      i = 0;
    }
    if (header[i].hash == 0) {
      continue;  // skipped/hidden header entry
    }
    if (header[i].key.len == 6 &&
        ngx_strncasecmp(header[i].key.data, (u_char *)"cookie", 6) == 0) {
      // Cookie headers are tokenised below (all of them).
      const u_char *c = header[i].value.data;
      size_t clen = header[i].value.len;
      while (clen > 0) {
        while (clen > 0 && (*c == ' ' || *c == ';' || *c == '\t')) {
          c++;
          clen--;
        }
        if (clen == 0) {
          break;
        }
        const u_char *name_start = c;
        size_t name_len = 0;
        while (clen > 0 && *c != '=' && *c != ';') {
          c++;
          clen--;
          name_len++;
        }
        if (clen == 0 || *c != '=') {
          // No '=': not a name=value pair — skip to the next ';'.
          while (clen > 0 && *c != ';') {
            c++;
            clen--;
          }
          continue;
        }
        clen--;
        c++;
        const u_char *val_start = c;
        size_t val_len = 0;
        while (clen > 0 && *c != ';') {
          c++;
          clen--;
          val_len++;
        }
        while (name_len > 0 &&
               (name_start[name_len - 1] == ' ' || name_start[name_len - 1] == '\t')) {
          name_len--;
        }
        while (name_len > 0 && (*name_start == ' ' || *name_start == '\t')) {
          name_start++;
          name_len--;
        }
        while (val_len > 0 &&
               (val_start[val_len - 1] == ' ' || val_start[val_len - 1] == '\t')) {
          val_len--;
        }
        while (val_len > 0 && (*val_start == ' ' || *val_start == '\t')) {
          val_start++;
          val_len--;
        }
        if (name_len == 0) {
          continue;
        }
        n_cookies++;
        // 22 fixed bytes per cookie entry (see the Pass 1 comment).
        total += 22 + mesi_json_escaped_len(name_start, name_len) +
                 mesi_json_escaped_len(val_start, val_len);
        if (n_cookies > 1) {
          total++;
        }
      }
      continue;
    }
    n_headers++;
    // 5 fixed bytes per header entry: '"' key '"' ':' '"' value '"'
    // (two JSON string literals = 4 quotes, plus the ':').
    total += 5 + mesi_json_escaped_len(header[i].key.data, header[i].key.len) +
             mesi_json_escaped_len(header[i].value.data, header[i].value.len);
    if (n_headers > 1) {
      total++;
    }
  }

  u_char *buf = ngx_palloc(pool, total);
  if (buf == NULL) {
    return NULL;
  }
  u_char *w = buf;

  // Pass 2: write.
  ngx_memcpy(w, "{\"headers\":{", 12);
  w += 12;

  n_headers = 0;
  part = &r->headers_in.headers.part;
  header = part->elts;
  for (i = 0; /* void */; i++) {
    if (i >= part->nelts) {
      if (part->next == NULL) {
        break;
      }
      part = part->next;
      header = part->elts;
      i = 0;
    }
    if (header[i].hash == 0) {
      continue;
    }
    if (header[i].key.len == 6 &&
        ngx_strncasecmp(header[i].key.data, (u_char *)"cookie", 6) == 0) {
      continue;  // serialised in the cookies array below
    }
    if (n_headers > 0) {
      *w++ = ',';
    }
    n_headers++;
    mesi_json_append_str(&w, header[i].key.data, header[i].key.len);
    *w++ = ':';
    mesi_json_append_str(&w, header[i].value.data, header[i].value.len);
  }

  ngx_memcpy(w, "},\"cookies\":[", 13);
  w += 13;

  n_cookies = 0;
  part = &r->headers_in.headers.part;
  header = part->elts;
  for (i = 0; /* void */; i++) {
    if (i >= part->nelts) {
      if (part->next == NULL) {
        break;
      }
      part = part->next;
      header = part->elts;
      i = 0;
    }
    if (header[i].hash == 0) {
      continue;
    }
    if (!(header[i].key.len == 6 &&
          ngx_strncasecmp(header[i].key.data, (u_char *)"cookie", 6) == 0)) {
      continue;
    }
    const u_char *c = header[i].value.data;
    size_t clen = header[i].value.len;
    while (clen > 0) {
      while (clen > 0 && (*c == ' ' || *c == ';' || *c == '\t')) {
        c++;
        clen--;
      }
      if (clen == 0) {
        break;
      }
      const u_char *name_start = c;
      size_t name_len = 0;
      while (clen > 0 && *c != '=' && *c != ';') {
        c++;
        clen--;
        name_len++;
      }
      if (clen == 0 || *c != '=') {
        while (clen > 0 && *c != ';') {
          c++;
          clen--;
        }
        continue;
      }
      clen--;
      c++;
      const u_char *val_start = c;
      size_t val_len = 0;
      while (clen > 0 && *c != ';') {
        c++;
        clen--;
        val_len++;
      }
      while (name_len > 0 &&
             (name_start[name_len - 1] == ' ' || name_start[name_len - 1] == '\t')) {
        name_len--;
      }
      while (name_len > 0 && (*name_start == ' ' || *name_start == '\t')) {
        name_start++;
        name_len--;
      }
      while (val_len > 0 &&
             (val_start[val_len - 1] == ' ' || val_start[val_len - 1] == '\t')) {
        val_len--;
      }
      while (val_len > 0 && (*val_start == ' ' || *val_start == '\t')) {
        val_start++;
        val_len--;
      }
      if (name_len == 0) {
        continue;
      }
      if (n_cookies > 0) {
        *w++ = ',';
      }
      n_cookies++;
      ngx_memcpy(w, "{\"name\":", 8);
      w += 8;
      mesi_json_append_str(&w, name_start, name_len);
      ngx_memcpy(w, ",\"value\":", 9);
      w += 9;
      mesi_json_append_str(&w, val_start, val_len);
      *w++ = '}';
    }
  }

  *w++ = ']';
  *w++ = '}';
  *w = '\0';

  return (char *)buf;
}

// build_parse_json_config renders the fully-resolved per-request parse
// configuration into the JSON blob accepted by libgomesi's ParseJson
// entry point (the #167 config.ParseConfig schema, keys: maxDepth,
// defaultUrl, allowedHosts, blockPrivateIPs,
// allowPrivateIPsForAllowedHosts, cacheKeyTemplate, requestCtx,
// timeoutSeconds, maxResponseSize, maxConcurrentRequests). Only called
// when mesi_timeout, mesi_max_response_size or
// mesi_max_concurrent_requests is set (that is what routes a request
// through ParseJson, #184/#208/#214 — mirror of Apache's
// build_parse_json_config + used_parse_json pattern from #167).
// Every other key mirrors exactly what the legacy positional path
// would pass, so behaviour is identical except for the values the
// configured directives carry.
// timeoutSeconds travels in SECONDS (the nanosecond conversion happens
// Go-side in config.ResolveTimeout), maxResponseSize in BYTES and
// maxConcurrentRequests as a plain count; all three are rendered only
// when their directive is configured — an absent key resolves Go-side
// to the same value the positional path applies (30s / 0 = unlimited /
// 0 = unlimited), so each key's presence is purely a function of its
// directive being set.
// cacheKeyTemplate/requestCtx are included only when a template is
// configured, mirroring ParseWithConfigCtx's contract (absent/empty
// template → URL-only keys; requestCtx is passed through verbatim as
// pre-rendered JSON and omitted when build_request_ctx_json returned
// "", so a template without ${header:}/${cookie:} gets the exact same
// dummy-request treatment as on the positional path).
// The key prefixes are local arrays so sizeof()-1 measures them —
// hand-counted JSON lengths are a classic off-by-one; two passes
// (measure, then write) keep the allocation exact because nginx pools
// have no realloc.
static char *build_parse_json_config(ngx_http_mesi_loc_conf_t *lcf,
                                     ngx_int_t depth, const char *base_url,
                                     const char *allowed_hosts,
                                     const char *ctx_json, ngx_pool_t *pool) {
  static const char pfx_depth[] = "{\"maxDepth\":";
  static const char pfx_url[] = ",\"defaultUrl\":";
  static const char pfx_hosts[] = ",\"allowedHosts\":";
  static const char pfx_block[] = ",\"blockPrivateIPs\":";
  static const char pfx_bypass[] =
      ",\"allowPrivateIPsForAllowedHosts\":";
  static const char pfx_timeout[] = ",\"timeoutSeconds\":";
  static const char pfx_maxrs[] = ",\"maxResponseSize\":";
  static const char pfx_maxcr[] = ",\"maxConcurrentRequests\":";
  static const char pfx_tmpl[] = ",\"cacheKeyTemplate\":";
  static const char pfx_ctx[] = ",\"requestCtx\":";

  int has_timeout = lcf->timeout_seconds != NGX_CONF_UNSET;
  int has_maxrs = lcf->max_response_size != NGX_CONF_UNSET;
  int has_maxcr = lcf->max_concurrent_requests != NGX_CONF_UNSET;
  int has_tmpl = lcf->cache_key_template.len > 0;
  int has_ctx = has_tmpl && ctx_json != NULL && ctx_json[0] != '\0';
  const char *block_str = lcf->block_private_ips ? "true" : "false";
  size_t block_len = lcf->block_private_ips ? 4 : 5;   // "true"/"false"
  const char *bypass_str =
      lcf->allow_private_ips_for_allowed ? "true" : "false";
  size_t bypass_len = lcf->allow_private_ips_for_allowed ? 4 : 5;
  size_t url_len = strlen(base_url);
  size_t hosts_len = strlen(allowed_hosts);

  // Pass 1: measure (every JSON string literal costs its escaped body
  // plus the two surrounding quotes).
  size_t total = sizeof(pfx_depth) - 1 + mesi_json_uint_len(depth);
  total += sizeof(pfx_url) - 1 + 2 +
           mesi_json_escaped_len((const u_char *)base_url, url_len);
  total += sizeof(pfx_hosts) - 1 + 2 +
           mesi_json_escaped_len((const u_char *)allowed_hosts, hosts_len);
  total += sizeof(pfx_block) - 1 + block_len;
  total += sizeof(pfx_bypass) - 1 + bypass_len;
  if (has_timeout) {
    total += sizeof(pfx_timeout) - 1 +
             mesi_json_uint_len(lcf->timeout_seconds);
  }
  if (has_maxrs) {
    total += sizeof(pfx_maxrs) - 1 + mesi_json_off_len(lcf->max_response_size);
  }
  if (has_maxcr) {
    total += sizeof(pfx_maxcr) - 1 +
             mesi_json_uint_len(lcf->max_concurrent_requests);
  }
  if (has_tmpl) {
    total += sizeof(pfx_tmpl) - 1 + 2 +
             mesi_json_escaped_len(lcf->cache_key_template.data,
                                   lcf->cache_key_template.len);
  }
  if (has_ctx) {
    total += sizeof(pfx_ctx) - 1 + strlen(ctx_json);
  }
  total += 1;  // '}'
  total += 1;  // NUL

  char *buf = ngx_palloc(pool, total);
  if (buf == NULL) {
    return NULL;
  }

  // Pass 2: write.
  u_char *w = (u_char *)buf;
  ngx_memcpy(w, pfx_depth, sizeof(pfx_depth) - 1);
  w += sizeof(pfx_depth) - 1;
  mesi_json_write_uint(&w, depth);

  ngx_memcpy(w, pfx_url, sizeof(pfx_url) - 1);
  w += sizeof(pfx_url) - 1;
  mesi_json_append_str(&w, (const u_char *)base_url, url_len);

  ngx_memcpy(w, pfx_hosts, sizeof(pfx_hosts) - 1);
  w += sizeof(pfx_hosts) - 1;
  mesi_json_append_str(&w, (const u_char *)allowed_hosts, hosts_len);

  ngx_memcpy(w, pfx_block, sizeof(pfx_block) - 1);
  w += sizeof(pfx_block) - 1;
  ngx_memcpy(w, block_str, block_len);
  w += block_len;

  ngx_memcpy(w, pfx_bypass, sizeof(pfx_bypass) - 1);
  w += sizeof(pfx_bypass) - 1;
  ngx_memcpy(w, bypass_str, bypass_len);
  w += bypass_len;

  if (has_timeout) {
    ngx_memcpy(w, pfx_timeout, sizeof(pfx_timeout) - 1);
    w += sizeof(pfx_timeout) - 1;
    mesi_json_write_uint(&w, lcf->timeout_seconds);
  }
  if (has_maxrs) {
    ngx_memcpy(w, pfx_maxrs, sizeof(pfx_maxrs) - 1);
    w += sizeof(pfx_maxrs) - 1;
    mesi_json_write_off(&w, lcf->max_response_size);
  }
  if (has_maxcr) {
    ngx_memcpy(w, pfx_maxcr, sizeof(pfx_maxcr) - 1);
    w += sizeof(pfx_maxcr) - 1;
    mesi_json_write_uint(&w, lcf->max_concurrent_requests);
  }
  if (has_tmpl) {
    ngx_memcpy(w, pfx_tmpl, sizeof(pfx_tmpl) - 1);
    w += sizeof(pfx_tmpl) - 1;
    mesi_json_append_str(&w, lcf->cache_key_template.data,
                         lcf->cache_key_template.len);
  }
  if (has_ctx) {
    size_t ctx_len = strlen(ctx_json);
    ngx_memcpy(w, pfx_ctx, sizeof(pfx_ctx) - 1);
    w += sizeof(pfx_ctx) - 1;
    ngx_memcpy(w, ctx_json, ctx_len);
    w += ctx_len;
  }

  *w++ = '}';
  *w = '\0';
  return buf;
}

// ngx_http_mesi_unicode_space returns the width in bytes of the UTF-8 rune
// starting at p when it is a Unicode whitespace rune that libgomesi's
// strings.Fields treats as a separator (Go's unicode.IsSpace), 0 otherwise.
// Covers every non-ASCII whitespace rune: U+0085, U+00A0, U+1680,
// U+2000..U+200A, U+2028, U+2029, U+202F, U+205F and U+3000. Any other
// byte — including truncated or invalid UTF-8, which Go decodes as U+FFFD
// (not a space) — forms a hostname token, so it is not whitespace here.
static size_t ngx_http_mesi_unicode_space(const u_char *p, size_t len) {
  if (len >= 2 && p[0] == 0xc2 && (p[1] == 0x85 || p[1] == 0xa0)) {
    return 2;  // U+0085 NEL, U+00A0 no-break space
  }
  if (len >= 3 && p[0] == 0xe1 && p[1] == 0x9a && p[2] == 0x80) {
    return 3;  // U+1680 ogham space mark
  }
  if (len >= 3 && p[0] == 0xe2 && p[1] == 0x80 &&
      ((p[2] >= 0x80 && p[2] <= 0x8a)  // U+2000..U+200A
       || p[2] == 0xa8                 // U+2028 line separator
       || p[2] == 0xa9                 // U+2029 paragraph separator
       || p[2] == 0xaf))               // U+202F narrow no-break space
  {
    return 3;
  }
  if (len >= 3 && p[0] == 0xe2 && p[1] == 0x81 && p[2] == 0x9f) {
    return 3;  // U+205F medium mathematical space
  }
  if (len >= 3 && p[0] == 0xe3 && p[1] == 0x80 && p[2] == 0x80) {
    return 3;  // U+3000 ideographic space
  }
  return 0;
}

static ngx_str_t parse(ngx_str_t input, ngx_http_request_t *r) {
  ngx_str_t output = {0, NULL};

  ngx_http_mesi_loc_conf_t *lcf =
      ngx_http_get_module_loc_conf(r, ngx_http_mesi_module);

  // Global ESI nesting depth (#180). The directive setter validated
  // [0, MESI_MAX_MAX_DEPTH] at config load and ngx_conf_merge_value
  // applied the default (5), so this guard is defense-in-depth: an
  // unvalidated value fails the request closed (the module's existing
  // fail-closed empty terminal response) with an ERR log instead of
  // being silently clamped or defaulted — never a silent substitution.
  ngx_int_t max_depth = lcf->max_depth;
  if (max_depth < 0 || max_depth > MESI_MAX_MAX_DEPTH) {
    ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                  "mesi: mesi_max_depth %d out of range [0, %d]; failing "
                  "request (fail closed)",
                  (int)max_depth, MESI_MAX_MAX_DEPTH);
    // Zero length with a non-NULL data pointer — same terminal-response
    // contract as the other fail-closed paths below (never a NULL-pos
    // zero-size buffer for the body writer).
    return (ngx_str_t){0, (u_char *)""};
  }

  // Global per-include fetch budget (#184). The directive setter
  // validated [1, MESI_MAX_TIMEOUT_SECONDS] at config load and
  // ngx_conf_merge_value only ever stores the unset sentinel or a
  // validated value, so this guard is defense-in-depth: an
  // unvalidated stored value fails the request closed (the module's
  // existing fail-closed empty terminal response) with an ERR log
  // instead of being silently clamped or defaulted — never a silent
  // substitution. Go-side ParseJson would reject such a value anyway
  // (warn + NULL); this keeps the failure local and logged with the
  // directive name even against a stale libgomesi without that symbol.
  if (lcf->timeout_seconds != NGX_CONF_UNSET &&
      (lcf->timeout_seconds < 1 ||
       lcf->timeout_seconds > MESI_MAX_TIMEOUT_SECONDS)) {
    ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                  "mesi: mesi_timeout %d out of range [1, %d]; failing "
                  "request (fail closed)",
                  (int)lcf->timeout_seconds, MESI_MAX_TIMEOUT_SECONDS);
    return (ngx_str_t){0, (u_char *)""};
  }

  // Per-include response body cap (#208). The directive setter
  // validated [0, MESI_MAX_MAX_RESPONSE_SIZE] at config load and
  // ngx_conf_merge_off_value only ever stores the unset sentinel or a
  // validated value, so this guard is defense-in-depth: an
  // unvalidated stored value fails the request closed (the module's
  // existing fail-closed empty terminal response) with an ERR log
  // instead of being silently clamped or defaulted — never a silent
  // substitution. Go-side ParseJson would reject such a value anyway
  // (warn + NULL); this keeps the failure local and logged with the
  // directive name even against a stale libgomesi without that symbol.
  if (lcf->max_response_size != NGX_CONF_UNSET &&
      (lcf->max_response_size < 0 ||
       lcf->max_response_size > MESI_MAX_MAX_RESPONSE_SIZE)) {
    ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                  "mesi: mesi_max_response_size %O out of range [0, %O]; "
                  "failing request (fail closed)",
                  lcf->max_response_size,
                  (off_t)MESI_MAX_MAX_RESPONSE_SIZE);
    return (ngx_str_t){0, (u_char *)""};
  }

  // Per-parse concurrent-fetch cap (#214). The directive setter
  // validated [0, MESI_MAX_MAX_CONCURRENT_REQUESTS] at config load and
  // ngx_conf_merge_value only ever stores the unset sentinel or a
  // validated value, so this guard is defense-in-depth: an
  // unvalidated stored value fails the request closed (the module's
  // existing fail-closed empty terminal response) with an ERR log
  // instead of being silently clamped or defaulted — never a silent
  // substitution. Go-side ParseJson would reject such a value anyway
  // (warn + NULL); this keeps the failure local and logged with the
  // directive name even against a stale libgomesi without that symbol.
  if (lcf->max_concurrent_requests != NGX_CONF_UNSET &&
      (lcf->max_concurrent_requests < 0 ||
       lcf->max_concurrent_requests > MESI_MAX_MAX_CONCURRENT_REQUESTS)) {
    ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                  "mesi: mesi_max_concurrent_requests %d out of range "
                  "[0, %d]; failing request (fail closed)",
                  (int)lcf->max_concurrent_requests,
                  MESI_MAX_MAX_CONCURRENT_REQUESTS);
    return (ngx_str_t){0, (u_char *)""};
  }

  if (lcf->cache_backend.len > 0 &&
      (!cache_initialized ||
       cache_last_backend.len != lcf->cache_backend.len ||
       ngx_strncmp(cache_last_backend.data, lcf->cache_backend.data, lcf->cache_backend.len) != 0)) {
    char *backend = ngx_str_to_cstr(&lcf->cache_backend, r->pool);
    if (strcmp(backend, "memcached") == 0) {
      // For memcached we need InitCacheWithConfig with a JSON config blob.
      if (!EsiInitCacheWithConfig) {
        ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                      "mesi: InitCacheWithConfig not available in libgomesi");
      } else {
        char *config_json = build_memcached_config_json(lcf, r->pool);
        if (config_json == NULL) {
          ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                        "mesi: failed to build memcached config JSON");
        } else {
          int rc = EsiInitCacheWithConfig(backend, lcf->cache_size, lcf->cache_ttl, config_json);
          if (rc < 0) {
            ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                          "mesi: InitCacheWithConfig failed for backend '%s' (returned %d)",
                          backend, rc);
          }
        }
      }
    } else if (strcmp(backend, "redis") == 0) {
      // For redis we need InitCacheWithConfig with a JSON config blob.
      if (!EsiInitCacheWithConfig) {
        ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                      "mesi: InitCacheWithConfig not available in libgomesi");
      } else {
        char *config_json = build_redis_config_json(lcf, r->pool);
        if (config_json == NULL) {
          ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                        "mesi: failed to build redis config JSON");
        } else {
          int rc = EsiInitCacheWithConfig(backend, lcf->cache_size, lcf->cache_ttl, config_json);
          if (rc < 0) {
            ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                          "mesi: InitCacheWithConfig failed for backend '%s' (returned %d)",
                          backend, rc);
          }
        }
      }
    } else if (EsiInitCache) {
      EsiInitCache(backend, lcf->cache_size, lcf->cache_ttl);
    }
    cache_initialized = 1;
    cache_last_backend = lcf->cache_backend;
  }

  ngx_str_t scheme = r->schema;
  ngx_str_t host = r->headers_in.host->value;
  size_t len = scheme.len + sizeof("://") - 1 + host.len + sizeof("/") - 1;

  // Relative <esi:include src="..."> paths resolve against this base URL,
  // which is built from the request's Host header. When mesi_allowed_hosts
  // is set, the resolved (expanded) host is subject to the whitelist in the
  // shared core — a relative include can only fetch a host the operator
  // explicitly allowed.
  ngx_str_t base_url;
  base_url.len = len;
  base_url.data = ngx_pnalloc(r->pool, len + 1);
  if (base_url.data == NULL) {
    return output;
  }

  ngx_snprintf(base_url.data, len + 1, "%V://%V/", &scheme, &host);

  char *input_cstr = ngx_str_to_cstr(&input, r->pool);
  char *base_url_cstr = ngx_str_to_cstr(&base_url, r->pool);

  // AllowedHosts is checked by hostname before any dial; BlockPrivateIPs
  // runs at dial time. Empty string = no hostname restriction.
  char *hosts_cstr = lcf->allowed_hosts.len > 0
                         ? ngx_str_to_cstr(&lcf->allowed_hosts, r->pool)
                         : "";

  char *message = NULL;

  // mesi_timeout / mesi_max_response_size / mesi_max_concurrent_requests
  // routing (#184/#208/#214): when any of the three directives is set,
  // the whole parse is routed through libgomesi's ParseJson entry
  // point (the #167 schema) so {"timeoutSeconds":N},
  // {"maxResponseSize":N} and/or {"maxConcurrentRequests":N} reach the
  // core — the same used_parse_json pattern Apache has used since
  // #167. The blob carries every other resolved setting (depth, base
  // URL, SSRF flags, optional cache key template + request context),
  // so behaviour matches the legacy positional path exactly except for
  // those values. When the symbol is missing (older libgomesi.so),
  // fall through to the legacy chain below with a logged per-request
  // warning naming each set directive — the directives are ignored
  // (30s / unlimited / unlimited apply), never a crash and
  // never a silently wrong config. used_parse_json distinguishes
  // "not attempted" from "ParseJson returned NULL" — a NULL must NOT
  // fall back silently; it fails the request closed below (config
  // errors are already logged Go-side).
  int configured = lcf->timeout_seconds != NGX_CONF_UNSET ||
                   lcf->max_response_size != NGX_CONF_UNSET ||
                   lcf->max_concurrent_requests != NGX_CONF_UNSET;
  int used_parse_json = 0;
  if (configured) {
    if (EsiParseJson != NULL) {
      used_parse_json = 1;
      // build_request_ctx_json returns "" when no template is
      // configured or the template uses no ${header:}/${cookie:}
      // placeholder — the same optimisation as the positional
      // ParseWithConfigCtx path below.
      char *ctx_json = build_request_ctx_json(r, &lcf->cache_key_template,
                                               r->pool);
      char *parse_cfg_json =
          build_parse_json_config(lcf, max_depth, base_url_cstr, hosts_cstr,
                                  ctx_json, r->pool);
      if (ctx_json == NULL || parse_cfg_json == NULL) {
        ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                      "mesi: failed to allocate ParseJson config; failing "
                      "request (fail closed)");
        return (ngx_str_t){0, (u_char *)""};
      }
      message = EsiParseJson(input_cstr, parse_cfg_json);
    } else {
      if (lcf->timeout_seconds != NGX_CONF_UNSET) {
        ngx_log_error(NGX_LOG_WARN, r->connection->log, 0,
                      "mesi: mesi_timeout is set but libgomesi lacks "
                      "ParseJson — mesi_timeout ignored (default %ds "
                      "timeout applies)",
                      MESI_DEFAULT_TIMEOUT_SECONDS);
      }
      if (lcf->max_response_size != NGX_CONF_UNSET) {
        ngx_log_error(NGX_LOG_WARN, r->connection->log, 0,
                      "mesi: mesi_max_response_size is set but libgomesi "
                      "lacks ParseJson — mesi_max_response_size ignored "
                      "(unlimited response size applies, the pre-#208 "
                      "behaviour)");
      }
      if (lcf->max_concurrent_requests != NGX_CONF_UNSET) {
        ngx_log_error(NGX_LOG_WARN, r->connection->log, 0,
                      "mesi: mesi_max_concurrent_requests is set but "
                      "libgomesi lacks ParseJson — "
                      "mesi_max_concurrent_requests ignored (unlimited "
                      "concurrent requests apply, the pre-#214 "
                      "behaviour)");
      }
    }
  }

  if (!used_parse_json &&
      lcf->cache_key_template.len > 0 && EsiParseWithConfigCtx != NULL) {
    // ParseWithConfigCtx extends ParseWithConfigEx with the
    // cacheKeyTemplate + requestCtxJSON parameters: the shared core
    // installs a CacheKeyFunc that evaluates the template via
    // mesi.BuildCacheKey (${url}, ${header:Name} case-insensitive,
    // ${cookie:Name} case-insensitive; unknown placeholders stay
    // literal). The template travels verbatim — nginx performs no
    // ${VAR} interpolation of directive arguments (unlike Apache's
    // ap_resolve_env, no $$ escaping is needed). The request context
    // JSON is only built when the template actually references a
    // header/cookie; otherwise "" reaches libgomesi, which then uses a
    // dummy request (identical key, zero per-request overhead).
    char *template_cstr = ngx_str_to_cstr(&lcf->cache_key_template, r->pool);
    char *ctx_json = build_request_ctx_json(r, &lcf->cache_key_template, r->pool);
    if (template_cstr == NULL || ctx_json == NULL) {
      ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                    "mesi: failed to allocate cache key template context");
      return (ngx_str_t){0, (u_char *)""};
    }
    message = EsiParseWithConfigCtx(input_cstr, (int)max_depth, base_url_cstr,
                                    hosts_cstr, lcf->block_private_ips,
                                    lcf->allow_private_ips_for_allowed,
                                    template_cstr, ctx_json);
  } else if (!used_parse_json && lcf->cache_key_template.len > 0) {
    // Fail loud, never silently wrong keys: with a stale libgomesi that
    // lacks ParseWithConfigCtx the template CANNOT be honoured, so warn
    // per request and fall back to the URL-only DefaultCacheKey path
    // below (the documented pre-template behaviour).
    ngx_log_error(NGX_LOG_WARN, r->connection->log, 0,
                  "mesi: mesi_cache_key_template is set but libgomesi "
                  "lacks ParseWithConfigCtx — cache key template ignored, "
                  "using URL-only cache keys");
  }

  if (!used_parse_json && message == NULL) {
    if (EsiParseWithConfigEx != NULL) {
      // ParseWithConfigEx extends ParseWithConfig with the
      // allowPrivateIPsForAllowedHosts parameter: hosts listed in
      // allowed_hosts may bypass the dial-time private-IP block (only
      // effective when block_private_ips is on AND allowed_hosts is
      // non-empty; the core grants the bypass per-host only for hosts
      // present in AllowedHosts).
      message = EsiParseWithConfigEx(input_cstr, (int)max_depth, base_url_cstr,
                                     hosts_cstr, lcf->block_private_ips,
                                     lcf->allow_private_ips_for_allowed);
    } else if (EsiParseWithConfig != NULL) {
      if (lcf->allow_private_ips_for_allowed) {
        ngx_log_error(NGX_LOG_WARN, r->connection->log, 0,
                      "mesi: mesi_allow_private_ips_for_allowed is On but "
                      "libgomesi lacks ParseWithConfigEx — private-IP bypass "
                      "for allowed hosts DISABLED, falling back to "
                      "ParseWithConfig");
      }
      // ParseWithConfig enables SSRF protection (blockPrivateIPs) and an
      // optional allowed-hosts whitelist (empty string = no restriction).
      message = EsiParseWithConfig(input_cstr, (int)max_depth, base_url_cstr,
                                   hosts_cstr, lcf->block_private_ips);
    } else if (lcf->allowed_hosts.len > 0) {
      // Defensive fail-closed fallback: the header phase already refused the
      // request with HTTP 500 before any body filter ctx was created, so this
      // path should never be reached. If it ever is, return a valid empty
      // terminal response: zero length with a non-NULL data pointer, so the
      // writer never receives the NULL-pos zero-size buffer that
      // ngx_null_string would yield.
      ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                    "mesi: ParseWithConfig unavailable in libgomesi — "
                    "mesi_allowed_hosts cannot be enforced; failing request "
                    "(fail closed)");
      return (ngx_str_t){0, (u_char *)""};
    } else {
      ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                    "mesi: ParseWithConfig unavailable in libgomesi — "
                    "SSRF protection DISABLED, falling back to Parse");
      message = EsiParse(input_cstr, (int)max_depth, base_url_cstr);
    }
  }

  if (message == NULL) {
    ngx_log_error(NGX_LOG_ERR, r->connection->log, 0,
                  "mesi: libgomesi Parse returned NULL; failing request "
                  "(fail closed)");
    return (ngx_str_t){0, (u_char *)""};
  }

  output.len = ngx_strlen(message);
  // +1 for the NUL terminator written below — the historical allocation
  // of exactly output.len bytes made output.data[output.len] = '\0' write
  // one byte past the pool block.
  output.data = ngx_palloc(r->pool, output.len + 1);
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

  EsiParseWithConfig = (ParseWithConfigFunc)dlsym(go_module, "ParseWithConfig");
  if (dlerror() != NULL) {
    EsiParseWithConfig = NULL;
    ngx_log_error(NGX_LOG_WARN, cycle->log, 0,
                  "mesi: ParseWithConfig not available in libgomesi — "
                  "mesi_block_private_ips will not be enforced and a "
                  "configured mesi_allowed_hosts will fail requests "
                  "(fail closed) instead of being silently ignored");
  }

  // ParseWithConfigEx is optional: it adds the allowPrivateIPsForAllowedHosts
  // parameter. When present, the module uses it so the
  // mesi_allow_private_ips_for_allowed directive takes effect. Older
  // libgomesi builds without it fall back to ParseWithConfig (bypass
  // disabled) — the directive is then a no-op with a logged warning.
  EsiParseWithConfigEx = (ParseWithConfigExFunc)dlsym(go_module, "ParseWithConfigEx");
  if (dlerror() != NULL) {
    EsiParseWithConfigEx = NULL;
  }

  // ParseWithConfigCtx is optional: it adds the cacheKeyTemplate +
  // requestCtxJSON parameters. When present, the module uses it so the
  // mesi_cache_key_template directive takes effect. Older libgomesi
  // builds without it fall back to ParseWithConfigEx/ParseWithConfig
  // (URL-only DefaultCacheKey) — the template is then ignored with a
  // per-request warning logged in parse() (never silently wrong keys).
  EsiParseWithConfigCtx = (ParseWithConfigCtxFunc)dlsym(go_module, "ParseWithConfigCtx");
  if (dlerror() != NULL) {
    EsiParseWithConfigCtx = NULL;
  }

  // ParseJson is optional: the JSON config entry point that carries
  // timeoutSeconds, maxResponseSize and maxConcurrentRequests (the
  // #167 schema, routed when mesi_timeout, mesi_max_response_size or
  // mesi_max_concurrent_requests is set, #184/#208/#214). Older
  // libgomesi.so builds without it keep working: the directives then
  // degrade to a per-request warning in parse() and the defaults
  // apply (30s / unlimited / unlimited) — never a crash, never a
  // link-time hard dependency (same pattern as ParseWithConfigEx /
  // ParseWithConfigCtx above).
  EsiParseJson = (ParseJsonFunc)dlsym(go_module, "ParseJson");
  if (dlerror() != NULL) {
    EsiParseJson = NULL;
  }

  EsiInitCache = (InitCacheFunc)dlsym(go_module, "InitCache");
  if (dlerror() != NULL) {
    EsiInitCache = NULL;
  }

  EsiInitCacheWithConfig = (InitCacheWithConfigFunc)dlsym(go_module, "InitCacheWithConfig");
  if (dlerror() != NULL) {
    EsiInitCacheWithConfig = NULL;
  }

  EsiFreeCache = (FreeCacheFunc)dlsym(go_module, "FreeCache");
  if (dlerror() != NULL) {
    EsiFreeCache = NULL;
  }

  return NGX_OK;
}

static void ngx_http_mesi_thread_exit(ngx_cycle_t *cycle) {
  if (EsiFreeCache) {
    EsiFreeCache();
  }
  if (go_module) {
    dlclose(go_module);
    go_module = NULL;
    cache_initialized = 0;
    cache_last_backend.len = 0;
    cache_last_backend.data = NULL;
  }
}

// ngx_http_mesi_set_max_depth parses the `mesi_max_depth` directive
// argument (#180). Deliberately NOT ngx_conf_set_num_slot: the stock
// slot setter only runs ngx_atoi — digits-only, but with no upper bound,
// so `mesi_max_depth 10001` would pass `nginx -t` and reach libgomesi
// (which rejects it at request time since #414, far from where the
// operator looks). This setter mirrors Apache's parse_nonneg_int
// (mod_mesi.c):
// non-empty, digits only (rejects "-1", "+1", "3.5", "abc", "3foo"),
// and the accumulated value must stay within [0, MESI_MAX_MAX_DEPTH]
// (early exit bounds the accumulator, so it can never overflow
// ngx_int_t). Explicit 0 is valid passthrough (no ESI fetch) — same
// contract as Apache MesiMaxDepth / Caddy max_depth / CLI -max-depth.
// Every rejection fails config load with an ERR-level (EMERG) log that
// names the directive and the offending value — never a silent default.
static char *ngx_http_mesi_set_max_depth(ngx_conf_t *cf, ngx_command_t *cmd,
                                         void *conf) {
  ngx_http_mesi_loc_conf_t *lcf = conf;
  ngx_str_t *value = cf->args->elts;  // value[0] = directive name (TAKE1)
  ngx_int_t val = 0;
  size_t i;

  (void)cmd;  // offset is informational; the setter writes lcf directly.

  // Reject a repeated directive in the same scope, matching the
  // "is duplicate" behaviour of the ngx_conf_set_*_slot setters used
  // by every other directive in this module — a silent last-wins would
  // substitute the operator's intent without a word.
  if (lcf->max_depth != NGX_CONF_UNSET) {
    return "is duplicate";
  }

  // NGX_CONF_TAKE1 guarantees one argument, but an empty quoted string
  // ("") is still a zero-length token — reject it instead of letting
  // the loop below parse "" as a silent passthrough 0.
  if (value[1].len == 0) {
    ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                       "\"mesi_max_depth\" directive requires an argument "
                       "(a non-negative integer in [0, %d])",
                       MESI_MAX_MAX_DEPTH);
    return NGX_CONF_ERROR;
  }

  for (i = 0; i < value[1].len; i++) {
    u_char c = value[1].data[i];
    if (c < '0' || c > '9') {
      // atoi would silently coerce "-1" (wrap when cast to uint),
      // "3.5" (truncate) and "abc" (→ 0 = passthrough). Fail fast.
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "invalid value \"%V\" in \"mesi_max_depth\" "
                         "directive: must be a non-negative integer "
                         "(digits only) in [0, %d]",
                         &value[1], MESI_MAX_MAX_DEPTH);
      return NGX_CONF_ERROR;
    }
    val = val * 10 + (c - '0');
    if (val > MESI_MAX_MAX_DEPTH) {
      // Early exit: the accumulator can never grow past
      // MESI_MAX_MAX_DEPTH * 10 + 9, so this cannot overflow ngx_int_t
      // regardless of argument length.
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "value \"%V\" out of range in \"mesi_max_depth\" "
                         "directive: must be in [0, %d]",
                         &value[1], MESI_MAX_MAX_DEPTH);
      return NGX_CONF_ERROR;
    }
  }

  lcf->max_depth = val;
  return NGX_CONF_OK;
}

// ngx_http_mesi_set_timeout parses the `mesi_timeout` directive
// argument (#184). Deliberately NOT ngx_conf_set_num_slot: the stock
// slot setter only runs ngx_atoi — digits-only but unbounded on both
// ends, so `mesi_timeout 0` would pass `nginx -t` and make EVERY
// include fail immediately with ErrTimeBudgetExceeded (mesi/fetch.go)
// instead of meaning "no timeout". This setter mirrors Apache's
// parse_nonneg_int with min=1 (mod_mesi.c set_timeout) and the
// mesi_max_depth setter above: non-empty, digits only (rejects "-1",
// "+1", "1.5", "abc", "3foo"), and the accumulated value must stay
// within [1, MESI_MAX_TIMEOUT_SECONDS] (early exit bounds the
// accumulator, so it can never overflow ngx_int_t). The range matches
// libgomesi's config.ValidateTimeout, so nginx and the Go side can
// never disagree. Every rejection fails config load with an
// ERR-level (EMERG) log that names the directive and the offending
// value — never a silent default.
static char *ngx_http_mesi_set_timeout(ngx_conf_t *cf, ngx_command_t *cmd,
                                       void *conf) {
  ngx_http_mesi_loc_conf_t *lcf = conf;
  ngx_str_t *value = cf->args->elts;  // value[0] = directive name (TAKE1)
  ngx_int_t val = 0;
  size_t i;

  (void)cmd;  // offset is informational; the setter writes lcf directly.

  // Reject a repeated directive in the same scope, matching the
  // "is duplicate" behaviour of the ngx_conf_set_*_slot setters used
  // by every other directive in this module — a silent last-wins would
  // substitute the operator's intent without a word.
  if (lcf->timeout_seconds != NGX_CONF_UNSET) {
    return "is duplicate";
  }

  // NGX_CONF_TAKE1 guarantees one argument, but an empty quoted string
  // ("") is still a zero-length token — reject it instead of letting
  // the loop below parse "" as a silent 0 (which would then be range-
  // rejected with a misleading message).
  if (value[1].len == 0) {
    ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                       "\"mesi_timeout\" directive requires an argument "
                       "(an integer in [1, %d])",
                       MESI_MAX_TIMEOUT_SECONDS);
    return NGX_CONF_ERROR;
  }

  for (i = 0; i < value[1].len; i++) {
    u_char c = value[1].data[i];
    if (c < '0' || c > '9') {
      // atoi would silently coerce "abc" (→ 0 = every include fails),
      // "-1" and "2.5" (→ 2). Fail fast.
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "invalid value \"%V\" in \"mesi_timeout\" "
                         "directive: must be a positive integer "
                         "(digits only) in [1, %d]",
                         &value[1], MESI_MAX_TIMEOUT_SECONDS);
      return NGX_CONF_ERROR;
    }
    val = val * 10 + (c - '0');
    if (val > MESI_MAX_TIMEOUT_SECONDS) {
      // Early exit: the accumulator can never grow past
      // MESI_MAX_TIMEOUT_SECONDS * 10 + 9, so this cannot overflow
      // ngx_int_t regardless of argument length.
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "value \"%V\" out of range in \"mesi_timeout\" "
                         "directive: must be in [1, %d]",
                         &value[1], MESI_MAX_TIMEOUT_SECONDS);
      return NGX_CONF_ERROR;
    }
  }

  // Explicit 0 ("0", "00", …): digits-only passed, but the landed
  // #167 contract rejects 0 — it is NOT "no timeout" (see the setter
  // comment above).
  if (val < 1) {
    ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                       "value \"%V\" out of range in \"mesi_timeout\" "
                       "directive: must be in [1, %d]",
                       &value[1], MESI_MAX_TIMEOUT_SECONDS);
    return NGX_CONF_ERROR;
  }

  lcf->timeout_seconds = val;
  return NGX_CONF_OK;
}

// ngx_http_mesi_set_max_response_size parses the
// `mesi_max_response_size` directive argument (#208). Deliberately
// NOT ngx_conf_set_size_slot (whose ngx_parse_size grammar accepts
// k/m/g suffixes no other platform's grammar accepts) and NOT
// ngx_conf_set_off_slot (whose ngx_atoi/ngx_parse_off parse has no
// upper bound). Mirrors Apache's parse_nonneg_off (mod_mesi.c) and
// this module's mesi_max_depth / mesi_timeout setters: non-empty,
// digits only (rejects "-1", "+1", "1.5", "abc", "3foo", "1m"),
// plain integer BYTES with a per-digit overflow guard against the cap
// BEFORE the multiply (val > (max - d) / 10 means val*10 + d would
// exceed max), so no intermediate ever wraps off_t and the full
// 19-digit range below the cap parses. Explicit 0 IS accepted — the
// documented "unlimited" value (the core only limits when
// MaxResponseSize > 0, mesi/fetch.go:288), unlike mesi_timeout 0.
// Negatives are rejected (the core's > 0 check would silently treat
// them like 0 = unlimited). The range matches libgomesi's
// config.MaxMaxResponseSize, so nginx and the Go side can never
// disagree. Every rejection fails config load with an ERR-level
// (EMERG) log that names the directive and the offending value —
// never a silent default.
static char *ngx_http_mesi_set_max_response_size(ngx_conf_t *cf,
                                                 ngx_command_t *cmd,
                                                 void *conf) {
  ngx_http_mesi_loc_conf_t *lcf = conf;
  ngx_str_t *value = cf->args->elts;  // value[0] = directive name (TAKE1)
  off_t val = 0;
  size_t i;

  (void)cmd;  // offset is informational; the setter writes lcf directly.

  // Reject a repeated directive in the same scope, matching the
  // "is duplicate" behaviour of the ngx_conf_set_*_slot setters used
  // by every other directive in this module — a silent last-wins would
  // substitute the operator's intent without a word.
  if (lcf->max_response_size != NGX_CONF_UNSET) {
    return "is duplicate";
  }

  // NGX_CONF_TAKE1 guarantees one argument, but an empty quoted string
  // ("") is still a zero-length token — reject it instead of letting
  // the loop below parse "" as a silent 0 (= unlimited).
  if (value[1].len == 0) {
    ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                       "\"mesi_max_response_size\" directive requires an "
                       "argument (a non-negative integer in bytes, in "
                       "[0, %O])",
                       (off_t)MESI_MAX_MAX_RESPONSE_SIZE);
    return NGX_CONF_ERROR;
  }

  for (i = 0; i < value[1].len; i++) {
    u_char c = value[1].data[i];
    off_t d;
    if (c < '0' || c > '9') {
      // atoi/ngx_parse_size-style coercion would silently accept
      // "-1" (unlimited), "1.5" (truncate), "abc" (→ 0 = unlimited),
      // "100abc" (trailing garbage) and size suffixes ("10m").
      // Cross-platform contract: plain integer bytes, digits only.
      // Fail fast.
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "invalid value \"%V\" in "
                         "\"mesi_max_response_size\" directive: must be "
                         "a non-negative integer (digits only, bytes) in "
                         "[0, %O]",
                         &value[1], (off_t)MESI_MAX_MAX_RESPONSE_SIZE);
      return NGX_CONF_ERROR;
    }
    d = (off_t)(c - '0');
    if (val > (MESI_MAX_MAX_RESPONSE_SIZE - d) / 10) {
      // Per-digit overflow guard against the cap BEFORE the multiply:
      // no intermediate can ever wrap off_t regardless of argument
      // length, and MaxInt64 (= cap + 1) / oversized digit strings
      // are rejected instead of wrapping the core's +1 LimitReader
      // bound negative (which would silently render an empty body).
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "value \"%V\" out of range in "
                         "\"mesi_max_response_size\" directive: must be "
                         "in [0, %O]",
                         &value[1], (off_t)MESI_MAX_MAX_RESPONSE_SIZE);
      return NGX_CONF_ERROR;
    }
    val = val * 10 + d;
  }

  // Explicit 0 ("0", "00", …): digits-only passed and 0 is the
  // documented "unlimited" value — deliberately ACCEPTED (unlike
  // mesi_timeout 0). Negatives can never reach here (digits only).

  lcf->max_response_size = val;
  return NGX_CONF_OK;
}

// ngx_http_mesi_set_max_concurrent_requests parses the
// `mesi_max_concurrent_requests` directive argument (#214). Deliberately
// NOT ngx_conf_set_num_slot (the issue's sketch): the stock slot
// setter only runs ngx_atoi — digits-only, but with no upper bound,
// so `mesi_max_concurrent_requests 1000000000` would pass `nginx -t`
// and reach libgomesi, diverging from Apache's parse_nonneg_int /
// php-ext IS_LONG / CLI int64, which all cap at 999999999. This
// setter mirrors those and this module's mesi_max_depth /
// mesi_timeout / mesi_max_response_size setters: non-empty, digits
// only (rejects "-1", "+1", "3.5", "abc", "3foo"), and the
// accumulated value must stay within [0,
// MESI_MAX_MAX_CONCURRENT_REQUESTS] with a per-digit overflow guard
// BEFORE the multiply, so no intermediate ever wraps ngx_int_t
// regardless of argument length. Explicit 0 IS accepted — the
// documented "unlimited" value (the core only installs the
// admission-control semaphore when MaxConcurrentRequests > 0,
// mesi/parser.go) — unlike mesi_timeout 0. Negatives are rejected
// (the core would only warn "max_concurrent_requests_invalid" and
// normalize them to 0 = unlimited, #329 — a malformed explicit value
// must never pass as the documented one). Unset (-1 sentinel) keeps
// the legacy parse path, where libgomesi leaves the field at 0 —
// byte-identical to pre-#214 nginx behaviour. Every rejection fails
// config load with an ERR-level (EMERG) log that names the directive
// and the offending value — never a silent default.
static char *ngx_http_mesi_set_max_concurrent_requests(ngx_conf_t *cf,
                                                       ngx_command_t *cmd,
                                                       void *conf) {
  ngx_http_mesi_loc_conf_t *lcf = conf;
  ngx_str_t *value = cf->args->elts;  // value[0] = directive name (TAKE1)
  ngx_int_t val = 0;
  size_t i;

  (void)cmd;  // offset is informational; the setter writes lcf directly.

  // Reject a repeated directive in the same scope, matching the
  // "is duplicate" behaviour of the ngx_conf_set_*_slot setters used
  // by every other directive in this module — a silent last-wins would
  // substitute the operator's intent without a word.
  if (lcf->max_concurrent_requests != NGX_CONF_UNSET) {
    return "is duplicate";
  }

  // NGX_CONF_TAKE1 guarantees one argument, but an empty quoted string
  // ("") is still a zero-length token — reject it instead of letting
  // the loop below parse "" as a silent 0 (= unlimited).
  if (value[1].len == 0) {
    ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                       "\"mesi_max_concurrent_requests\" directive requires "
                       "an argument (a non-negative integer in [0, %d])",
                       MESI_MAX_MAX_CONCURRENT_REQUESTS);
    return NGX_CONF_ERROR;
  }

  for (i = 0; i < value[1].len; i++) {
    u_char c = value[1].data[i];
    ngx_int_t d;
    if (c < '0' || c > '9') {
      // atoi/ngx_atoi-style coercion would silently accept "-1"
      // (wrap when cast, or core warn+normalize to unlimited),
      // "3.5" (truncate), "abc" (→ 0 = unlimited) and trailing
      // garbage ("3foo" → 3). Cross-platform contract: plain integer,
      // digits only. Fail fast.
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "invalid value \"%V\" in "
                         "\"mesi_max_concurrent_requests\" directive: must "
                         "be a non-negative integer (digits only) in "
                         "[0, %d]",
                         &value[1], MESI_MAX_MAX_CONCURRENT_REQUESTS);
      return NGX_CONF_ERROR;
    }
    d = (ngx_int_t)(c - '0');
    if (val > (MESI_MAX_MAX_CONCURRENT_REQUESTS - d) / 10) {
      // Per-digit overflow guard against the cap BEFORE the multiply:
      // no intermediate can ever wrap ngx_int_t regardless of argument
      // length, and cap+1 (1000000000) / oversized digit strings are
      // rejected instead of reaching libgomesi out of range.
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "value \"%V\" out of range in "
                         "\"mesi_max_concurrent_requests\" directive: must "
                         "be in [0, %d]",
                         &value[1], MESI_MAX_MAX_CONCURRENT_REQUESTS);
      return NGX_CONF_ERROR;
    }
    val = val * 10 + d;
  }

  // Explicit 0 ("0", "00", …): digits-only passed and 0 is the
  // documented "unlimited" value — deliberately ACCEPTED (unlike
  // mesi_timeout 0). Negatives can never reach here (digits only).

  lcf->max_concurrent_requests = val;
  return NGX_CONF_OK;
}

static void *ngx_http_mesi_create_loc_conf(ngx_conf_t *cf) {
  ngx_http_mesi_loc_conf_t *conf;
  conf = ngx_pcalloc(cf->pool, sizeof(ngx_http_mesi_loc_conf_t));
  if (conf == NULL) {
    return NULL;
  }
  conf->enable_mesi = NGX_CONF_UNSET;
  conf->max_depth = NGX_CONF_UNSET;
  conf->timeout_seconds = NGX_CONF_UNSET;
  // ngx_pcalloc zeroes the struct — without re-installing the
  // sentinel here, 0 ("unlimited") would masquerade as a configured
  // value and route every parse through ParseJson (#208, #214).
  conf->max_response_size = NGX_CONF_UNSET;
  conf->max_concurrent_requests = NGX_CONF_UNSET;
  conf->cache_size = NGX_CONF_UNSET;
  conf->cache_ttl = NGX_CONF_UNSET;
  conf->cache_redis_db = NGX_CONF_UNSET;
  conf->block_private_ips = NGX_CONF_UNSET;
  conf->allow_private_ips_for_allowed = NGX_CONF_UNSET;
  return conf;
}

static char *ngx_http_mesi_merge_loc_conf(ngx_conf_t *cf, void *parent,
                                           void *child) {
  ngx_http_mesi_loc_conf_t *prev = parent;
  ngx_http_mesi_loc_conf_t *conf = child;
  ngx_conf_merge_value(conf->enable_mesi, prev->enable_mesi, 0);
  // Unset → 5 (backward compatible with the previously hardcoded
  // literal). An explicit 0 survives the merge untouched — passthrough,
  // matching Apache MesiMaxDepth / Caddy max_depth / CLI -max-depth.
  // A child location that sets the directive overrides its parent's;
  // an unset child inherits the parent's value.
  ngx_conf_merge_value(conf->max_depth, prev->max_depth,
                       MESI_DEFAULT_MAX_DEPTH);
  // Unset STAYS NGX_CONF_UNSET (-1) — deliberately NOT a merged 30:
  // the sentinel is what distinguishes "mesi_timeout configured" (the
  // parse routes through ParseJson, #184) from "unset" (the
  // byte-identical legacy positional path, where libgomesi's
  // historical 30s default is applied Go-side). Merging to a literal
  // 30 — as the issue sketched — would route every parse through
  // ParseJson and log the stale-libgomesi warning even when the
  // operator never set the directive. A child location that sets the
  // directive overrides its parent's; an unset child inherits the
  // parent's value (same -1-sentinel rule as Apache's MesiTimeout,
  // mod_mesi.c merge_mesi_config).
  ngx_conf_merge_value(conf->timeout_seconds, prev->timeout_seconds,
                       NGX_CONF_UNSET);
  // Unset STAYS NGX_CONF_UNSET (-1) — deliberately NOT a merged 0
  // and NOT a merged 10 MB: the sentinel is what distinguishes
  // "mesi_max_response_size configured" (the parse routes through
  // ParseJson, #208) from "unset" (the byte-identical legacy
  // positional path, where libgomesi leaves the field at 0 =
  // unlimited). And because 0 IS a storable value ("unlimited"), the
  // sentinel being -1 (not 0) is what lets a child location's
  // explicit `mesi_max_response_size 0` override a parent's limit
  // while an unset child inherits it (same -1-sentinel rule as
  // Apache's MesiMaxResponseSize, mod_mesi.c merge_mesi_config, and
  // this module's mesi_timeout). NOTE: nginx defines no
  // NGX_CONF_UNSET_OFF macro (only UNSET/UNSET_UINT/UNSET_PTR/
  // UNSET_SIZE/UNSET_MSEC) — off_t fields use plain NGX_CONF_UNSET
  // (-1), which is exactly what ngx_conf_merge_off_value compares
  // against; ngx_conf_set_off_slot does the same.
  ngx_conf_merge_off_value(conf->max_response_size,
                           prev->max_response_size, NGX_CONF_UNSET);
  // Unset STAYS NGX_CONF_UNSET (-1) — deliberately NOT a merged 0:
  // the sentinel is what distinguishes "mesi_max_concurrent_requests
  // configured" (the parse routes through ParseJson, #214) from
  // "unset" (the byte-identical legacy positional path, where
  // libgomesi leaves the field at 0 = unlimited). And because 0 IS a
  // storable value ("unlimited"), the sentinel being -1 (not 0) is
  // what lets a child location's explicit
  // `mesi_max_concurrent_requests 0` override a parent's cap while an
  // unset child inherits it (same -1-sentinel rule as Apache's
  // MesiMaxConcurrentRequests, mod_mesi.c merge_server_config, and
  // this module's mesi_timeout / mesi_max_response_size).
  ngx_conf_merge_value(conf->max_concurrent_requests,
                       prev->max_concurrent_requests, NGX_CONF_UNSET);
  ngx_conf_merge_str_value(conf->cache_backend, prev->cache_backend, "");
  ngx_conf_merge_value(conf->cache_size, prev->cache_size, 10000);
  ngx_conf_merge_value(conf->cache_ttl, prev->cache_ttl, 30);
  ngx_conf_merge_str_value(conf->cache_memcached_servers, prev->cache_memcached_servers, "");
  ngx_conf_merge_str_value(conf->cache_redis_addr, prev->cache_redis_addr, "");
  ngx_conf_merge_str_value(conf->cache_redis_password, prev->cache_redis_password, "");
  ngx_conf_merge_value(conf->cache_redis_db, prev->cache_redis_db, 0);
  // Default ON: nginx previously had no SSRF protection (implicit off).
  // Enabling by default is a BREAKING CHANGE — operators with intentional
  // private-IP includes must set `mesi_block_private_ips off;`.
  ngx_conf_merge_value(conf->block_private_ips, prev->block_private_ips, 1);
  // Empty (unset) = no hostname restriction (backward compatible). A child
  // that sets its own hosts always overrides the parent's list.
  ngx_conf_merge_str_value(conf->allowed_hosts, prev->allowed_hosts, "");
  // Default OFF: private IPs always blocked regardless of allowed_hosts
  // membership unless the operator explicitly opts into the bypass (the
  // same secure default as Apache's MesiAllowPrivateIPsForAllowedHosts).
  ngx_conf_merge_value(conf->allow_private_ips_for_allowed,
                       prev->allow_private_ips_for_allowed, 0);
  // Empty (unset) = URL-only DefaultCacheKey (backward compatible); an
  // explicitly empty value keeps the same meaning.
  ngx_conf_merge_str_value(conf->cache_key_template, prev->cache_key_template, "");
  if (conf->cache_key_template.len > 0) {
    // Mirror the Apache MesiCacheKeyTemplate validator: cap the length
    // (unbounded config values must not drive unbounded allocations —
    // the template is copied into a per-request C string) and reject
    // control characters and DEL, which would end up verbatim in cache
    // keys and logs. Spaces are allowed (a template may legitimately
    // use them as separators); '"' and '\' are allowed (the template
    // travels as a plain C string — only the request-context JSON is
    // escaped, never the template). nginx performs no ${VAR}
    // interpolation of directive arguments, so no Apache-style "$$"
    // escaping or mangled-template ("::"/trailing ":") checks apply —
    // the value is taken verbatim.
    if (conf->cache_key_template.len > MESI_MAX_CACHE_KEY_TEMPLATE) {
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "\"mesi_cache_key_template\" exceeds maximum length "
                         "%d (got %d)",
                         MESI_MAX_CACHE_KEY_TEMPLATE,
                         (int)conf->cache_key_template.len);
      return NGX_CONF_ERROR;
    }
    for (size_t ti = 0; ti < conf->cache_key_template.len; ti++) {
      u_char tc = conf->cache_key_template.data[ti];
      if (tc < 0x20) {
        ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                           "\"mesi_cache_key_template\" must not contain "
                           "control characters (found byte %d)", (int)tc);
        return NGX_CONF_ERROR;
      }
      if (tc == 0x7f) {
        ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                           "\"mesi_cache_key_template\" must not contain the "
                           "DEL character");
        return NGX_CONF_ERROR;
      }
    }
  }
  if (conf->allowed_hosts.len > 0) {
    // Reject whitespace-only allowlists: they would silently disable the
    // hostname restriction the operator intended to configure. libgomesi
    // splits the value with Go's strings.Fields, so the check mirrors that
    // tokenization: every byte of the ASCII whitespace set (space, tab, CR,
    // LF, VT, FF) plus every rune of the Unicode whitespace set (U+0085,
    // U+00A0, U+1680, U+2000..U+200A, U+2028, U+2029, U+202F, U+205F,
    // U+3000 — e.g. a no-break space U+00A0 encoded as bytes c2 a0) counts
    // as a separator, and a value with no hostname token at all is rejected.
    // Any other byte, including invalid UTF-8, forms a token exactly like
    // strings.Fields.
    size_t i = 0;
    while (i < conf->allowed_hosts.len) {
      u_char c = conf->allowed_hosts.data[i];
      size_t ws_width;
      if (c == ' ' || c == '\t' || c == '\r' || c == '\n' ||
          c == '\v' || c == '\f') {
        i++;
        continue;
      }
      ws_width = ngx_http_mesi_unicode_space(&conf->allowed_hosts.data[i],
                                             conf->allowed_hosts.len - i);
      if (ws_width == 0) {
        break;  // a non-whitespace rune: the value has a hostname token
      }
      i += ws_width;
    }
    if (i == conf->allowed_hosts.len) {
      ngx_conf_log_error(NGX_LOG_EMERG, cf, 0,
                         "\"mesi_allowed_hosts\" must contain at least "
                         "one hostname");
      return NGX_CONF_ERROR;
    }
  }
  return NGX_CONF_OK;
}
