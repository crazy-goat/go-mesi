/*
 * Unit tests for Apache module directive parsing
 * Compile: gcc -o test_directives test_directives.c -I/usr/include/apr-1.0 -lapr-1 -laprutil-1
 * Run: ./test_directives
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <assert.h>
#include <apr_general.h>
#include <apr_pools.h>
#include <apr_tables.h>
#include <apr_strings.h>

static apr_pool_t *pool;
static int tests_passed = 0;
static int tests_failed = 0;

#define TEST(name) static void test_##name()
#define RUN_TEST(name) do { \
    printf("  Testing %s... ", #name); \
    test_##name(); \
    printf("PASS\n"); \
    tests_passed++; \
} while(0)

#define ASSERT_EQ(a, b) assert((a) == (b))
#define ASSERT_STR_EQ(a, b) assert(strcmp((a), (b)) == 0)
#define ASSERT_STR_CONTAINS(haystack, needle) assert(strstr((haystack), (needle)) != NULL)
#define ASSERT_NOT_NULL(x) assert((x) != NULL)
#define ASSERT_NULL(x) assert((x) == NULL)

/* Mock types from mod_mesi.c */
typedef struct {
    int enable_mesi;
    apr_array_header_t *allowed_hosts;
    int block_private_ips;
    int allow_private_ips_for_allowed;
    int shared_http_client;
    /* Cache backend (parity with mod_mesi.c mesi_config) */
    const char *cache_backend;
    int cache_size;
    int cache_ttl;
    /* Redis backend fields (#175) */
    const char *cache_redis_addr;
    const char *cache_redis_password;
    int cache_redis_db;
    /* Memcached backend fields (#176) */
    apr_array_header_t *cache_memcached_servers;
    /* Cache key template (#177) */
    const char *cache_key_template;
    /* ESI nesting depth (#166). -1 = unset (filter uses 5). */
    int max_depth;
    /* Global per-include fetch timeout in seconds (#167).
     * -1 = unset (filter uses 30). 0 can never be stored — rejected. */
    int timeout_seconds;
    /* Per-include response body cap in bytes (#169).
     * -1 = unset (libgomesi leaves 0 = unlimited). 0 IS a storable
     * configured value — "unlimited" — so the sentinel must stay -1. */
    apr_off_t max_response_size;
    /* Concurrent-fetch cap per page render (#170).
     * -1 = unset (libgomesi leaves 0 = unlimited). 0 IS a storable
     * configured value — "unlimited" — so the sentinel must stay -1. */
    int max_concurrent_requests;
} mesi_config;

/* Sentinel/constant values copied from mod_mesi.c */
#define MESI_DEFAULT_CACHE_SIZE 10000
#define MESI_MAX_CACHE_SIZE 1000000
#define MESI_MAX_CACHE_TTL_SECONDS (24 * 60 * 60)
#define MESI_MAX_REDIS_DB 15
// Cap on Memcached server entries — mirrors MESI_MAX_MEMCACHED_SERVERS.
#define MESI_MAX_MEMCACHED_SERVERS 64
#define MESI_MAX_CACHE_KEY_TEMPLATE 4096
#define MESI_MAX_MAX_DEPTH 10000
#define MESI_DEFAULT_MAX_DEPTH 5
/* Global ESI timeout bounds (#167) — mirrors mod_mesi.c; keep in sync
 * with libgomesi config.MaxTimeoutSeconds / DefaultTimeoutSeconds. */
#define MESI_MAX_TIMEOUT_SECONDS (24 * 60 * 60)
#define MESI_DEFAULT_TIMEOUT_SECONDS 30
/* MesiMaxResponseSize cap (#169) — mirrors mod_mesi.c; keep in sync
 * with libgomesi config.MaxMaxResponseSize (math.MaxInt64 - 1): the
 * core computes MaxResponseSize+1 for its io.LimitReader bound
 * (mesi/fetch.go), which would wrap negative at math.MaxInt64. */
#define MESI_MAX_MAX_RESPONSE_SIZE ((apr_off_t)9223372036854775806LL)
/* Concurrent-fetch cap (#170) — mirrors mod_mesi.c; keep in sync
 * with libgomesi config.MaxMaxConcurrentRequests. Neither core nor
 * Caddy caps the value; the bound is parse_nonneg_int's 9-digit
 * int32-safe guard, the largest value the C `int` field and the
 * helper can both represent without a wrap. */
#define MESI_MAX_MAX_CONCURRENT_REQUESTS 999999999

/* Directive parsing functions (copied from mod_mesi.c for testing) */
static const char *parse_allowed_hosts(mesi_config *conf, const char *arg) {
    const char *host;
    while (*arg) {
        while (*arg && (*arg == ' ' || *arg == '\t')) arg++;
        host = arg;
        while (*arg && *arg != ' ' && *arg != '\t') arg++;
        if (host != arg) {
            const char **new_host = apr_array_push(conf->allowed_hosts);
            *new_host = apr_pstrndup(pool, host, arg - host);
        }
    }
    return NULL;
}

static const char *parse_block_private_ips(mesi_config *conf, int flag) {
    conf->block_private_ips = flag;
    return NULL;
}

static const char *parse_allow_private_for_allowed(mesi_config *conf, int flag) {
    conf->allow_private_ips_for_allowed = flag;
    return NULL;
}

static const char *parse_shared_http_client(mesi_config *conf, int flag) {
    conf->shared_http_client = flag;
    return NULL;
}

/* Parse helper copied verbatim from mod_mesi.c — verifies the
 * validator rejects malformed integers silently rather than falling
 * back to a default.
 * Parses an NUL-terminated integer (TAKE1 directive shapes).
 * `directive` is the Apache directive name used in every error string. */
static const char *parse_nonneg_int(apr_pool_t *pool_arg, const char *arg,
                                    const char *directive,
                                    int min, int max, int *out) {
    const char *p = arg ? arg : "";
    while (*p == ' ' || *p == '\t') p++;
    if (*p == '\0') {
        return apr_psprintf(pool_arg,
            "%s requires a non-negative integer argument", directive);
    }
    const char *digits = p;
    while (*p >= '0' && *p <= '9') p++;
    if (*p != '\0') {
        return apr_psprintf(pool_arg,
            "%s must be a non-negative integer (got: %s)", directive, arg);
    }
    if (digits == p) {
        return apr_psprintf(pool_arg,
            "%s must contain at least one digit (got: %s)", directive, arg);
    }
    size_t n = (size_t)(p - digits);
    if (n > 9) {
        return apr_psprintf(pool_arg,
            "%s value %s exceeds maximum allowed (%d)", directive, arg, max);
    }
    long val = 0;
    for (size_t i = 0; i < n; i++) {
        val = val * 10 + (digits[i] - '0');
    }
    if (val < min || val > max) {
        return apr_psprintf(pool_arg,
            "%s value %s out of range [%d, %d]", directive, arg, min, max);
    }
    *out = (int)val;
    return NULL;
}

/* Bounded variant — copied verbatim from mod_mesi.c. Used for parsing
 * a port within a multi-token line (e.g. host:port inside a RAW_ARGS
 * Memcached server list), where the standard parse_nonneg_int would
 * mistakenly consume digits past the colon. */
static const char *parse_nonneg_int_bounded(apr_pool_t *pool_arg,
                                            const char *arg, const char *end,
                                            const char *directive,
                                            int min, int max, int *out) {
    if (!arg || !end || arg >= end) {
        return apr_psprintf(pool_arg,
            "%s requires a non-negative integer argument", directive);
    }
    const char *p = arg;
    while (p < end && (*p == ' ' || *p == '\t')) p++;
    if (p >= end) {
        return apr_psprintf(pool_arg,
            "%s requires a non-negative integer argument", directive);
    }
    const char *digits = p;
    while (p < end && *p >= '0' && *p <= '9') p++;
    if (p != end) {
        return apr_psprintf(pool_arg,
            "%s must be a non-negative integer (got: %.*s)",
            directive, (int)(end - arg), arg);
    }
    if (digits == p) {
        return apr_psprintf(pool_arg,
            "%s must contain at least one digit", directive);
    }
    size_t n = (size_t)(p - digits);
    if (n > 9) {
        return apr_psprintf(pool_arg,
            "%s value exceeds maximum allowed (%d)", directive, max);
    }
    long val = 0;
    for (size_t i = 0; i < n; i++) {
        val = val * 10 + (digits[i] - '0');
    }
    if (val < min || val > max) {
        return apr_psprintf(pool_arg,
            "%s value out of range [%d, %d]", directive, min, max);
    }
    *out = (int)val;
    return NULL;
}

/* Set functions copied from mod_mesi.c — verify they wire directives
 * into mesi_config correctly and reject invalid values without
 * silent-default substitution. */
/* MesiMaxDepth — mirrors set_max_depth in mod_mesi.c. Uses
 * parse_nonneg_int (never atoi). Helper errors already name MesiMaxDepth. */
static const char *set_max_depth(mesi_config *conf, const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiMaxDepth",
                                       0, MESI_MAX_MAX_DEPTH, &v);
    if (err) {
        return err;
    }
    conf->max_depth = v;
    return NULL;
}

/* MesiTimeout — mirrors set_timeout in mod_mesi.c (#167). Uses
 * parse_nonneg_int (never atoi) with range [1, MESI_MAX_TIMEOUT_SECONDS]:
 * 0 is rejected because the core fails EVERY include when Timeout <= 0
 * (mesi/fetch.go → ErrTimeBudgetExceeded) — it is not "no timeout".
 * Helper errors already name MesiTimeout. */
static const char *set_timeout(mesi_config *conf, const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiTimeout",
                                       1, MESI_MAX_TIMEOUT_SECONDS, &v);
    if (err) {
        return err;
    }
    conf->timeout_seconds = v;
    return NULL;
}

/* 64-bit strict non-negative integer parser — copied verbatim from
 * mod_mesi.c. Same rejection contract as parse_nonneg_int (empty,
 * '-', '+', '.', trailing garbage, out of range — error names the
 * directive) but stores into apr_off_t: parse_nonneg_int's 9-digit
 * guard pins it to int32 range, which cannot express byte counts for
 * MesiMaxResponseSize (#169). Overflow is guarded per digit against
 * `max` before the multiply so no intermediate wraps. */
