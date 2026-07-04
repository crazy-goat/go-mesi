#include "httpd.h"
#include "http_config.h"
#include "http_protocol.h"
#include "http_request.h"
#include "http_core.h"
#include "http_log.h"
#include "util_filter.h"
#include "apr_strings.h"

#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>

#ifndef LIB_GOMESI_PATH
#define LIB_GOMESI_PATH "/usr/lib/libgomesi.so"
#endif

typedef char *(*ParseFunc)(char *, int, char *);
typedef char *(*ParseWithConfigFunc)(char *, int, char *, char *, int);
typedef void (*FreeFunc)(char *);
typedef int (*InitCacheFunc)(char *, int, int);
typedef int (*InitCacheWithConfigFunc)(char *, int, int, char *);
typedef void (*FreeCacheFunc)(void);

static void *go_module = NULL;
static ParseFunc EsiParse = NULL;
static ParseWithConfigFunc EsiParseWithConfig = NULL;
static FreeFunc EsiFreeString = NULL;
static InitCacheFunc EsiInitCache = NULL;
static InitCacheWithConfigFunc EsiInitCacheWithConfig = NULL;
static FreeCacheFunc EsiFreeCache = NULL;
// Tracks whether EsiInitCache has already been called for this worker
// process. libgomesi keeps cache state in package-level vars; calling
// InitCache() multiple times resets it, so we only invoke it once and
// guard subsequent requests. Reset to 0 in mesi_child_cleanup().
static int cache_initialized = 0;

// Test-only: set MESI_FORCE_FLATTEN_ERROR=1 in the environment to force
// flatten_brigade() to return 0, simulating a brigade flatten failure.
static int force_flatten_error = 0;

typedef struct {
    apr_bucket_brigade *bb;
} response_filter_ctx;

module AP_MODULE_DECLARE_DATA mesi_module;

typedef struct {
    int enable_mesi;
    apr_array_header_t *allowed_hosts;
    int block_private_ips;  // -1=unset, 0=off, 1=on
    // Cached URI of the merged server config that owns the active
    // cache settings. Each child process uses this to lazy-init
    // InitCache once per cache_backend on first request, then skips.
    const char *cache_backend;        // "" (off) | "memory" | "redis" | "memcached"
    int cache_size;                   // >0 = configured, 0 = unset (default 10000)
    int cache_ttl;                    // seconds; >=0 = configured, -1 = unset
    const char *cache_redis_addr;     // "host:port" or NULL = unset (default localhost:6379)
    const char *cache_redis_password; // "" or NULL = unset (default no auth)
    int cache_redis_db;               // -1 = unset, >=0 = selected DB (Redis max 16)
} mesi_config;

// Default memory cache size when MesiCacheSize is not set.
// Matches libgomesi.InitCache default (10000 entries).
#define MESI_DEFAULT_CACHE_SIZE 10000

// Allow up to 1M entries / 24h TTL / Redis DB 0..15 to keep configs in
// sensible range and avoid silent overflow feeding libgomesi cache
// internals.
#define MESI_MAX_CACHE_SIZE 1000000
#define MESI_MAX_CACHE_TTL_SECONDS (24 * 60 * 60)
#define MESI_MAX_REDIS_DB 15

static void *create_server_config(apr_pool_t *p, server_rec *s) {
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    conf->enable_mesi = 0;
    conf->allowed_hosts = apr_array_make(p, 4, sizeof(const char *));
    conf->block_private_ips = -1;  // -1 = unset, default will be applied in filter
    conf->cache_backend = "";
    conf->cache_size = 0;
    conf->cache_ttl = -1;  // -1 = unset (no expiry)
    conf->cache_redis_addr = NULL;
    conf->cache_redis_password = NULL;
    conf->cache_redis_db = -1;  // -1 = unset, default 0 in libgomesi
    return conf;
}

static void *merge_server_config(apr_pool_t *p, void *basev, void *addv) {
    mesi_config *base = (mesi_config *) basev;
    mesi_config *add = (mesi_config *) addv;
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    conf->enable_mesi = (add->enable_mesi != 0) ? add->enable_mesi : base->enable_mesi;
    conf->allowed_hosts = (add->allowed_hosts->nelts > 0) ? add->allowed_hosts : base->allowed_hosts;
    conf->block_private_ips = (add->block_private_ips != -1) ? add->block_private_ips : base->block_private_ips;
    // Cache config: child overrides parent when child explicitly sets a
    // backend ("" means "inherit from base"); size/ttl use 0 (unconfigured)
    // sentinel so add's explicit value wins over base's explicit value.
    conf->cache_backend = (add->cache_backend && add->cache_backend[0] != '\0')
                           ? add->cache_backend
                           : base->cache_backend;
    conf->cache_size = (add->cache_size > 0) ? add->cache_size : base->cache_size;
    conf->cache_ttl = (add->cache_ttl >= 0) ? add->cache_ttl : base->cache_ttl;
    // Redis config: child overrides parent when child explicitly sets a
    // non-NULL value; DB uses -1 sentinel for "unset".
    conf->cache_redis_addr = add->cache_redis_addr ? add->cache_redis_addr : base->cache_redis_addr;
    conf->cache_redis_password = add->cache_redis_password ? add->cache_redis_password : base->cache_redis_password;
    conf->cache_redis_db = (add->cache_redis_db >= 0) ? add->cache_redis_db : base->cache_redis_db;
    return conf;
}

static apr_status_t mesi_child_cleanup(void *data) {
    if (EsiFreeCache) {
        EsiFreeCache();
    }
    if (go_module) {
        dlclose(go_module);
        go_module = NULL;
    }
    EsiParse = NULL;
    EsiParseWithConfig = NULL;
    EsiFreeString = NULL;
    EsiInitCache = NULL;
    EsiInitCacheWithConfig = NULL;
    EsiFreeCache = NULL;
    cache_initialized = 0;
    return APR_SUCCESS;
}

static void mesi_child_init(apr_pool_t *p, server_rec *s) {
    char *env_force = getenv("MESI_FORCE_FLATTEN_ERROR");
    if (env_force && env_force[0] == '1' && env_force[1] == '\0') {
        force_flatten_error = 1;
        ap_log_error(APLOG_MARK, APLOG_WARNING, 0, s,
            "mesi: MESI_FORCE_FLATTEN_ERROR=1 - flatten errors will be forced (test mode)");
    }

    // RTLD_GLOBAL is required for Go's runtime (signal handlers, etc.)
    // Without it, Go's runtime initialization may fail or behave incorrectly
    go_module = dlopen(LIB_GOMESI_PATH, RTLD_NOW | RTLD_GLOBAL);
    if (!go_module) {
        char *err = dlerror();
        ap_log_error(APLOG_MARK, APLOG_ERR, 0, s,
                     "mesi: dlopen(%s) failed: %s", LIB_GOMESI_PATH, err ? err : "(unknown error)");
        return;
    }

    // Resolve symbols defensively. dlerror() must be cleared before
    // probing so a NULL result from dlerror() after dlsym() means
    // "found", not "stale error from earlier lookup". Treat all five
    // as optional — required ones (Parse*/FreeString) are checked below,
    // InitCache/FreeCache are optional and just downgraded to a warning
    // at request time when missing.
    (void) dlerror();
    EsiParse = (ParseFunc)dlsym(go_module, "Parse");
    if (dlerror() != NULL) {
        EsiParse = NULL;
        (void) dlerror();
    }
    EsiParseWithConfig = (ParseWithConfigFunc)dlsym(go_module, "ParseWithConfig");
    if (dlerror() != NULL) {
        EsiParseWithConfig = NULL;
        (void) dlerror();
    }
    EsiFreeString = (FreeFunc)dlsym(go_module, "FreeString");
    if (dlerror() != NULL) {
        EsiFreeString = NULL;
        (void) dlerror();
    }
    EsiInitCache = (InitCacheFunc)dlsym(go_module, "InitCache");
    if (dlerror() != NULL) {
        EsiInitCache = NULL;
        (void) dlerror();
    }
    EsiInitCacheWithConfig = (InitCacheWithConfigFunc)dlsym(go_module, "InitCacheWithConfig");
    if (dlerror() != NULL) {
        EsiInitCacheWithConfig = NULL;
        (void) dlerror();
    }
    EsiFreeCache = (FreeCacheFunc)dlsym(go_module, "FreeCache");
    if (dlerror() != NULL) {
        EsiFreeCache = NULL;
        (void) dlerror();
    }

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
        EsiInitCache = NULL;
        EsiInitCacheWithConfig = NULL;
        EsiFreeCache = NULL;
        return;
    }

    apr_pool_cleanup_register(p, NULL, mesi_child_cleanup, apr_pool_cleanup_null);
}