static const char *parse_nonneg_off(apr_pool_t *pool_arg, const char *arg,
                                    const char *directive,
                                    apr_off_t min, apr_off_t max,
                                    apr_off_t *out) {
    const char *p = arg ? arg : "";
    while (*p == ' ' || *p == '\t') p++;
    if (*p == '\0') {
        return apr_psprintf(pool_arg,
            "%s requires a non-negative integer argument", directive);
    }
    const char *digits = p;
    while (*p >= '0' && *p <= '9') p++;
    if (*p != '\0') {
        return apr_psprintf(pool_arg,
            "%s must be a non-negative integer (got: %s)", directive, arg);
    }
    if (digits == p) {
        return apr_psprintf(pool_arg,
            "%s must contain at least one digit (got: %s)", directive, arg);
    }
    apr_off_t val = 0;
    for (const char *q = digits; q < p; q++) {
        apr_off_t d = (apr_off_t)(*q - '0');
        if (val > (max - d) / 10) {
            return apr_psprintf(pool_arg,
                "%s value %s exceeds maximum allowed (%" APR_INT64_T_FMT ")",
                directive, arg, (apr_int64_t)max);
        }
        val = val * 10 + d;
    }
    if (val < min || val > max) {
        return apr_psprintf(pool_arg,
            "%s value %s out of range [%" APR_INT64_T_FMT ", %" APR_INT64_T_FMT "]",
            directive, arg, (apr_int64_t)min, (apr_int64_t)max);
    }
    *out = val;
    return NULL;
}

/* MesiMaxResponseSize — mirrors set_max_response_size in mod_mesi.c
 * (#169). Uses parse_nonneg_off (64-bit, never apr_strtoff — its
 * end=NULL form silently accepts trailing garbage and '+') with range
 * [0, MESI_MAX_MAX_RESPONSE_SIZE]. 0 is a LEGITIMATE configured
 * value ("unlimited", mesi/fetch.go only limits when > 0); negatives
 * are rejected (the core would silently treat them like 0).
 * Helper errors already name MesiMaxResponseSize. */
static const char *set_max_response_size(mesi_config *conf, const char *arg) {
    apr_off_t v = 0;
    const char *err = parse_nonneg_off(pool, arg, "MesiMaxResponseSize",
                                       0, MESI_MAX_MAX_RESPONSE_SIZE, &v);
    if (err) {
        return err;
    }
    conf->max_response_size = v;
    return NULL;
}

/* MesiMaxConcurrentRequests — mirrors set_max_concurrent_requests in
 * mod_mesi.c (#170). Uses parse_nonneg_int (never the issue's atoi
 * sketch — atoi coerces "abc"/"" to 0 = unlimited and accepts
 * trailing garbage like "3foo") with range [0,
 * MESI_MAX_MAX_CONCURRENT_REQUESTS]. 0 is a LEGITIMATE configured
 * value ("unlimited": mesi/parser.go only installs the semaphore when
 * > 0); negatives are rejected (the core would only warn and
 * normalize them to 0 = unlimited, #329). Helper errors already name
 * MesiMaxConcurrentRequests. */
static const char *set_max_concurrent_requests(mesi_config *conf, const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiMaxConcurrentRequests",
                                       0, MESI_MAX_MAX_CONCURRENT_REQUESTS, &v);
    if (err) {
        return err;
    }
    conf->max_concurrent_requests = v;
    return NULL;
}

static const char *set_cache_backend(mesi_config *conf, const char *arg) {
    if (!arg) {
        return "MesiCacheBackend requires an argument (use empty string to disable)";
    }
    if (strcmp(arg, "memory") == 0) {
        conf->cache_backend = "memory";
        return NULL;
    }
    if (strcmp(arg, "redis") == 0) {
        conf->cache_backend = "redis";
        return NULL;
    }
    if (strcmp(arg, "memcached") == 0) {
        conf->cache_backend = "memcached";
        return NULL;
    }
    if (arg[0] == '\0') {
        conf->cache_backend = "";
        return NULL;
    }
    return apr_psprintf(pool,
        "MesiCacheBackend: unknown backend %s "
        "(supported: \"memory\", \"redis\", \"memcached\", or empty)",
        arg);
}

static const char *set_cache_size(mesi_config *conf, const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiCacheSize",
                                       1, MESI_MAX_CACHE_SIZE, &v);
    if (err) {
        return err;
    }
    conf->cache_size = v;
    return NULL;
}

static const char *set_cache_ttl(mesi_config *conf, const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiCacheTTL",
                                       0, MESI_MAX_CACHE_TTL_SECONDS, &v);
    if (err) {
        return err;
    }
    conf->cache_ttl = v;
    return NULL;
}

/* MesiCacheRedisAddr — host:port. Empty arg clears config (default
 * localhost:6379 in libgomesi). Must contain ':' and a valid port
 * (1..65535). No whitespace, control chars, or JSON-meta chars. */
static const char *set_cache_redis_addr(mesi_config *conf, const char *arg) {
    if (!arg) {
        return "MesiCacheRedisAddr requires a host:port argument";
    }
    if (arg[0] == '\0') {
        conf->cache_redis_addr = NULL;
        return NULL;
    }
    for (const char *p = arg; *p; p++) {
        unsigned char c = (unsigned char)*p;
        if (c == ' ' || c == '\t' || c == '"' || c == '\\' || c < 0x20) {
            return apr_psprintf(pool,
                "MesiCacheRedisAddr: invalid character %d in %s",
                (int)c, arg);
        }
    }
    const char *colon = strrchr(arg, ':');
    if (!colon || colon == arg || *(colon + 1) == '\0') {
        return apr_psprintf(pool,
            "MesiCacheRedisAddr: must be host:port (got: %s)", arg);
    }
    int port = 0;
    const char *err = parse_nonneg_int(pool, colon + 1, "MesiCacheRedisAddr",
                                       1, 65535, &port);
    if (err) {
        return apr_psprintf(pool,
            "MesiCacheRedisAddr: port invalid: %s", arg);
    }
    conf->cache_redis_addr = apr_pstrdup(pool, arg);
    return NULL;
}

/* MesiCacheRedisPassword — raw Redis AUTH password. No control chars.
 * Empty arg sets empty password (auth disabled). Mock must not leak
 * the password into error messages. */
static const char *set_cache_redis_password(mesi_config *conf, const char *arg) {
    if (!arg) {
        conf->cache_redis_password = "";
        return NULL;
    }
    for (const char *p = arg; *p; p++) {
        unsigned char c = (unsigned char)*p;
        if (c < 0x20) {
            return apr_psprintf(pool,
                "MesiCacheRedisPassword: invalid control character 0x%02x in value",
                (unsigned)c);
        }
    }
    conf->cache_redis_password = apr_pstrdup(pool, arg);
    return NULL;
}

/* MesiCacheRedisDB — Redis logical DB number. 0..15 (Redis default). */
static const char *set_cache_redis_db(mesi_config *conf, const char *arg) {
    int v = -1;
    const char *err = parse_nonneg_int(pool, arg, "MesiCacheRedisDB",
                                       0, MESI_MAX_REDIS_DB, &v);
    if (err) {
        return err;
    }
    conf->cache_redis_db = v;
    return NULL;
}

/* MesiCacheMemcachedServers — space-separated "host:port" entries
 * (#176). Mirrors set_cache_memcached_servers in mod_mesi.c.
 * Each entry must contain a ':'+port_in_[1,65535]. Tokens with
 * embedded control chars or JSON-meta characters are rejected.
 */
static const char *set_cache_key_template(mesi_config *conf, const char *arg) {
    if (!arg) return "MesiCacheKeyTemplate requires an argument";
    if (arg[0] == '\0') { conf->cache_key_template = NULL; return NULL; }
    size_t len = strlen(arg);
    if (len > MESI_MAX_CACHE_KEY_TEMPLATE) return apr_psprintf(pool, "MesiCacheKeyTemplate exceeds maximum length %d (got %zu)", MESI_MAX_CACHE_KEY_TEMPLATE, len);
    for (const char *c = arg; *c; c++) {
        unsigned char uc = (unsigned char)*c;
        if (uc < 0x20) return apr_psprintf(pool, "MesiCacheKeyTemplate contains control character 0x%02x", uc);
        if (uc == 0x7f) return apr_psprintf(pool, "MesiCacheKeyTemplate contains DEL character");
    }
    char *norm = NULL;
    if (strstr(arg, "$${") != NULL) {
        size_t cnt = 0;
        for (const char *q = arg; (q = strstr(q, "$${")) != NULL; q += 3) cnt++;
        norm = apr_palloc(pool, strlen(arg) - cnt + 1);
        char *w = norm;
        const char *r = arg;
        while (*r) {
            if (r[0] == '$' && r[1] == '$' && r[2] == '{') {
                *w++ = '$'; *w++ = '{'; r += 3;
            } else {
                *w++ = *r++;
            }
        }
        *w = '\0';
        arg = norm;
        len = strlen(arg);
    }
    if (strstr(arg, "::") != NULL) {
        return apr_psprintf(pool, "MesiCacheKeyTemplate: Apache config interpolation replaced ${url} (AH00111); escape the dollar sign as $${url} in httpd.conf (got: %s)", arg);
    }
    if (arg[len - 1] == ':') {
        return apr_psprintf(pool, "MesiCacheKeyTemplate: Apache config interpolation replaced ${url} (AH00111); escape the dollar sign as $${url} in httpd.conf (got: %s)", arg);
    }
    conf->cache_key_template = apr_pstrdup(pool, arg);
    return NULL;
}

static const char *set_cache_memcached_servers(mesi_config *conf, const char *arg) {
    if (!arg) {
        return "MesiCacheMemcachedServers requires space-separated host:port entries";
    }
    int count = 0;
    const char *tok;
    while (*arg) {
        while (*arg && (*arg == ' ' || *arg == '\t')) arg++;
        tok = arg;
        while (*arg && *arg != ' ' && *arg != '\t') arg++;
        if (tok == arg) {
            continue;
        }
        int has_invalid = 0;
        for (const char *p = tok; p < arg; p++) {
            unsigned char c = (unsigned char)*p;
            if (c == '"' || c == '\\' || c < 0x20) {
                has_invalid = 1;
                break;
            }
        }
        if (has_invalid) {
            return apr_psprintf(pool,
                "MesiCacheMemcachedServers: invalid character in entry %.*s",
                (int)(arg - tok), tok);
        }
        const char *colon = NULL;
        for (const char *p = arg - 1; p >= tok; p--) {
            if (*p == ':') { colon = p; break; }
        }
        if (!colon || colon == tok || colon + 1 == arg) {
            return apr_psprintf(pool,
                "MesiCacheMemcachedServers: entry must be host:port (got: %.*s)",
                (int)(arg - tok), tok);
        }
        int port = 0;
        const char *err = parse_nonneg_int_bounded(pool, colon + 1, arg,
                                                    "MesiCacheMemcachedServers",
                                                    1, 65535, &port);
        if (err) {
            return apr_psprintf(pool,
                "MesiCacheMemcachedServers: port invalid in %.*s",
                (int)(arg - tok), tok);
        }
        if (count >= MESI_MAX_MEMCACHED_SERVERS) {
            return apr_psprintf(pool,
                "MesiCacheMemcachedServers: too many entries (max %d)",
                MESI_MAX_MEMCACHED_SERVERS);
        }
        const char **slot = apr_array_push(conf->cache_memcached_servers);
        *slot = apr_pstrndup(pool, tok, arg - tok);
        count++;
    }
    if (count == 0) {
        return "MesiCacheMemcachedServers requires at least one host:port entry";
    }
    return NULL;
}

static void init_config(mesi_config *conf) {
    memset(conf, 0, sizeof(*conf));
    conf->allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    conf->block_private_ips = -1;
    conf->allow_private_ips_for_allowed = -1;
    conf->shared_http_client = -1;
    conf->cache_backend = "";
    conf->cache_size = 0;
    conf->cache_ttl = -1;
    conf->cache_redis_addr = NULL;
    conf->cache_redis_password = NULL;
    conf->cache_redis_db = -1;
    conf->cache_memcached_servers = apr_array_make(pool, 2, sizeof(const char *));
    conf->cache_key_template = NULL;
    conf->max_depth = -1;
    conf->timeout_seconds = -1;
    conf->max_response_size = -1;
    conf->max_concurrent_requests = -1;
}

static void merge_configs(mesi_config *base, mesi_config *add, mesi_config *merged) {
    merged->enable_mesi = (add->enable_mesi != 0) ? add->enable_mesi : base->enable_mesi;
    merged->allowed_hosts = (add->allowed_hosts->nelts > 0) ? add->allowed_hosts : base->allowed_hosts;
    merged->block_private_ips = (add->block_private_ips != -1) ? add->block_private_ips : base->block_private_ips;
    merged->allow_private_ips_for_allowed = (add->allow_private_ips_for_allowed != -1)
        ? add->allow_private_ips_for_allowed : base->allow_private_ips_for_allowed;
    merged->shared_http_client = (add->shared_http_client != -1)
        ? add->shared_http_client : base->shared_http_client;
    merged->cache_backend = (add->cache_backend && add->cache_backend[0] != '\0')
                           ? add->cache_backend
                           : base->cache_backend;
    merged->cache_size = (add->cache_size > 0) ? add->cache_size : base->cache_size;
    merged->cache_ttl = (add->cache_ttl >= 0) ? add->cache_ttl : base->cache_ttl;
    merged->cache_redis_addr = add->cache_redis_addr ? add->cache_redis_addr : base->cache_redis_addr;
    merged->cache_redis_password = add->cache_redis_password ? add->cache_redis_password : base->cache_redis_password;
    merged->cache_redis_db = (add->cache_redis_db >= 0) ? add->cache_redis_db : base->cache_redis_db;
    merged->cache_memcached_servers = (add->cache_memcached_servers->nelts > 0)
                                      ? add->cache_memcached_servers
                                      : base->cache_memcached_servers;
    merged->cache_key_template = add->cache_key_template ? add->cache_key_template : base->cache_key_template;
    merged->max_depth = (add->max_depth != -1) ? add->max_depth : base->max_depth;
    merged->timeout_seconds = (add->timeout_seconds != -1) ? add->timeout_seconds : base->timeout_seconds;
    merged->max_response_size = (add->max_response_size != -1) ? add->max_response_size : base->max_response_size;
    merged->max_concurrent_requests = (add->max_concurrent_requests != -1)
        ? add->max_concurrent_requests
        : base->max_concurrent_requests;
}

/* Test cases */

TEST(single_hostname) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_allowed_hosts(&conf, "trusted.example.com");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 1);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "trusted.example.com");
}

TEST(multiple_hostnames_space) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_allowed_hosts(&conf, "host1.com host2.com host3.com");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 3);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "host1.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[1], "host2.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[2], "host3.com");
}

TEST(multiple_hostnames_tab) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_allowed_hosts(&conf, "host1.com\thost2.com");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 2);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "host1.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[1], "host2.com");
}

TEST(mixed_whitespace) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_allowed_hosts(&conf, "host1.com  host2.com\t host3.com");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 3);
}

TEST(empty_string) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_allowed_hosts(&conf, "");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 0);
}

TEST(whitespace_only) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_allowed_hosts(&conf, "   \t  ");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 0);
}

TEST(block_private_on) {
    mesi_config conf;
    conf.block_private_ips = -1;

    const char *err = parse_block_private_ips(&conf, 1);

    ASSERT_NULL(err);
    ASSERT_EQ(conf.block_private_ips, 1);
}

TEST(block_private_off) {
    mesi_config conf;
    conf.block_private_ips = -1;

    const char *err = parse_block_private_ips(&conf, 0);

    ASSERT_NULL(err);
    ASSERT_EQ(conf.block_private_ips, 0);
}

TEST(merge_child_overrides) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);

    base.block_private_ips = 0;
    add.block_private_ips = 1;

    merge_configs(&base, &add, &merged);

    ASSERT_EQ(merged.block_private_ips, 1);
}

TEST(merge_child_inherits) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);

    base.block_private_ips = 1;
    add.block_private_ips = -1;

    merge_configs(&base, &add, &merged);

    ASSERT_EQ(merged.block_private_ips, 1);
}

TEST(merge_allowed_hosts_child_set) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);

    const char **h1 = apr_array_push(base.allowed_hosts);
    *h1 = apr_pstrdup(pool, "base.com");

    const char **h2 = apr_array_push(add.allowed_hosts);
    *h2 = apr_pstrdup(pool, "add.com");

    merge_configs(&base, &add, &merged);

    ASSERT_EQ(merged.allowed_hosts->nelts, 1);
    ASSERT_STR_EQ(((const char **)merged.allowed_hosts->elts)[0], "add.com");
}

/* --- MesiAllowPrivateIPsForAllowedHosts directive tests (#168) --- */

TEST(allow_private_for_allowed_default_unset) {
    /* A freshly-created config has the sentinel -1 (unset → off). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_EQ(conf.allow_private_ips_for_allowed, -1);
}

TEST(allow_private_for_allowed_on) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_allow_private_for_allowed(&conf, 1);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allow_private_ips_for_allowed, 1);
}

TEST(allow_private_for_allowed_off) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_allow_private_for_allowed(&conf, 0);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allow_private_ips_for_allowed, 0);
}

TEST(merge_allow_private_for_allowed_child_overrides) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.allow_private_ips_for_allowed = 0;
    add.allow_private_ips_for_allowed = 1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.allow_private_ips_for_allowed, 1);
}

TEST(merge_allow_private_for_allowed_child_inherits) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.allow_private_ips_for_allowed = 1;
    add.allow_private_ips_for_allowed = -1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.allow_private_ips_for_allowed, 1);
}

/* --- MesiSharedHTTPClient directive tests (#178) --- */

TEST(shared_http_client_default_unset) {
    /* A freshly-created config has the sentinel -1 (unset → off). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_EQ(conf.shared_http_client, -1);
}

TEST(shared_http_client_on) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_shared_http_client(&conf, 1);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.shared_http_client, 1);
}

TEST(shared_http_client_off) {
    mesi_config conf;
    init_config(&conf);

    const char *err = parse_shared_http_client(&conf, 0);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.shared_http_client, 0);
}

TEST(merge_shared_http_client_child_overrides) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.shared_http_client = 0;
    add.shared_http_client = 1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.shared_http_client, 1);
}

TEST(merge_shared_http_client_child_inherits) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.shared_http_client = 1;
    add.shared_http_client = -1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.shared_http_client, 1);
}

/* --- Cache backend directive tests (#174) --- */

TEST(cache_backend_memory) {
    mesi_config conf;
    init_config(&conf);

    /* camelCase constant "memory" */
    ASSERT_NULL(set_cache_backend(&conf, "memory"));
    ASSERT_STR_EQ(conf.cache_backend, "memory");
}

TEST(cache_backend_empty_disables) {
    mesi_config conf;
    init_config(&conf);
    conf.cache_backend = "memory";

    /* empty string explicitly disables (visible operator action) */
    ASSERT_NULL(set_cache_backend(&conf, ""));
    ASSERT_STR_EQ(conf.cache_backend, "");
}

TEST(cache_backend_unknown_rejected) {
    /* Unknown backend must NOT silently fall back to "no cache" — the
     * workflow prohibits silent-default in parsers. Reject loudly. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_backend(&conf, "file");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "file");
    ASSERT_STR_CONTAINS(err, "memory");
    ASSERT_STR_CONTAINS(err, "redis");
    ASSERT_STR_CONTAINS(err, "memcached");
    /* config left at default "" instead of being polluted with bogus value */
    ASSERT_STR_EQ(conf.cache_backend, "");
}

TEST(cache_backend_redis_accepted) {
    /* Redis backend is supported (#175). */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_backend(&conf, "redis"));
    ASSERT_STR_EQ(conf.cache_backend, "redis");
}

TEST(cache_backend_memcached_accepted) {
    /* Memcached backend string is accepted; runtime InitCacheWithConfig
     * will surface the missing servers list (#176). */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_backend(&conf, "memcached"));
    ASSERT_STR_EQ(conf.cache_backend, "memcached");
}

TEST(cache_backend_unknown_variant_rejected) {
    /* A near-miss like "rediscluster" must be rejected, not silently
     * aliased. Parity with #174 unknown-rejected test. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_backend(&conf, "rediscluster");
    ASSERT_NOT_NULL(err);
    /* value was NOT stored */
    ASSERT_STR_EQ(conf.cache_backend, "");
}

TEST(cache_size_default_unset) {
    /* 0 / -1 are sentinel "unset" — directive never invokes parser with those */
    mesi_config conf;
    init_config(&conf);

    ASSERT_EQ(conf.cache_size, 0);
    ASSERT_EQ(conf.cache_ttl, -1);
}