// build_redis_config_json renders the mesi_config Redis fields into a
// JSON blob compatible with libgomesi.InitCacheWithConfig. Returns
// NULL if backend is not "redis" (caller must short-circuit).
// Memory is allocated from the request pool (short-lived: feeds one cgo
// call). The numeric DB field is omitted when unset (libgomesi default
// 0). The password is omitted when NULL.
static const char *build_redis_config_json(mesi_config *conf, apr_pool_t *pool) {
    if (!conf->cache_backend || strcmp(conf->cache_backend, "redis") != 0) {
        return NULL;
    }
    const char *addr = conf->cache_redis_addr
                       ? conf->cache_redis_addr
                       : "localhost:6379";
    // Escape any embedded '"' or '\' so a misconfig password can't inject
    // JSON keys. Use a small stack buffer; bail out via apr_psprintf
    // for oversized strings (apr_psprintf does allocation regardless).
    apr_size_t pwd_len = conf->cache_redis_password
                          ? strlen(conf->cache_redis_password)
                          : 0;
    // Worst-case: every char is escaped (×2) + 2 quotes.
    char *pwd_esc = NULL;
    if (pwd_len > 0) {
        apr_size_t esc_cap = pwd_len * 2 + 3;  // + '"' + '"' + NUL
        pwd_esc = apr_palloc(pool, esc_cap);
        char *w = pwd_esc;
        *w++ = '"';
        const char *r = conf->cache_redis_password;
        while (*r) {
            if (*r == '"' || *r == '\\') *w++ = '\\';
            *w++ = *r++;
        }
        *w++ = '"';
        *w = '\0';
    }
    // Escape addr the same way (host:port shouldn't contain JSON meta
    // characters, but a hostile config could).
    apr_size_t addr_len = strlen(addr);
    apr_size_t addr_cap = addr_len * 2 + 3;
    char *addr_esc = apr_palloc(pool, addr_cap);
    char *w = addr_esc;
    *w++ = '"';
    const char *r = addr;
    while (*r) {
        if (*r == '"' || *r == '\\') *w++ = '\\';
        *w++ = *r++;
    }
    *w++ = '"';
    *w = '\0';

    if (conf->cache_redis_db >= 0) {
        return apr_psprintf(pool,
            "{\"redisAddr\":%s,\"redisPassword\":%s,\"redisDB\":%d}",
            addr_esc,
            pwd_esc ? pwd_esc : "\"\"",
            conf->cache_redis_db);
    }
    return apr_psprintf(pool,
        "{\"redisAddr\":%s,\"redisPassword\":%s}",
        addr_esc,
        pwd_esc ? pwd_esc : "\"\"");
}

// build_redis_config_json_max verifies the rendered JSON is small enough
// to safely copy into a stack-friendly buffer. libgomesi.sscanf-style
// parsers don't artificially cap; this is a sanity check for operators.
// 4 KB is more than enough for a host:port + a password.
#define MESI_MAX_REDIS_CONFIG_JSON 4096