TEST(cache_size_valid) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_size(&conf, "5000"));
    ASSERT_EQ(conf.cache_size, 5000);
}

TEST(cache_size_min_accepted) {
    /* Boundary: 1 is the smallest legal size */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_size(&conf, "1"));
    ASSERT_EQ(conf.cache_size, 1);
}

TEST(cache_size_zero_rejected) {
    /* Boundary: 0 must be rejected (size must be positive) */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_size(&conf, "0");
    ASSERT_NOT_NULL(err);
    /* config must NOT silently retain a non-zero value */
    ASSERT_EQ(conf.cache_size, 0);
}

TEST(cache_size_negative_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_size(&conf, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "non-negative");
}

TEST(cache_size_max_accepted) {
    /* Boundary: MESI_MAX_CACHE_SIZE (1000000) is accepted */
    mesi_config conf;
    init_config(&conf);

    char buf[16];
    snprintf(buf, sizeof(buf), "%d", MESI_MAX_CACHE_SIZE);
    ASSERT_NULL(set_cache_size(&conf, buf));
    ASSERT_EQ(conf.cache_size, MESI_MAX_CACHE_SIZE);
}

TEST(cache_size_max_plus_one_rejected) {
    /* Boundary: MESI_MAX_CACHE_SIZE+1 must be rejected */
    mesi_config conf;
    init_config(&conf);

    char buf[16];
    snprintf(buf, sizeof(buf), "%d", MESI_MAX_CACHE_SIZE + 1);
    const char *err = set_cache_size(&conf, buf);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "out of range");
}

TEST(cache_size_decimal_rejected) {
    /* Decimals must fail-fast — no silent truncation */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_size(&conf, "100.5");
    ASSERT_NOT_NULL(err);
}

TEST(cache_size_alpha_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_size(&conf, "abc");
    ASSERT_NOT_NULL(err);
}

TEST(cache_size_oversized_rejected) {
    /* 12345678901 (11 digits) must be rejected for safety */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_size(&conf, "12345678901");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "exceeds");
}

TEST(cache_size_with_leading_space) {
    /* Leading [ \t] is allowed by the parser */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_size(&conf, "  42"));
    ASSERT_EQ(conf.cache_size, 42);
}

TEST(cache_size_with_leading_plus_rejected) {
    /* "+10" is rejected — only [ \t] can precede the integer */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_size(&conf, "+10");
    ASSERT_NOT_NULL(err);
}

TEST(cache_size_empty_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_size(&conf, "");
    ASSERT_NOT_NULL(err);
}

TEST(cache_ttl_zero_accepted) {
    /* Boundary: 0 is the smallest legal TTL (no expiry) */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_ttl(&conf, "0"));
    ASSERT_EQ(conf.cache_ttl, 0);
}

TEST(cache_ttl_valid) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_ttl(&conf, "60"));
    ASSERT_EQ(conf.cache_ttl, 60);
}

TEST(cache_ttl_max_accepted) {
    /* Boundary: 86400 (1 day) is the configurable max */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_ttl(&conf, "86400"));
    ASSERT_EQ(conf.cache_ttl, 86400);
}

TEST(cache_ttl_max_plus_one_rejected) {
    /* Boundary: 86401 must be rejected */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_ttl(&conf, "86401");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "out of range");
}

TEST(cache_ttl_negative_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_ttl(&conf, "-5");
    ASSERT_NOT_NULL(err);
}

TEST(cache_ttl_decimal_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_ttl(&conf, "30.5");
    ASSERT_NOT_NULL(err);
}

TEST(cache_ttl_alpha_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_ttl(&conf, "60s");
    ASSERT_NOT_NULL(err);
}

TEST(merge_cache_backend_child_overrides) {
    /* Child directive wins even when it differs from parent.
     * (Both having the same value would not exercise the override
     * branch — child simply needs an explicit, non-empty backend.) */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_backend = "";        /* parent has cache disabled */
    add.cache_backend = "memory";   /* child opts in */

    merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_backend, "memory");
}

TEST(merge_cache_backend_child_inherits) {
    /* Child unset ("" from default-initialized config) inherits parent */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_backend = "memory";
    add.cache_backend = "";

    merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_backend, "memory");
}

TEST(merge_cache_size_child_overrides) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_size = 100;
    add.cache_size = 5000;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_size, 5000);
}

TEST(merge_cache_size_child_inherits) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_size = 100;
    add.cache_size = 0;  /* sentinel "unset" */

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_size, 100);
}

TEST(merge_cache_ttl_child_overrides) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_ttl = 30;
    add.cache_ttl = 0;  /* explicit 0 = no expiry */

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_ttl, 0);
}

TEST(merge_cache_ttl_child_inherits) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_ttl = 30;
    add.cache_ttl = -1;  /* sentinel "unset" */

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_ttl, 30);
}

/* --- Redis backend directive tests (#175) --- */

TEST(redis_addr_default_unset) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(conf.cache_redis_addr);
}

TEST(redis_addr_valid) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_addr(&conf, "10.0.0.5:6379"));
    ASSERT_STR_EQ(conf.cache_redis_addr, "10.0.0.5:6379");
}

TEST(redis_addr_localhost_default_port) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_addr(&conf, "localhost:6379"));
    ASSERT_STR_EQ(conf.cache_redis_addr, "localhost:6379");
}

TEST(redis_addr_hostname_with_port) {
    /* Issue example: "10.0.0.5:6379" — also confirms hostnames work. */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_addr(&conf, "redis.local:6380"));
    ASSERT_STR_EQ(conf.cache_redis_addr, "redis.local:6380");
}

TEST(redis_addr_empty_clears) {
    /* Empty arg explicitly clears the address (operator action: use
     * libgomesi defaults localhost:6379). Mirrors mod_mesi.c
     * set_cache_redis_addr() behavior. */
    mesi_config conf;
    init_config(&conf);
    conf.cache_redis_addr = "10.0.0.5:6379";  // existing value

    ASSERT_NULL(set_cache_redis_addr(&conf, ""));
    ASSERT_NULL(conf.cache_redis_addr);
}

TEST(redis_addr_missing_port_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, "10.0.0.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host:port");
}

TEST(redis_addr_missing_host_rejected) {
    /* ":6379" — colon at start, no host. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, ":6379");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host:port");
}

TEST(redis_addr_missing_port_value_rejected) {
    /* "host:" — colon at end. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, "10.0.0.5:");
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_port_zero_rejected) {
    /* Boundary: port must be >= 1. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, "10.0.0.5:0");
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_port_max_accepted) {
    /* Boundary: port 65535 is the largest legal port. */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_addr(&conf, "10.0.0.5:65535"));
    ASSERT_STR_EQ(conf.cache_redis_addr, "10.0.0.5:65535");
}

TEST(redis_addr_port_max_plus_one_rejected) {
    /* Boundary: 65536 rejected. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, "10.0.0.5:65536");
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_port_negative_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, "10.0.0.5:-1");
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_with_whitespace_rejected) {
    /* JSON cannot be safely generated with embedded whitespace or
     * tabs. Reject so misconfig never silently produces odd JSON. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, "10.0.0.5 :6379");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(redis_addr_with_quote_rejected) {
    /* Embedded '"' would inject JSON keys. Reject. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, "10.0.0.5\" :6379");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(redis_addr_with_backslash_rejected) {
    /* Embedded '\\' would inject JSON escape sequences. Reject. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, "10.0.0.5\\:6379");
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_null_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_addr(&conf, NULL);
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_ipv6_localhost_accepted) {
    /* [::1]:6379 — IPv6 literal. Note: we treat as a single colon in
     * strrchr so the *last* colon is the port separator; the host
     * itself may contain colons. Validate that the resulting port
     * is in 1..65535 and that the inner content is unfiltered only
     * for control chars (we don't validate IPv6 syntax beyond that). */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_addr(&conf, "[::1]:6379"));
    ASSERT_STR_EQ(conf.cache_redis_addr, "[::1]:6379");
}

TEST(redis_password_default_unset) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(conf.cache_redis_password);
}

TEST(redis_password_empty_accepted) {
    /* Empty password explicitly set = no auth. */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_password(&conf, ""));
    ASSERT_STR_EQ(conf.cache_redis_password, "");
}

TEST(redis_password_valid) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_password(&conf, "supersecret123"));
    ASSERT_STR_EQ(conf.cache_redis_password, "supersecret123");
}

TEST(redis_password_with_special_chars_accepted) {
    /* Quotes and backslashes inside the password are fine — they get
     * escaped by build_redis_config_json. */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_password(&conf, "p@ssw0rd\"with\\quotes"));
    ASSERT_STR_EQ(conf.cache_redis_password, "p@ssw0rd\"with\\quotes");
}

TEST(redis_password_with_control_char_rejected) {
    /* Newlines, tabs, BEL, etc. would corrupt the rendered JSON. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_password(&conf, "abc\x01");
    ASSERT_NOT_NULL(err);
    /* Must NOT leak the password into the error message. */
    ASSERT_NULL(strstr(err, "abc"));
    ASSERT_STR_CONTAINS(err, "control character");
}

TEST(redis_password_null_accepted) {
    /* AP_INIT_TAKE1 args are never NULL per Apache contract, but the
     * parser must guard anyway — treat NULL as "clear password". */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_password(&conf, NULL));
    ASSERT_STR_EQ(conf.cache_redis_password, "");
}

TEST(redis_db_default_unset) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_EQ(conf.cache_redis_db, -1);
}

TEST(redis_db_zero_accepted) {
    /* Boundary: 0 is the smallest legal DB number. */
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_db(&conf, "0"));
    ASSERT_EQ(conf.cache_redis_db, 0);
}

TEST(redis_db_valid) {
    mesi_config conf;
    init_config(&conf);

    ASSERT_NULL(set_cache_redis_db(&conf, "2"));
    ASSERT_EQ(conf.cache_redis_db, 2);
}

TEST(redis_db_max_accepted) {
    /* Boundary: MESI_MAX_REDIS_DB (15) is the largest legal DB. */
    mesi_config conf;
    init_config(&conf);

    char buf[16];
    snprintf(buf, sizeof(buf), "%d", MESI_MAX_REDIS_DB);
    ASSERT_NULL(set_cache_redis_db(&conf, buf));
    ASSERT_EQ(conf.cache_redis_db, MESI_MAX_REDIS_DB);
}