// mesi_init_cache lazily initializes the shared cache for this worker
// process. Called once per process from mesi_response_filter when
// caching is enabled. Returns 0 on success or "no cache configured";
// -1 if InitCache rejected the configuration (already logged).
// For backends that require extra configuration ("redis", "memcached")
// this uses the InitCacheWithConfig entry point passing backend-specific
// JSON. For "memory", it uses the original InitCache so existing
// libs without InitCacheWithConfig keep working.
static int mesi_init_cache(mesi_config *conf, request_rec *r) {
    if (cache_initialized) {
        return 0;
    }
    if (!conf->cache_backend || conf->cache_backend[0] == '\0') {
        return 0;  // Cache disabled — nothing to do.
    }
    cache_initialized = 1;  // Mark before probing so a failing dlsym is not retried.

    int size = conf->cache_size > 0 ? conf->cache_size : MESI_DEFAULT_CACHE_SIZE;
    int ttl  = conf->cache_ttl >= 0 ? conf->cache_ttl : 0;

    int needs_config = (strcmp(conf->cache_backend, "redis") == 0)
                    || (strcmp(conf->cache_backend, "memcached") == 0);
    if (needs_config) {
        // Resolve InitCacheWithConfig lazily — mirrors the InitCache
        // fallback below in case the first request arrives before
        // child_init finished probing all symbols.
        if (!EsiInitCacheWithConfig) {
            if (go_module) {
                (void) dlerror();
                EsiInitCacheWithConfig = (InitCacheWithConfigFunc)
                    dlsym(go_module, "InitCacheWithConfig");
                if (dlerror() != NULL) {
                    EsiInitCacheWithConfig = NULL;
                }
            }
        }
        if (!EsiInitCacheWithConfig) {
            ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, r,
                "mesi: InitCacheWithConfig symbol not available in libgomesi; "
                "MesiCacheBackend %s requires a newer libgomesi (rebuild). "
                "ESI will run without cache.",
                conf->cache_backend);
            return 0;
        }
    } else if (!EsiInitCache) {
        if (go_module) {
            (void) dlerror();
            EsiInitCache = (InitCacheFunc)dlsym(go_module, "InitCache");
            if (dlerror() != NULL) {
                EsiInitCache = NULL;
            }
        }
        if (!EsiInitCache) {
            ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, r,
                "mesi: InitCache symbol not available in libgomesi; "
                "ESI will run without cache despite MesiCacheBackend %s",
                conf->cache_backend);
            return 0;
        }
    }

    int rc;
    if (needs_config) {
        const char *cfg_json = build_redis_config_json(conf, r->pool);
        if (!cfg_json) {
            // backend was "memcached" — emit a minimal sentinel that
            // libgomesi.InitCacheWithConfig will reject. We DO NOT
            // silently fall back to memory.
            ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, r,
                "mesi: cache backend %s lacks server-list configuration; "
                "ESI will run without cache",
                conf->cache_backend);
            cache_initialized = 0;  // Allow next request to retry.
            return -1;
        }
        if (strlen(cfg_json) > MESI_MAX_REDIS_CONFIG_JSON) {
            ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, r,
                "mesi: rendered cache config JSON exceeds %d bytes; "
                "refusing to init cache",
                MESI_MAX_REDIS_CONFIG_JSON);
            cache_initialized = 0;
            return -1;
        }
        rc = EsiInitCacheWithConfig((char *)conf->cache_backend, size, ttl,
                                    (char *)cfg_json);
    } else {
        rc = EsiInitCache((char *)conf->cache_backend, size, ttl);
    }
    if (rc != 0) {
        ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, r,
            "mesi: InitCache(backend=%s, size=%d, ttl=%d) returned %d; "
            "ESI will run without cache",
            conf->cache_backend, size, ttl, rc);
        cache_initialized = 0;  // Allow next request to retry.
        return -1;
    }
    ap_log_rerror(APLOG_MARK, APLOG_NOTICE, 0, r,
        "mesi: cache initialized (backend=%s, size=%d, ttl=%ds)",
        conf->cache_backend, size, ttl);
    return 0;
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

// Parse a non-negative decimal integer from arg. Reject empty input,
// non-digit characters (including '-', '+', '.') — fail-fast instead of
// silently coercing via strtol, and values outside [min, max].
// Returns NULL on success (parsed value stored in *out) or an
// Apache-pool-allocated error string suitable as set_* return value.
static const char *parse_nonneg_int(apr_pool_t *pool, const char *arg, int min, int max, int *out) {
    const char *p = arg ? arg : "";
    // Skip leading spaces and tabs only (no newlines per Apache directive).
    while (*p == ' ' || *p == '\t') p++;
    if (*p == '\0') {
        return apr_psprintf(pool,
            "MesiCache* requires a non-negative integer argument");
    }
    const char *digits = p;
    while (*p >= '0' && *p <= '9') p++;
    if (*p != '\0') {
        return apr_psprintf(pool,
            "MesiCache* must be a non-negative integer (got: %s)", arg);
    }
    if (digits == p) {
        return apr_psprintf(pool,
            "MesiCache* must contain at least one digit (got: %s)", arg);
    }
    // Compute length and compare without atoi to catch overflow cheaply.
    size_t n = (size_t)(p - digits);
    if (n > 9) {
        // 9 digits fits in 1_000_000_000; reject anything longer to
        // guarantee we stay inside int32 range (max is 2_147_483_647,
        // which is 10 digits, but we cap at MESI_MAX_* anyway).
        return apr_psprintf(pool,
            "MesiCache* value %s exceeds maximum allowed (%d)", arg, max);
    }
    long val = 0;
    for (size_t i = 0; i < n; i++) {
        val = val * 10 + (digits[i] - '0');
    }
    if (val < min || val > max) {
        return apr_psprintf(pool,
            "MesiCache* value %s out of range [%d, %d]", arg, min, max);
    }
    *out = (int)val;
    return NULL;
}