TEST(redis_db_max_plus_one_rejected) {
    /* Boundary: 16 is out of range (Redis default max is 16). */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_db(&conf, "16");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "out of range");
}

TEST(redis_db_negative_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_db(&conf, "-1");
    ASSERT_NOT_NULL(err);
    /* config must NOT silently retain a non-negative value */
    ASSERT_EQ(conf.cache_redis_db, -1);
}

TEST(redis_db_decimal_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_db(&conf, "2.5");
    ASSERT_NOT_NULL(err);
}

TEST(redis_db_empty_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_db(&conf, "");
    ASSERT_NOT_NULL(err);
}

TEST(redis_db_oversized_rejected) {
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_redis_db(&conf, "12345678901");
    ASSERT_NOT_NULL(err);
}

TEST(merge_redis_addr_child_overrides) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_redis_addr = NULL;
    add.cache_redis_addr = "10.0.0.5:6379";

    merge_configs(&base, &add, &merged);
    ASSERT_NOT_NULL(merged.cache_redis_addr);
    ASSERT_STR_EQ(merged.cache_redis_addr, "10.0.0.5:6379");
}

TEST(merge_redis_addr_child_inherits) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_redis_addr = "10.0.0.5:6379";
    add.cache_redis_addr = NULL;

    merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_redis_addr, "10.0.0.5:6379");
}

TEST(merge_redis_db_child_overrides) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_redis_db = -1;
    add.cache_redis_db = 2;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_redis_db, 2);
}

TEST(merge_redis_db_child_inherits) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_redis_db = 5;
    add.cache_redis_db = -1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_redis_db, 5);
}

TEST(merge_redis_password_child_overrides) {
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.cache_redis_password = NULL;
    add.cache_redis_password = "secret";

    merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_redis_password, "secret");
}

/* --- Memcached backend directive tests (#176) --- */

TEST(memcached_servers_default_unset) {
    /* A freshly-created config has an empty server array (nelts == 0),
     * not NULL — runtime cache init renders an empty JSON array so the
     * libgomesi parser produces a deterministic error. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NOT_NULL(conf.cache_memcached_servers);
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_single) {
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_cache_memcached_servers(&conf, "10.0.0.1:11211"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 1);
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "10.0.0.1:11211");
}

TEST(memcached_servers_multiple) {
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_cache_memcached_servers(&conf,
        "10.0.0.1:11211 10.0.0.2:11211 10.0.0.3:11211"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 3);
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "10.0.0.1:11211");
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[1],
                  "10.0.0.2:11211");
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[2],
                  "10.0.0.3:11211");
}

TEST(memcached_servers_mixed_whitespace) {
    mesi_config conf;
    init_config(&conf);
    /* Tabs and spaces between entries; leading/trailing whitespace
     * silently trimmed (matches set_allowed_hosts behavior). */
    ASSERT_NULL(set_cache_memcached_servers(&conf,
        "  host1:11211\thost2:11211  \thost3:11211"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 3);
}

TEST(memcached_servers_empty_list_rejected) {
    /* Boundary: an empty value list produces no entries — must be
     * rejected, never treated as a valid config. Operators who
     * accidentally leave the directive empty should see an error. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "at least one");
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_whitespace_only_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "   \t  ");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "at least one");
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_null_arg_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, NULL);
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_default_port_explicit_rejected) {
    /* Issue example: "10.0.0.1" (no port) — fail-fast instead of
     * accepting whatever default memcache.New might pick. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host:port");
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_only_colon_rejected) {
    /* ":11211" — colon at start, empty host. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, ":11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host:port");
}

TEST(memcached_servers_missing_port_value_rejected) {
    /* "host:" — colon at end, empty port. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1:");
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_port_zero_rejected) {
    /* Boundary: port must be >= 1. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1:0");
    ASSERT_NOT_NULL(err);
    /* Error must NOT leak the value verbatim into a control-char
     * path; use port-invalid phrasing instead. */
    ASSERT_STR_CONTAINS(err, "port invalid");
}

TEST(memcached_servers_port_max_accepted) {
    /* Boundary: port 65535 is the largest legal port. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_cache_memcached_servers(&conf, "10.0.0.1:65535"));
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "10.0.0.1:65535");
}

TEST(memcached_servers_port_max_plus_one_rejected) {
    /* Boundary: port 65536 is out of range. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1:65536");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "port invalid");
}

TEST(memcached_servers_negative_port_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1:-1");
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_decimal_port_rejected) {
    /* parse_nonneg_int rejects decimal ports in [0,1), so 11211.5 fails
     * before the int parser trips. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1:11211.5");
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_alpha_port_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "host:http");
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_internal_whitespace_rejected) {
    /* Tokens were extracted on whitespace boundaries, so internal
     * whitespace is impossible at this layer. Verify quote/control
     * chars instead — those would corrupt the rendered JSON config. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1\" :11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(memcached_servers_backslash_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1\\:11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(memcached_servers_control_char_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_cache_memcached_servers(&conf, "10.0.0.1\x01:11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(memcached_servers_max_count_accepted) {
    /* Boundary: MESI_MAX_MEMCACHED_SERVERS entries are accepted in
     * one directive. We build "n.n.n.n:nnnn" × N in a fresh pool. */
    mesi_config conf;
    init_config(&conf);
    char args[4096];
    int pos = 0;
    for (int i = 0; i < MESI_MAX_MEMCACHED_SERVERS && pos < (int)sizeof(args) - 32; i++) {
        int n = snprintf(args + pos, sizeof(args) - pos,
                         "%s10.0.0.%d:11211", (i == 0 ? "" : " "), i);
        if (n < 0 || n >= (int)sizeof(args) - pos) break;
        pos += n;
    }
    ASSERT_NULL(set_cache_memcached_servers(&conf, args));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, MESI_MAX_MEMCACHED_SERVERS);
}

TEST(memcached_servers_over_max_rejected) {
    /* Boundary: MESI_MAX_MEMCACHED_SERVERS + 1 must be rejected. */
    mesi_config conf;
    init_config(&conf);
    char args[8192];
    int pos = 0;
    for (int i = 0; i < MESI_MAX_MEMCACHED_SERVERS + 1 && pos < (int)sizeof(args) - 32; i++) {
        int n = snprintf(args + pos, sizeof(args) - pos,
                         "%s10.0.0.%d:11211", (i == 0 ? "" : " "), i);
        if (n < 0 || n >= (int)sizeof(args) - pos) break;
        pos += n;
    }
    const char *err = set_cache_memcached_servers(&conf, args);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "too many");
    /* Count must match the cap; we never silently truncated and stored
     * a partial list. */
    ASSERT_EQ(conf.cache_memcached_servers->nelts, MESI_MAX_MEMCACHED_SERVERS);
}

TEST(memcached_servers_ipv6_accepted) {
    /* "[::1]:11211" — IPv6 literal with brackets. The "last ':'"
     * detection finds the trailing :port colon; the host field is
     * otherwise unvalidated beyond char-class safety. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_cache_memcached_servers(&conf, "[::1]:11211"));
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "[::1]:11211");
}

TEST(memcached_servers_append_to_existing_in_field) {
    /* Mock mirrors mod_mesi.c: each call *appends* to the array rather
     * than replacing it, matching set_allowed_hosts. Calling the
     * directive twice builds a longer list (operator can split a list
     * across multiple directive lines). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_cache_memcached_servers(&conf, "host-a:11211"));
    ASSERT_NULL(set_cache_memcached_servers(&conf, "host-b:11211"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 2);
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "host-a:11211");
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[1],
                  "host-b:11211");
}

TEST(merge_memcached_servers_child_overrides) {
    /* Child directive with entries wins over parent. */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    const char **b1 = apr_array_push(base.cache_memcached_servers);
    *b1 = apr_pstrdup(pool, "10.0.0.1:11211");
    const char **b2 = apr_array_push(add.cache_memcached_servers);
    *b2 = apr_pstrdup(pool, "10.0.0.99:11211");

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_memcached_servers->nelts, 1);
    ASSERT_STR_EQ(((const char **)merged.cache_memcached_servers->elts)[0],
                  "10.0.0.99:11211");
}

TEST(merge_memcached_servers_child_inherits) {
    /* Child has empty list → inherits parent's list verbatim. */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    const char **b1 = apr_array_push(base.cache_memcached_servers);
    *b1 = apr_pstrdup(pool, "10.0.0.1:11211");

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_memcached_servers->nelts, 1);
    ASSERT_STR_EQ(((const char **)merged.cache_memcached_servers->elts)[0],
                  "10.0.0.1:11211");
}