static const char *set_cache_backend(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    if (!arg) {
        return "MesiCacheBackend requires an argument (use empty string to disable)";
    }
    // Reject anything outside the supported set so a typo doesn't silently
    // fall back to "no cache" (which would change behavior without
    // operator awareness). Backends: "memory", "redis", "memcached".
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
    return apr_psprintf(cmd->pool,
        "MesiCacheBackend: unknown backend %s "
        "(supported: \"memory\", \"redis\", \"memcached\", or empty)",
        arg);
}

static const char *set_cache_size(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int v = 0;
    const char *err = parse_nonneg_int(cmd->pool, arg, 1, MESI_MAX_CACHE_SIZE, &v);
    if (err) {
        return apr_psprintf(cmd->pool,
            "MesiCacheSize: %s", err);
    }
    conf->cache_size = v;
    return NULL;
}

static const char *set_cache_ttl(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int v = 0;
    const char *err = parse_nonneg_int(cmd->pool, arg, 0, MESI_MAX_CACHE_TTL_SECONDS, &v);
    if (err) {
        return apr_psprintf(cmd->pool,
            "MesiCacheTTL: %s", err);
    }
    conf->cache_ttl = v;
    return NULL;
}

// MesiCacheRedisAddr — host:port pair. Empty value clears any
// previously-set addr (treated as "use default localhost:6379").
// We use a tiny "loose" check: must contain ':' followed by digits
// (port 1..65535). Reject hostnames containing whitespace, control
// chars, etc. to keep the address safe to embed in JSON.
static const char *set_cache_redis_addr(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    if (!arg) {
        return "MesiCacheRedisAddr requires a host:port argument";
    }
    // Empty arg → unset (use default).
    if (arg[0] == '\0') {
        conf->cache_redis_addr = NULL;
        return NULL;
    }
    // Disallow embedded whitespace, control chars, or JSON meta chars
    // in the address — the value gets serialized into JSON.
    for (const char *p = arg; *p; p++) {
        unsigned char c = (unsigned char)*p;
        if (c == ' ' || c == '\t' || c == '"' || c == '\\' || c < 0x20) {
            return apr_psprintf(cmd->pool,
                "MesiCacheRedisAddr: invalid character %d in %s",
                (int)c, arg);
        }
    }
    // Find last ':' (IPv6 addresses use [...] or no port — we keep it
    // simple: must contain a colon, port part must be digits in 1..65535).
    const char *colon = strrchr(arg, ':');
    if (!colon || colon == arg || *(colon + 1) == '\0') {
        return apr_psprintf(cmd->pool,
            "MesiCacheRedisAddr: must be host:port (got: %s)", arg);
    }
    // Validate port is a positive decimal in [1, 65535].
    int port = 0;
    const char *err = parse_nonneg_int(cmd->pool, colon + 1, 1, 65535, &port);
    if (err) {
        return apr_psprintf(cmd->pool,
            "MesiCacheRedisAddr: port invalid: %s", arg);
    }
    conf->cache_redis_addr = apr_pstrdup(cmd->pool, arg);
    return NULL;
}

// MesiCacheRedisPassword — raw Redis AUTH password. We do NOT log
// the password value on error (don't leak creds into error.log).
// Empty arg clears the password.
static const char *set_cache_redis_password(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    if (!arg) {
        // AP_INIT_TAKE1 args are never NULL per Apache directive contract,
        // but guard anyway — silently treating NULL as "clear" would mask
        // misconfiguration. Treat NULL as an explicit empty string.
        conf->cache_redis_password = "";
        return NULL;
    }
    // Reject embedded control chars (< 0x20) and JSON-meta chars
    // (',",\\,<,>,&) only for control-char detection). Quotes/backslashes
    // are explicitly escaped by build_redis_config_json, so they're OK.
    for (const char *p = arg; *p; p++) {
        unsigned char c = (unsigned char)*p;
        if (c < 0x20) {
            return apr_psprintf(cmd->pool,
                "MesiCacheRedisPassword: invalid control character 0x%02x in value",
                (unsigned)c);
        }
    }
    conf->cache_redis_password = apr_pstrdup(cmd->pool, arg);
    return NULL;
}

// MesiCacheRedisDB — Redis logical database number. 0..15 (Redis
// default config; redis.conf "databases 16"). Negatives are rejected.
static const char *set_cache_redis_db(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int v = -1;
    const char *err = parse_nonneg_int(cmd->pool, arg, 0, MESI_MAX_REDIS_DB, &v);
    if (err) {
        return apr_psprintf(cmd->pool,
            "MesiCacheRedisDB: %s", err);
    }
    conf->cache_redis_db = v;
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

// Flatten brigade into a single NUL-terminated string.
// Returns 1 on success, 0 on failure.
// On failure, *html is set to NULL (no dangling pointer to uninitialized memory)
// and *len is set to the brigade size (0 if empty or length call failed).
//
// Contract for the fallback path (caller when returns 0):
//   - brigade is NOT modified (caller appends EOS and passes through)
//   - no ESI processing is performed
//   - caller can use len > 0 to decide whether to log a warning
//     (non-zero len means flatten failed despite having data)
//
// Synthetic failure injection: checked once at child_init via
// MESI_FORCE_FLATTEN_ERROR=1 env var (stored in static force_flatten_error).
static int flatten_brigade(apr_bucket_brigade *bb, char **html, apr_size_t *len, apr_pool_t *pool) {
    if (force_flatten_error) {
        *html = NULL;
        apr_brigade_length(bb, 1, len);
        return 0;
    }

    if (apr_brigade_length(bb, 1, len) == APR_SUCCESS && *len > 0) {
        *html = apr_palloc(pool, *len + 1);
        apr_size_t copied = *len;
        if (apr_brigade_flatten(bb, *html, &copied) == APR_SUCCESS) {
            (*html)[copied] = '\0';
            return 1;
        }
        *html = NULL;
    }
    return 0;
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
    int flatten_ok = flatten_brigade(ctx->bb, &html, &len, f->r->pool);

    if (!flatten_ok) {
        APR_BRIGADE_INSERT_TAIL(ctx->bb, apr_bucket_eos_create(ctx->bb->bucket_alloc));
        if (len > 0) {
            ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                "mesi: failed to flatten response body (%lu bytes), skipping ESI processing",
                (unsigned long)len);
        }
        return ap_pass_brigade(f->next, ctx->bb);
    }

    // Initialize shared cache on first request. The cache lives across
    // requests in this worker process; once initialized, repeated
    // calls are no-ops (guarded by cache_initialized).
    if (conf->cache_backend && conf->cache_backend[0] != '\0') {
        /* Errors here are logged; on -1 we proceed without cache. */
        (void) mesi_init_cache(conf, f->r);
    }

    // Build allowed_hosts string from config (O(n) time, single allocation)
    char *allowed_hosts_str = "";
    if (conf->allowed_hosts && conf->allowed_hosts->nelts > 0) {
        apr_array_header_t *arr = conf->allowed_hosts;
        const char **hosts = (const char **)arr->elts;
        apr_size_t total = 0;
        for (int i = 0; i < arr->nelts; i++) {
            total += strlen(hosts[i]);
            if (i > 0) total++;
        }
        char *buf = apr_palloc(f->r->pool, total + 1);
        char *p = buf;
        for (int i = 0; i < arr->nelts; i++) {
            if (i > 0) *p++ = ' ';
            apr_size_t host_len = strlen(hosts[i]);
            memcpy(p, hosts[i], host_len);
            p += host_len;
        }
        *p = '\0';
        allowed_hosts_str = buf;
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
    AP_INIT_TAKE1("MesiCacheBackend", set_cache_backend, NULL, RSRC_CONF, "Cache backend: \"memory\", \"redis\", \"memcached\" (off when empty). Default: off"),
    AP_INIT_TAKE1("MesiCacheSize", set_cache_size, NULL, RSRC_CONF, "Memory cache max entries (1..1000000). Default: 10000"),
    AP_INIT_TAKE1("MesiCacheTTL", set_cache_ttl, NULL, RSRC_CONF, "Memory cache entry TTL in seconds (0..86400). Default: 0 (no expiry)"),
    AP_INIT_TAKE1("MesiCacheRedisAddr", set_cache_redis_addr, NULL, RSRC_CONF, "Redis server address for ESI caching (default: localhost:6379). Used when MesiCacheBackend is redis"),
    AP_INIT_TAKE1("MesiCacheRedisPassword", set_cache_redis_password, NULL, RSRC_CONF, "Redis AUTH password (default: none). Used when MesiCacheBackend is redis"),
    AP_INIT_TAKE1("MesiCacheRedisDB", set_cache_redis_db, NULL, RSRC_CONF, "Redis database number (0..15). Default: 0. Used when MesiCacheBackend is redis"),
    {NULL}
};

module AP_MODULE_DECLARE_DATA mesi_module = {
    STANDARD20_MODULE_STUFF,
    NULL,                 // no per-dir config (server-level only)
    NULL,                 // no per-dir merge
    create_server_config,
    merge_server_config,
    mesi_directives,
    register_hooks
};