/* --- MesiCacheKeyTemplate directive tests (#177) --- */
TEST(cache_key_template_default_null) {
    mesi_config conf; init_config(&conf);
    ASSERT_NULL(conf.cache_key_template);
}
TEST(cache_key_template_valid_simple) {
    mesi_config conf; init_config(&conf);
    ASSERT_NULL(set_cache_key_template(&conf, "mesi:${url}"));
    ASSERT_NOT_NULL(conf.cache_key_template);
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}");
}
TEST(cache_key_template_with_header) {
    mesi_config conf; init_config(&conf);
    ASSERT_NULL(set_cache_key_template(&conf, "mesi:${url}:${header:Accept-Language}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${header:Accept-Language}");
}
TEST(cache_key_template_with_cookie) {
    mesi_config conf; init_config(&conf);
    ASSERT_NULL(set_cache_key_template(&conf, "mesi:${url}:${cookie:segment}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${cookie:segment}");
}
TEST(cache_key_template_with_both) {
    mesi_config conf; init_config(&conf);
    ASSERT_NULL(set_cache_key_template(&conf, "mesi:${url}:${header:Accept-Language}:${cookie:segment}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${header:Accept-Language}:${cookie:segment}");
}
TEST(cache_key_template_unknown_placeholder_literal) {
    mesi_config conf; init_config(&conf);
    ASSERT_NULL(set_cache_key_template(&conf, "mesi:${url}:${unknown:foo}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${unknown:foo}");
}
TEST(cache_key_template_empty_clears) {
    mesi_config conf; init_config(&conf);
    conf.cache_key_template = "old";
    ASSERT_NULL(set_cache_key_template(&conf, ""));
    ASSERT_NULL(conf.cache_key_template);
}
TEST(cache_key_template_null_arg_rejected) {
    mesi_config conf; init_config(&conf);
    const char *err = set_cache_key_template(&conf, NULL);
    ASSERT_NOT_NULL(err);
}
TEST(cache_key_template_control_rejected) {
    mesi_config conf; init_config(&conf);
    const char *err = set_cache_key_template(&conf, "mesi:\001bad");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "control");
}
TEST(cache_key_template_mangled_double_colon_rejected) {
    mesi_config conf; init_config(&conf);
    const char *err = set_cache_key_template(&conf, "mesi::${header:X}");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "AH00111");
}
TEST(cache_key_template_trailing_colon_rejected) {
    mesi_config conf; init_config(&conf);
    const char *err = set_cache_key_template(&conf, "mesi:${header:X}:");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "AH00111");
}
TEST(cache_key_template_escaped_dollar_accepted) {
    mesi_config conf; init_config(&conf);
    ASSERT_NULL(set_cache_key_template(&conf, "mesi:${url}:${header:Accept-Language}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${header:Accept-Language}");
}
TEST(cache_key_template_mid_position_accepted) {
    mesi_config conf; init_config(&conf);
    ASSERT_NULL(set_cache_key_template(&conf, "mesi:${header:X}:${url}:${cookie:C}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${header:X}:${url}:${cookie:C}");
}
TEST(cache_key_template_too_long_rejected) {
    mesi_config conf; init_config(&conf);
    char big[MESI_MAX_CACHE_KEY_TEMPLATE+10];
    memset(big, 'a', sizeof(big)-1); big[sizeof(big)-1]='\0';
    const char *err = set_cache_key_template(&conf, big);
    ASSERT_NOT_NULL(err);
}
TEST(merge_cache_key_template_child_overrides) {
    mesi_config base, add, merged; init_config(&base); init_config(&add); init_config(&merged);
    base.cache_key_template = "mesi:${url}";
    add.cache_key_template = "mesi:${url}:${header:X}";
    merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_key_template, "mesi:${url}:${header:X}");
}
TEST(merge_cache_key_template_child_inherits) {
    mesi_config base, add, merged; init_config(&base); init_config(&add); init_config(&merged);
    base.cache_key_template = "mesi:${url}";
    add.cache_key_template = NULL;
    merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_key_template, "mesi:${url}");
}

/* --- MesiMaxDepth directive tests (#166) --- */

TEST(max_depth_default_unset) {
    /* A freshly-created config has the sentinel -1 (unset → 5 in filter). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_three_accepted) {
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_depth(&conf, "3"));
    ASSERT_EQ(conf.max_depth, 3);
}

TEST(max_depth_zero_accepted) {
    /* Explicit 0 is valid passthrough (no ESI fetch). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_depth(&conf, "0"));
    ASSERT_EQ(conf.max_depth, 0);
}

TEST(max_depth_hundred_accepted) {
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_depth(&conf, "100"));
    ASSERT_EQ(conf.max_depth, 100);
}

TEST(max_depth_max_accepted) {
    /* Boundary: MESI_MAX_MAX_DEPTH (10000) is accepted. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_depth(&conf, "10000"));
    ASSERT_EQ(conf.max_depth, MESI_MAX_MAX_DEPTH);
}

TEST(max_depth_negative_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_depth(&conf, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    /* config must NOT silently retain a non-negative value */
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_alpha_rejected) {
    /* atoi("abc") → 0 would silently become passthrough. Reject. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_depth(&conf, "abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_trailing_garbage_rejected) {
    /* atoi("3foo") → 3 would silently accept. Reject. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_depth(&conf, "3foo");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_empty_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_depth(&conf, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_decimal_rejected) {
    /* Decimals must fail-fast — no silent truncation. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_depth(&conf, "3.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_oversize_rejected) {
    /* Boundary: MESI_MAX_MAX_DEPTH+1 (10001) must be rejected. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_depth(&conf, "10001");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(merge_max_depth_child_overrides) {
    /* Child 3 / parent unset → 3 */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_depth = -1;
    add.max_depth = 3;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_depth, 3);
}

TEST(merge_max_depth_child_inherits) {
    /* Child unset / parent 10 → 10 */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_depth = 10;
    add.max_depth = -1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_depth, 10);
}

TEST(merge_max_depth_child_zero_overrides) {
    /* Explicit 0 must win over parent — 0 is configured, not unset. */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_depth = 5;
    add.max_depth = 0;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_depth, 0);
}

/* --- MesiTimeout directive tests (#167) --- */

TEST(timeout_default_unset) {
    /* Fresh config: sentinel -1 (unset). The filter then stays on the
     * legacy parse path and LIBGOMESI applies its default 30s
     * (config.DefaultTimeoutSeconds) Go-side; MESI_DEFAULT_TIMEOUT_SECONDS
     * only documents that value (pinned to 30 here) so "unset → 30s" is
     * asserted at unit level alongside the functional default-timeout
     * case. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_EQ(conf.timeout_seconds, -1);
    ASSERT_EQ(MESI_DEFAULT_TIMEOUT_SECONDS, 30);
}

TEST(timeout_ten_accepted) {
    /* Issue example: MesiTimeout 10. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_timeout(&conf, "10"));
    ASSERT_EQ(conf.timeout_seconds, 10);
}

TEST(timeout_min_accepted) {
    /* Boundary: 1 is the smallest legal timeout. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_timeout(&conf, "1"));
    ASSERT_EQ(conf.timeout_seconds, 1);
}

TEST(timeout_max_accepted) {
    /* Boundary: 86400 (24h) is the configured max. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_timeout(&conf, "86400"));
    ASSERT_EQ(conf.timeout_seconds, MESI_MAX_TIMEOUT_SECONDS);
}

TEST(timeout_max_plus_one_rejected) {
    /* Boundary: 86401 must be rejected. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_timeout(&conf, "86401");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_STR_CONTAINS(err, "out of range");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_zero_rejected) {
    /* Boundary + AC deviation: the issue proposed 0 = "no timeout /
     * unlimited", but the core fails EVERY include immediately when
     * Timeout <= 0 (mesi/fetch.go → ErrTimeBudgetExceeded) — 0 would
     * brick all ESI. Rejected at config load instead (like Caddy's
     * "timeout must be positive"). */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_timeout(&conf, "0");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_negative_rejected) {
    /* AC: MesiTimeout -1 is an error (atoi would silently coerce it). */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_timeout(&conf, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_alpha_rejected) {
    /* atoi("abc") → 0 would silently mean... a broken config. Reject. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_timeout(&conf, "abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_trailing_garbage_rejected) {
    /* atoi("3foo") → 3 would silently accept. Reject. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_timeout(&conf, "3foo");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_decimal_rejected) {
    /* Decimals must fail-fast — atoi("2.5") would truncate to 2. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_timeout(&conf, "2.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_empty_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_timeout(&conf, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_oversize_rejected) {
    /* 12345678901 (11 digits) — overflow guard fires before the range
     * check; 999999999 (9 digits) passes the digit-count guard but is
     * caught by the [1, 86400] range. Both must fail. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_timeout(&conf, "12345678901");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "exceeds");
    ASSERT_EQ(conf.timeout_seconds, -1);

    err = set_timeout(&conf, "999999999");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "out of range");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(merge_timeout_child_overrides) {
    /* Child 30 / parent 10 → 30 (add overrides base). */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.timeout_seconds = 10;
    add.timeout_seconds = 30;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.timeout_seconds, 30);
}

TEST(merge_timeout_child_inherits) {
    /* Base 10 + unset add → 10 (unset child inherits the parent). */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.timeout_seconds = 10;
    add.timeout_seconds = -1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.timeout_seconds, 10);
}

TEST(merge_timeout_both_unset) {
    /* Both unset → -1 sentinel → legacy path; LIBGOMESI applies the
     * 30s default Go-side. */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.timeout_seconds, -1);
}

/* --- MesiMaxResponseSize directive tests (#169) --- */

TEST(mrs_default_unset) {
    /* Fresh config: sentinel -1 (unset). The filter then stays on the
     * legacy parse path and LIBGOMESI leaves MaxResponseSize at 0 —
     * "unlimited", the exact pre-#169 Apache behaviour (there is NO
     * implicit 10 MB default on the libgomesi path: the 10 MB of
     * mesi.CreateDefaultConfig only reaches Go callers of that
     * constructor, never the positional Parse* entry points). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_zero_accepted) {
    /* AC: MesiMaxResponseSize 0 = unlimited. Unlike MesiTimeout, 0 is
     * a legitimate configured value (mesi/fetch.go only limits when
     * MaxResponseSize > 0), so it must parse AND be storable — the
     * -1 sentinel is what keeps it distinct from "unset". */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_response_size(&conf, "0"));
    ASSERT_EQ(conf.max_response_size, (apr_off_t)0);
}

TEST(mrs_small_accepted) {
    /* AC: MesiMaxResponseSize 100 — a 200-byte include must be
     * rejected at fetch time (functional test on vhost 8089). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_response_size(&conf, "100"));
    ASSERT_EQ(conf.max_response_size, (apr_off_t)100);
}

TEST(mrs_one_mb_accepted) {
    /* AC: MesiMaxResponseSize 1048576 — a 500 KB include must
     * succeed (functional test on vhost 8090). Issue example. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_response_size(&conf, "1048576"));
    ASSERT_EQ(conf.max_response_size, (apr_off_t)1048576);
}

TEST(mrs_max_accepted) {
    /* Boundary: 9223372036854775806 (math.MaxInt64 - 1) is the
     * configured max — the largest value for which the core's
     * MaxResponseSize+1 LimitReader bound (mesi/fetch.go) stays
     * positive. Needs the full 19-digit range parse_nonneg_int's
     * 9-digit guard cannot express. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_response_size(&conf, "9223372036854775806"));
    ASSERT_EQ(conf.max_response_size, MESI_MAX_MAX_RESPONSE_SIZE);
    ASSERT_EQ(MESI_MAX_MAX_RESPONSE_SIZE, (apr_off_t)9223372036854775806LL);
}

TEST(mrs_max_plus_one_rejected) {
    /* Boundary: 9223372036854775807 (math.MaxInt64) — at this value
     * the core's MaxResponseSize+1 wraps negative and the include
     * would silently render an EMPTY body. Rejected at config load. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_response_size(&conf, "9223372036854775807");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_STR_CONTAINS(err, "exceeds maximum");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_negative_rejected) {
    /* AC: negative is an error. The core's > 0 check would silently
     * treat a negative exactly like 0 (unlimited) — a malformed
     * explicit value must never pass as a documented one. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_response_size(&conf, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_plus_sign_rejected) {
    /* Deviation from the issue's apr_strtoff sketch: apr_strtoff
     * accepts a leading '+'. parse_nonneg_off skips leading spaces/tabs
     * too (like parse_nonneg_int), but rejects the sign — the strict
     * digits-only parser requires a plain decimal form. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_response_size(&conf, "+100");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_alpha_rejected) {
    /* atoi("abc") → 0 would silently mean "unlimited". Reject. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_response_size(&conf, "abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_trailing_garbage_rejected) {
    /* Deviation from the issue's apr_strtoff sketch: with end=NULL,
     * apr_strtoff silently accepts "100abc" (it stops at the first
     * invalid char and nobody checks it). The strict parser rejects
     * any trailing garbage. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_response_size(&conf, "100abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_decimal_rejected) {
    /* Decimals must fail-fast — a truncating parse would silently
     * accept "1024.5" as 1024. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_response_size(&conf, "2.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_empty_rejected) {
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_response_size(&conf, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_oversize_rejected) {
    /* 19 nines (9999999999999999999) fits the digit count but not
     * the cap; 18446744073709551616 (20 digits, uint64_MAX+1) trips
     * the per-digit overflow guard before any intermediate wraps.
     * Both must fail with an error naming the directive. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_response_size(&conf, "9999999999999999999");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_STR_CONTAINS(err, "exceeds maximum");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);

    err = set_max_response_size(&conf, "18446744073709551616");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "exceeds maximum");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(merge_mrs_child_overrides) {
    /* Child 1 MB / parent 100 → 1 MB (add overrides base). */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_response_size = 100;
    add.max_response_size = 1048576;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_response_size, (apr_off_t)1048576);
}

TEST(merge_mrs_child_inherits) {
    /* Base 1 MB + unset add → 1 MB (unset child inherits the parent). */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_response_size = 1048576;
    add.max_response_size = -1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_response_size, (apr_off_t)1048576);
}

TEST(merge_mrs_child_zero_overrides) {
    /* Explicit 0 ("unlimited") must win over a parent limit — 0 is
     * configured, not unset. This is the whole reason the sentinel
     * is -1 and not 0 (as a `>= 0` merge would silently inherit the
     * parent's limit and the operator's "unlimited" would be lost). */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_response_size = 1048576;
    add.max_response_size = 0;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_response_size, (apr_off_t)0);
}

TEST(merge_mrs_both_unset) {
    /* Both unset → -1 sentinel → legacy path; LIBGOMESI leaves 0
     * (unlimited) — byte-identical to pre-#169 behaviour. */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_response_size, (apr_off_t)-1);
}

/* --- MesiMaxConcurrentRequests directive tests (#170) --- */

TEST(mcr_default_unset) {
    /* Fresh config: sentinel -1 (unset). The filter then stays on the
     * legacy parse path and LIBGOMESI leaves MaxConcurrentRequests at
     * 0 — "unlimited", the exact pre-#170 Apache behaviour (the core
     * only installs the admission-control semaphore when > 0,
     * mesi/parser.go). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_zero_accepted) {
    /* AC: MesiMaxConcurrentRequests 0 = unlimited. Unlike
     * MesiTimeout, 0 is a legitimate configured value (mesi/parser.go
     * only installs the semaphore when > 0), so it must parse AND be
     * storable — the -1 sentinel is what keeps it distinct from
     * "unset" so it can override an inherited global cap. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_concurrent_requests(&conf, "0"));
    ASSERT_EQ(conf.max_concurrent_requests, 0);
}

TEST(mcr_typical_accepted) {
    /* AC: MesiMaxConcurrentRequests 5 (the issue's example) — and the
     * functional cap-3 variant runs on vhost 8092. */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_concurrent_requests(&conf, "5"));
    ASSERT_EQ(conf.max_concurrent_requests, 5);
}

TEST(mcr_cap_accepted) {
    /* Boundary: 999999999 (MESI_MAX_MAX_CONCURRENT_REQUESTS) is the
     * configured max — the 9-digit int32-safe bound shared with
     * libgomesi config.MaxMaxConcurrentRequests (neither core nor
     * Caddy caps the value; this is the transport's portable range). */
    mesi_config conf;
    init_config(&conf);
    ASSERT_NULL(set_max_concurrent_requests(&conf, "999999999"));
    ASSERT_EQ(conf.max_concurrent_requests, MESI_MAX_MAX_CONCURRENT_REQUESTS);
    ASSERT_EQ(MESI_MAX_MAX_CONCURRENT_REQUESTS, 999999999);
}

TEST(mcr_cap_plus_one_rejected) {
    /* Boundary: 1000000000 (10 digits) trips parse_nonneg_int's
     * 9-digit overflow guard before any intermediate wraps. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_concurrent_requests(&conf, "1000000000");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_STR_CONTAINS(err, "exceeds");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_negative_rejected) {
    /* AC: negative is an error. The core would only warn
     * ("max_concurrent_requests_invalid") and normalize a negative to
     * 0 = unlimited (#329) — a malformed explicit value must never
     * pass as the documented "unlimited". */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_concurrent_requests(&conf, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_plus_sign_rejected) {
    /* "+3" is rejected — the strict digits-only parser (which skips
     * leading spaces/tabs, like parse_nonneg_int everywhere) requires
     * a plain decimal form; the issue's atoi sketch would accept the
     * sign. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_concurrent_requests(&conf, "+3");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_alpha_rejected) {
    /* atoi("abc") → 0 would silently mean "unlimited". Reject. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_concurrent_requests(&conf, "abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_trailing_garbage_rejected) {
    /* atoi("3foo") → 3 would silently accept. Reject. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_concurrent_requests(&conf, "3foo");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_decimal_rejected) {
    /* Decimals must fail-fast — atoi("2.5") would truncate to 2. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_concurrent_requests(&conf, "2.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_empty_rejected) {
    /* atoi("") → 0 would silently mean "unlimited". Reject. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_concurrent_requests(&conf, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_oversize_rejected) {
    /* 12345678901 (11 digits) — the digit-count guard fires before
     * the range check. Must fail with an error naming the directive. */
    mesi_config conf;
    init_config(&conf);
    const char *err = set_max_concurrent_requests(&conf, "12345678901");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_STR_CONTAINS(err, "exceeds");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(merge_mcr_child_overrides) {
    /* Child 5 / parent 3 → 5 (add overrides base). */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_concurrent_requests = 3;
    add.max_concurrent_requests = 5;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_concurrent_requests, 5);
}

TEST(merge_mcr_child_inherits) {
    /* Base 3 + unset add → 3 (unset child inherits the parent). */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_concurrent_requests = 3;
    add.max_concurrent_requests = -1;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_concurrent_requests, 3);
}

TEST(merge_mcr_child_zero_overrides) {
    /* Explicit 0 ("unlimited") must win over a parent cap — 0 is
     * configured, not unset. This is the whole reason the sentinel
     * is -1 and not 0 (a `>= 0` merge would silently inherit the
     * parent's cap and the operator's "unlimited" would be lost). */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);
    base.max_concurrent_requests = 3;
    add.max_concurrent_requests = 0;

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_concurrent_requests, 0);
}

TEST(merge_mcr_both_unset) {
    /* Both unset → -1 sentinel → legacy path; LIBGOMESI leaves 0
     * (unlimited) — byte-identical to pre-#170 behaviour. */
    mesi_config base, add, merged;
    init_config(&base);
    init_config(&add);
    init_config(&merged);

    merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_concurrent_requests, -1);
}

int main(int argc, char *argv[]) {
    printf("=== Apache Module Directive Unit Tests ===\n\n");

    apr_initialize();
    apr_pool_create(&pool, NULL);

    printf("Testing set_allowed_hosts():\n");
    RUN_TEST(single_hostname);
    RUN_TEST(multiple_hostnames_space);
    RUN_TEST(multiple_hostnames_tab);
    RUN_TEST(mixed_whitespace);
    RUN_TEST(empty_string);
    RUN_TEST(whitespace_only);

    printf("\nTesting set_block_private_ips():\n");
    RUN_TEST(block_private_on);
    RUN_TEST(block_private_off);

    printf("\nTesting merge_server_config():\n");
    RUN_TEST(merge_child_overrides);
    RUN_TEST(merge_child_inherits);
    RUN_TEST(merge_allowed_hosts_child_set);

    printf("\nTesting set_allow_private_for_allowed() (#168):\n");
    RUN_TEST(allow_private_for_allowed_default_unset);
    RUN_TEST(allow_private_for_allowed_on);
    RUN_TEST(allow_private_for_allowed_off);
    RUN_TEST(merge_allow_private_for_allowed_child_overrides);
    RUN_TEST(merge_allow_private_for_allowed_child_inherits);

    printf("\nTesting set_shared_http_client() (#178):\n");
    RUN_TEST(shared_http_client_default_unset);
    RUN_TEST(shared_http_client_on);
    RUN_TEST(shared_http_client_off);
    RUN_TEST(merge_shared_http_client_child_overrides);
    RUN_TEST(merge_shared_http_client_child_inherits);

    printf("\nTesting set_cache_backend() (#174):\n");
    RUN_TEST(cache_backend_memory);
    RUN_TEST(cache_backend_empty_disables);
    RUN_TEST(cache_backend_unknown_rejected);
    // (#175): "redis_rejected" test renamed -> "redis_accepted" (and added
    // memcached variant) — see below.

    printf("\nTesting set_cache_size() (#174):\n");
    RUN_TEST(cache_size_default_unset);
    RUN_TEST(cache_size_valid);
    RUN_TEST(cache_size_min_accepted);
    RUN_TEST(cache_size_zero_rejected);
    RUN_TEST(cache_size_negative_rejected);
    RUN_TEST(cache_size_max_accepted);
    RUN_TEST(cache_size_max_plus_one_rejected);
    RUN_TEST(cache_size_decimal_rejected);
    RUN_TEST(cache_size_alpha_rejected);
    RUN_TEST(cache_size_oversized_rejected);
    RUN_TEST(cache_size_with_leading_space);
    RUN_TEST(cache_size_with_leading_plus_rejected);
    RUN_TEST(cache_size_empty_rejected);

    printf("\nTesting set_cache_ttl() (#174):\n");
    RUN_TEST(cache_ttl_zero_accepted);
    RUN_TEST(cache_ttl_valid);
    RUN_TEST(cache_ttl_max_accepted);
    RUN_TEST(cache_ttl_max_plus_one_rejected);
    RUN_TEST(cache_ttl_negative_rejected);
    RUN_TEST(cache_ttl_decimal_rejected);
    RUN_TEST(cache_ttl_alpha_rejected);

    printf("\nTesting merge_server_config() cache fields (#174):\n");
    RUN_TEST(merge_cache_backend_child_overrides);
    RUN_TEST(merge_cache_backend_child_inherits);
    RUN_TEST(merge_cache_size_child_overrides);
    RUN_TEST(merge_cache_size_child_inherits);
    RUN_TEST(merge_cache_ttl_child_overrides);
    RUN_TEST(merge_cache_ttl_child_inherits);

    printf("\nTesting set_cache_backend() (#175):\n");
    RUN_TEST(cache_backend_redis_accepted);
    RUN_TEST(cache_backend_memcached_accepted);
    RUN_TEST(cache_backend_unknown_variant_rejected);

    printf("\nTesting set_cache_redis_addr() (#175):\n");
    RUN_TEST(redis_addr_default_unset);
    RUN_TEST(redis_addr_valid);
    RUN_TEST(redis_addr_localhost_default_port);
    RUN_TEST(redis_addr_hostname_with_port);
    RUN_TEST(redis_addr_empty_clears);
    RUN_TEST(redis_addr_missing_port_rejected);
    RUN_TEST(redis_addr_missing_host_rejected);
    RUN_TEST(redis_addr_missing_port_value_rejected);
    RUN_TEST(redis_addr_port_zero_rejected);
    RUN_TEST(redis_addr_port_max_accepted);
    RUN_TEST(redis_addr_port_max_plus_one_rejected);
    RUN_TEST(redis_addr_port_negative_rejected);
    RUN_TEST(redis_addr_with_whitespace_rejected);
    RUN_TEST(redis_addr_with_quote_rejected);
    RUN_TEST(redis_addr_with_backslash_rejected);
    RUN_TEST(redis_addr_null_rejected);
    RUN_TEST(redis_addr_ipv6_localhost_accepted);

    printf("\nTesting set_cache_redis_password() (#175):\n");
    RUN_TEST(redis_password_default_unset);
    RUN_TEST(redis_password_empty_accepted);
    RUN_TEST(redis_password_valid);
    RUN_TEST(redis_password_with_special_chars_accepted);
    RUN_TEST(redis_password_with_control_char_rejected);
    RUN_TEST(redis_password_null_accepted);

    printf("\nTesting set_cache_redis_db() (#175):\n");
    RUN_TEST(redis_db_default_unset);
    RUN_TEST(redis_db_zero_accepted);
    RUN_TEST(redis_db_valid);
    RUN_TEST(redis_db_max_accepted);
    RUN_TEST(redis_db_max_plus_one_rejected);
    RUN_TEST(redis_db_negative_rejected);
    RUN_TEST(redis_db_decimal_rejected);
    RUN_TEST(redis_db_empty_rejected);
    RUN_TEST(redis_db_oversized_rejected);

    printf("\nTesting merge_server_config() redis fields (#175):\n");
    RUN_TEST(merge_redis_addr_child_overrides);
    RUN_TEST(merge_redis_addr_child_inherits);
    RUN_TEST(merge_redis_db_child_overrides);
    RUN_TEST(merge_redis_db_child_inherits);
    RUN_TEST(merge_redis_password_child_overrides);

    printf("\nTesting set_cache_memcached_servers() (#176):\n");
    RUN_TEST(memcached_servers_default_unset);
    RUN_TEST(memcached_servers_single);
    RUN_TEST(memcached_servers_multiple);
    RUN_TEST(memcached_servers_mixed_whitespace);
    RUN_TEST(memcached_servers_empty_list_rejected);
    RUN_TEST(memcached_servers_whitespace_only_rejected);
    RUN_TEST(memcached_servers_null_arg_rejected);
    RUN_TEST(memcached_servers_default_port_explicit_rejected);
    RUN_TEST(memcached_servers_only_colon_rejected);
    RUN_TEST(memcached_servers_missing_port_value_rejected);
    RUN_TEST(memcached_servers_port_zero_rejected);
    RUN_TEST(memcached_servers_port_max_accepted);
    RUN_TEST(memcached_servers_port_max_plus_one_rejected);
    RUN_TEST(memcached_servers_negative_port_rejected);
    RUN_TEST(memcached_servers_decimal_port_rejected);
    RUN_TEST(memcached_servers_alpha_port_rejected);
    RUN_TEST(memcached_servers_internal_whitespace_rejected);
    RUN_TEST(memcached_servers_backslash_rejected);
    RUN_TEST(memcached_servers_control_char_rejected);
    RUN_TEST(memcached_servers_max_count_accepted);
    RUN_TEST(memcached_servers_over_max_rejected);
    RUN_TEST(memcached_servers_ipv6_accepted);
    RUN_TEST(memcached_servers_append_to_existing_in_field);

    printf("\nTesting merge_server_config() memcached fields (#176):\n");
    RUN_TEST(merge_memcached_servers_child_overrides);
    RUN_TEST(merge_memcached_servers_child_inherits);

    printf("\nTesting set_cache_key_template() (#177):\n");
    RUN_TEST(cache_key_template_default_null);
    RUN_TEST(cache_key_template_valid_simple);
    RUN_TEST(cache_key_template_with_header);
    RUN_TEST(cache_key_template_with_cookie);
    RUN_TEST(cache_key_template_with_both);
    RUN_TEST(cache_key_template_unknown_placeholder_literal);
    RUN_TEST(cache_key_template_empty_clears);
    RUN_TEST(cache_key_template_null_arg_rejected);
    RUN_TEST(cache_key_template_control_rejected);
    RUN_TEST(cache_key_template_mangled_double_colon_rejected);
    RUN_TEST(cache_key_template_trailing_colon_rejected);
    RUN_TEST(cache_key_template_escaped_dollar_accepted);
    RUN_TEST(cache_key_template_mid_position_accepted);
    RUN_TEST(cache_key_template_too_long_rejected);
    RUN_TEST(merge_cache_key_template_child_overrides);
    RUN_TEST(merge_cache_key_template_child_inherits);

    printf("\nTesting set_max_depth() (#166):\n");
    RUN_TEST(max_depth_default_unset);
    RUN_TEST(max_depth_three_accepted);
    RUN_TEST(max_depth_zero_accepted);
    RUN_TEST(max_depth_hundred_accepted);
    RUN_TEST(max_depth_max_accepted);
    RUN_TEST(max_depth_negative_rejected);
    RUN_TEST(max_depth_alpha_rejected);
    RUN_TEST(max_depth_trailing_garbage_rejected);
    RUN_TEST(max_depth_empty_rejected);
    RUN_TEST(max_depth_decimal_rejected);
    RUN_TEST(max_depth_oversize_rejected);
    RUN_TEST(merge_max_depth_child_overrides);
    RUN_TEST(merge_max_depth_child_inherits);
    RUN_TEST(merge_max_depth_child_zero_overrides);

    printf("\nTesting set_timeout() (#167):\n");
    RUN_TEST(timeout_default_unset);
    RUN_TEST(timeout_ten_accepted);
    RUN_TEST(timeout_min_accepted);
    RUN_TEST(timeout_max_accepted);
    RUN_TEST(timeout_max_plus_one_rejected);
    RUN_TEST(timeout_zero_rejected);
    RUN_TEST(timeout_negative_rejected);
    RUN_TEST(timeout_alpha_rejected);
    RUN_TEST(timeout_trailing_garbage_rejected);
    RUN_TEST(timeout_decimal_rejected);
    RUN_TEST(timeout_empty_rejected);
    RUN_TEST(timeout_oversize_rejected);

    printf("\nTesting merge_server_config() timeout fields (#167):\n");
    RUN_TEST(merge_timeout_child_overrides);
    RUN_TEST(merge_timeout_child_inherits);
    RUN_TEST(merge_timeout_both_unset);

    printf("\nTesting set_max_response_size() (#169):\n");
    RUN_TEST(mrs_default_unset);
    RUN_TEST(mrs_zero_accepted);
    RUN_TEST(mrs_small_accepted);
    RUN_TEST(mrs_one_mb_accepted);
    RUN_TEST(mrs_max_accepted);
    RUN_TEST(mrs_max_plus_one_rejected);
    RUN_TEST(mrs_negative_rejected);
    RUN_TEST(mrs_plus_sign_rejected);
    RUN_TEST(mrs_alpha_rejected);
    RUN_TEST(mrs_trailing_garbage_rejected);
    RUN_TEST(mrs_decimal_rejected);
    RUN_TEST(mrs_empty_rejected);
    RUN_TEST(mrs_oversize_rejected);

    printf("\nTesting merge_server_config() max_response_size fields (#169):\n");
    RUN_TEST(merge_mrs_child_overrides);
    RUN_TEST(merge_mrs_child_inherits);
    RUN_TEST(merge_mrs_child_zero_overrides);
    RUN_TEST(merge_mrs_both_unset);

    printf("\nTesting set_max_concurrent_requests() (#170):\n");
    RUN_TEST(mcr_default_unset);
    RUN_TEST(mcr_zero_accepted);
    RUN_TEST(mcr_typical_accepted);
    RUN_TEST(mcr_cap_accepted);
    RUN_TEST(mcr_cap_plus_one_rejected);
    RUN_TEST(mcr_negative_rejected);
    RUN_TEST(mcr_plus_sign_rejected);
    RUN_TEST(mcr_alpha_rejected);
    RUN_TEST(mcr_trailing_garbage_rejected);
    RUN_TEST(mcr_decimal_rejected);
    RUN_TEST(mcr_empty_rejected);
    RUN_TEST(mcr_oversize_rejected);

    printf("\nTesting merge_server_config() max_concurrent_requests fields (#170):\n");
    RUN_TEST(merge_mcr_child_overrides);
    RUN_TEST(merge_mcr_child_inherits);
    RUN_TEST(merge_mcr_child_zero_overrides);
    RUN_TEST(merge_mcr_both_unset);


    apr_pool_destroy(pool);

    apr_terminate();

    printf("\n=== Results: %d passed, %d failed ===\n", tests_passed, tests_failed);

    return tests_failed > 0 ? 1 : 0;
}
