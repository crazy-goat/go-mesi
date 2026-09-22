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
typedef char *(*ParseWithConfigExFunc)(char *, int, char *, char *, int, int);
typedef char *(*ParseWithConfigCtxFunc)(char *, int, char *, char *, int, int, char *, char *);
typedef char *(*ParseJsonFunc)(char *, char *);
typedef void (*FreeFunc)(char *);
typedef int (*InitCacheFunc)(char *, int, int);
typedef int (*InitCacheWithConfigFunc)(char *, int, int, char *);
typedef void (*FreeCacheFunc)(void);
typedef void (*InitHTTPClientFunc)(int);
typedef void (*FreeHTTPClientFunc)(void);

static void *go_module = NULL;
static ParseFunc EsiParse = NULL;
static ParseWithConfigFunc EsiParseWithConfig = NULL;
static ParseWithConfigExFunc EsiParseWithConfigEx = NULL;
static ParseWithConfigCtxFunc EsiParseWithConfigCtx = NULL;
static ParseJsonFunc EsiParseJson = NULL;
static FreeFunc EsiFreeString = NULL;
static InitCacheFunc EsiInitCache = NULL;
static InitCacheWithConfigFunc EsiInitCacheWithConfig = NULL;
static FreeCacheFunc EsiFreeCache = NULL;
static InitHTTPClientFunc EsiInitHTTPClient = NULL;
static FreeHTTPClientFunc EsiFreeHTTPClient = NULL;
// Tracks whether EsiInitCache has already been called for this worker
// process. libgomesi keeps cache state in package-level vars; calling
// InitCache() multiple times resets it, so we only invoke it once and
// guard subsequent requests. Reset to 0 in mesi_child_cleanup().
static int cache_initialized = 0;
// Tracks whether EsiInitHTTPClient has already been called for this worker
// process. libgomesi keeps the shared client in a package-level var; calling
// InitHTTPClient() multiple times resets it, so we only invoke it once and
// guard subsequent requests. Reset to 0 in mesi_child_cleanup().
static int http_client_initialized = 0;

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
    // Allow hosts in allowed_hosts to bypass BlockPrivateIPs (SSRF dial
    // block) when they resolve to private/reserved IPs. -1=unset,
    // 0=off, 1=on. Only effective when BOTH block_private_ips is on AND
    // allowed_hosts is set. Default (unset → off) keeps private IPs
    // always blocked regardless of allowed_hosts membership.
    int allow_private_ips_for_allowed;  // -1=unset, 0=off, 1=on
    // Share a single http.Client across all <esi:include> fetches in this
    // worker process for TCP/TLS connection pooling. -1=unset, 0=off, 1=on.
    // Default (unset → off) keeps the original per-include client creation
    // so behaviour is unchanged unless an operator opts in. The shared
    // client is created in mesi_child_init via libgomesi InitHTTPClient and
    // honours the effective MesiBlockPrivateIPs setting at startup.
    int shared_http_client;  // -1=unset, 0=off, 1=on
    // Cached URI of the merged server config that owns the active
    // cache settings. Each child process uses this to lazy-init
    // InitCache once per cache_backend on first request, then skips.
    const char *cache_backend;        // "" (off) | "memory" | "redis" | "memcached"
    int cache_size;                   // >0 = configured, 0 = unset (default 10000)
    int cache_ttl;                    // seconds; >=0 = configured, -1 = unset
    const char *cache_redis_addr;     // "host:port" or NULL = unset (default localhost:6379)
    const char *cache_redis_password; // "" or NULL = unset (default no auth)
    int cache_redis_db;               // -1 = unset, >=0 = selected DB (Redis max 16)
    // Memcached backend fields (#176). The list is an apr_array_header_t
    // of const char * "host:port" entries. nelts > 0 means a list was
    // configured (even one server is enough). When backend is memcached
    // and the array is empty, InitCacheWithConfig is called without the
    // required "servers" key and libgomesi rejects — that's the same
    // fail-fast path the Redis directives already use for missing config.
    apr_array_header_t *cache_memcached_servers;
    // Cache key template (#177). RSRC_CONF, NULL = use DefaultCacheKey
    // (URL-only). When set, the value is passed to libgomesi's
    // ParseWithConfigCtx together with a JSON request context (headers +
    // cookies) so mesi.BuildCacheKey can evaluate ${url},
    // ${header:Name} and ${cookie:Name} placeholders. Unknown
    // placeholders stay literal; empty/NULL falls back to URL-only for
    // backward compat.
    const char *cache_key_template;  // NULL/empty = DefaultCacheKey
    // ESI nesting depth (#166). -1 = unset (filter uses 5). Explicit 0
    // is valid passthrough (no ESI fetch). Range [0, MESI_MAX_MAX_DEPTH]
    // matches mesi.MaxMaxDepth / Caddy.
    int max_depth;  // -1=unset, >=0 = configured
    // Global per-include fetch budget in seconds (#167). -1 = unset:
    // the filter stays on the legacy parse path and libgomesi applies
    // its default 30s (config.DefaultTimeoutSeconds — the
    // MESI_DEFAULT_TIMEOUT_SECONDS macro below only documents that
    // value, the filter never substitutes it). Range [1,
    // MESI_MAX_TIMEOUT_SECONDS]; 0 is REJECTED at config load: the
    // core treats Timeout <= 0 as "budget already exhausted" and fails
    // every include immediately with ErrTimeBudgetExceeded
    // (mesi/fetch.go) — it does NOT mean "no timeout".
    int timeout_seconds;  // -1=unset, >=1 = configured
    // Cap on a single <esi:include> response body, in bytes (#169).
    // -1 = unset: the filter stays on the legacy parse path and
    // libgomesi leaves EsiParserConfig.MaxResponseSize at 0, which the
    // core treats as "unlimited" (mesi/fetch.go: the limiting branch
    // is MaxResponseSize > 0) — byte-identical to pre-#169 Apache
    // behaviour. There is NO implicit 10 MB default on this path: the
    // 10 MB of mesi.CreateDefaultConfig() only reaches Go callers
    // using that constructor, never libgomesi's positional Parse*
    // entry points. Explicit 0 is a legitimate configured value with
    // the same "unlimited" meaning (documented contract shared with
    // Caddy `max_response_size 0`), so the unset sentinel MUST stay
    // -1 for 0 to survive the merge below. Range [0,
    // MESI_MAX_MAX_RESPONSE_SIZE].
    apr_off_t max_response_size;  // -1=unset, >=0 = configured (bytes)
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
// Memcached: slots enough for a reasonable multi-cluster deployment
// without letting a runaway directive pollute the JSON config blob.
// 64 entries × ~30 ascii chars + JSON wrapping → ~2.5 KB, well under
// MESI_MAX_CACHE_CONFIG_JSON (4 KB).
#define MESI_MAX_MEMCACHED_SERVERS 64
// Cache key template: 4 KB comfortably holds a template with several
// placeholders (e.g. "mesi:${url}:${header:Accept-Language}").
#define MESI_MAX_CACHE_KEY_TEMPLATE 4096
// Global ESI nesting depth (#166). Matches mesi.MaxMaxDepth (10,000).
#define MESI_MAX_MAX_DEPTH 10000
#define MESI_DEFAULT_MAX_DEPTH 5
// Global ESI per-include fetch timeout (#167). Bounds match libgomesi's
// config.MaxTimeoutSeconds (86400 = 24h). Documentation-only mirror of
// libgomesi's config.DefaultTimeoutSeconds (30): an unset MesiTimeout
// keeps the legacy parse path and libgomesi applies 30s Go-side — this
// module never substitutes the macro itself. Keep the value in sync
// with libgomesi/internal/config/timeout.go.
#define MESI_MAX_TIMEOUT_SECONDS (24 * 60 * 60)
#define MESI_DEFAULT_TIMEOUT_SECONDS 30
// Cap on the MesiMaxResponseSize directive (#169). Matches libgomesi's
// config.MaxMaxResponseSize (math.MaxInt64 - 1): the value is an
// int64 byte count the core feeds into io.LimitReader as
// `MaxResponseSize + 1` (mesi/fetch.go) — at math.MaxInt64 that
// bound wraps negative, LimitedReader returns EOF immediately and the
// include would silently render an EMPTY body instead of failing, so
// the largest safe value is MaxInt64 - 1. apr_off_t is int64 on every
// platform Apache 2.4 supports. Keep in sync with
// libgomesi/internal/config/max_response_size.go.
#define MESI_MAX_MAX_RESPONSE_SIZE ((apr_off_t)9223372036854775806LL)

static void *create_server_config(apr_pool_t *p, server_rec *s) {
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    conf->enable_mesi = 0;
    conf->allowed_hosts = apr_array_make(p, 4, sizeof(const char *));
    conf->block_private_ips = -1;  // -1 = unset, default will be applied in filter
    conf->allow_private_ips_for_allowed = -1;  // -1 = unset, default off
    conf->shared_http_client = -1;  // -1 = unset, default off
    conf->cache_backend = "";
    conf->cache_size = 0;
    conf->cache_ttl = -1;  // -1 = unset (no expiry)
    conf->cache_redis_addr = NULL;
    conf->cache_redis_password = NULL;
    conf->cache_redis_db = -1;  // -1 = unset, default 0 in libgomesi
    // Memcached: empty list means "no server list configured". The
    // set_cache_memcached_servers directive is the only path that adds
    // entries; an empty list at request time triggers the runtime
    // fail-fast error rather than silently picking some default server.
    conf->cache_memcached_servers = apr_array_make(p, 2, sizeof(const char *));
    conf->cache_key_template = NULL;
    conf->max_depth = -1;  // -1 = unset, default 5 applied in filter
    conf->timeout_seconds = -1;  // -1 = unset: legacy parse path, libgomesi applies its 30s Go-side (macro documents it only)
    conf->max_response_size = -1;  // -1 = unset: legacy parse path, libgomesi leaves 0 = unlimited (pre-#169 behaviour)
    return conf;
}

static void *merge_server_config(apr_pool_t *p, void *basev, void *addv) {
    mesi_config *base = (mesi_config *) basev;
    mesi_config *add = (mesi_config *) addv;
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    conf->enable_mesi = (add->enable_mesi != 0) ? add->enable_mesi : base->enable_mesi;
    conf->allowed_hosts = (add->allowed_hosts->nelts > 0) ? add->allowed_hosts : base->allowed_hosts;
    conf->block_private_ips = (add->block_private_ips != -1) ? add->block_private_ips : base->block_private_ips;
    conf->allow_private_ips_for_allowed = (add->allow_private_ips_for_allowed != -1)
        ? add->allow_private_ips_for_allowed
        : base->allow_private_ips_for_allowed;
    conf->shared_http_client = (add->shared_http_client != -1)
        ? add->shared_http_client
        : base->shared_http_client;
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
    // Memcached: child wins when it parsed any servers (nelts > 0),
    // matching the allowed_hosts "child with entries replaces parent
    // entirely" rule. An empty child list inherits the parent's list.
    conf->cache_memcached_servers = (add->cache_memcached_servers->nelts > 0)
                                    ? add->cache_memcached_servers
                                    : base->cache_memcached_servers;
    conf->cache_key_template = add->cache_key_template ? add->cache_key_template : base->cache_key_template;
    // Max depth: child wins when explicitly set; -1 sentinel inherits.
    // Explicit 0 (passthrough) is a configured value and must win.
    conf->max_depth = (add->max_depth != -1) ? add->max_depth : base->max_depth;
    // Timeout: child wins when explicitly set; -1 sentinel inherits
    // (0 can never be stored — set_timeout rejects it — so the -1
    // sentinel is unambiguous).
    conf->timeout_seconds = (add->timeout_seconds != -1) ? add->timeout_seconds : base->timeout_seconds;
    // Max response size: child wins when explicitly set; -1 sentinel
    // inherits. Unlike timeout, 0 IS storable (explicit "unlimited"),
    // and -1 can never be stored (set_max_response_size rejects it),
    // so the sentinel stays unambiguous and a vhost's explicit 0
    // overrides a global limit.
    conf->max_response_size = (add->max_response_size != -1) ? add->max_response_size : base->max_response_size;
    return conf;
}

static apr_status_t mesi_child_cleanup(void *data) {
    if (EsiFreeHTTPClient) {
        EsiFreeHTTPClient();
    }
    http_client_initialized = 0;
    if (EsiFreeCache) {
        EsiFreeCache();
    }
    if (go_module) {
        dlclose(go_module);
        go_module = NULL;
    }
    EsiParse = NULL;
    EsiParseWithConfig = NULL;
    EsiParseWithConfigEx = NULL;
    EsiParseWithConfigCtx = NULL;
    EsiParseJson = NULL;
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
    // ParseWithConfigEx is optional: it adds the allowPrivateIPsForAllowedHosts
    // parameter. When present, the filter uses it so the
    // MesiAllowPrivateIPsForAllowedHosts directive takes effect. Older
    // libgomesi builds without it fall back to ParseWithConfig (bypass
    // disabled) — the directive is then a no-op with a logged warning.
    EsiParseWithConfigEx = (ParseWithConfigExFunc)dlsym(go_module, "ParseWithConfigEx");
    if (dlerror() != NULL) {
        EsiParseWithConfigEx = NULL;
        (void) dlerror();
    }
    // ParseWithConfigCtx is optional: it adds cache_key_template +
    // requestCtxJSON. When present, the filter uses it so the
    // MesiCacheKeyTemplate directive takes effect. Older libgomesi
    // builds without it fall back to ParseWithConfigEx / ParseWithConfig
    // (templated keys disabled) — template is ignored with no error.
    EsiParseWithConfigCtx = (ParseWithConfigCtxFunc)dlsym(go_module, "ParseWithConfigCtx");
    if (dlerror() != NULL) {
        EsiParseWithConfigCtx = NULL;
        (void) dlerror();
    }
    // ParseJson is optional: JSON config entry point that carries the
    // timeoutSeconds (#167) and maxResponseSize (#169) fields. When
    // present AND MesiTimeout or MesiMaxResponseSize is set, the
    // filter uses it so the directives take effect (the blob also
    // carries cache-key templating when configured, and each of the
    // two keys is only rendered when its own directive is set). Older
    // libgomesi builds without it fall back to the existing
    // ParseWithConfigCtx/Ex/Config chain and the configured
    // directives are ignored with a logged warning — never a crash,
    // never a silently wrong config (same graceful-fallback pattern
    // as ParseWithConfigEx above; Apache and libgomesi normally ship
    // together).
    EsiParseJson = (ParseJsonFunc)dlsym(go_module, "ParseJson");
    if (dlerror() != NULL) {
        EsiParseJson = NULL;
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
    EsiInitHTTPClient = (InitHTTPClientFunc)dlsym(go_module, "InitHTTPClient");
    if (dlerror() != NULL) {
        EsiInitHTTPClient = NULL;
        (void) dlerror();
    }
    EsiFreeHTTPClient = (FreeHTTPClientFunc)dlsym(go_module, "FreeHTTPClient");
    if (dlerror() != NULL) {
        EsiFreeHTTPClient = NULL;
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
        EsiParseWithConfigEx = NULL;
        // Every optional symbol must be cleared too — leaving a pointer
        // into the dlclosed library invites a call into unmapped memory
        // (mesi_init_http_client runs before the filter's NULL-guard).
        EsiParseWithConfigCtx = NULL;
        EsiParseJson = NULL;
        EsiInitHTTPClient = NULL;
        EsiFreeHTTPClient = NULL;
        EsiFreeString = NULL;
        EsiInitCache = NULL;
        EsiInitCacheWithConfig = NULL;
        EsiFreeCache = NULL;
        return;
    }

    apr_pool_cleanup_register(p, NULL, mesi_child_cleanup, apr_pool_cleanup_null);
}

// build_cache_config_json renders the mesi_config cache fields into
// a JSON blob compatible with libgomesi.InitCacheWithConfig. Returns
// NULL when the current backend does not take a config blob (caller
// short-circuits). Returns "{}" or "{}" with rendered keys for the
// matching backend ("redis" / "memcached"). The exact layout mirrors
// libgomesi's memcachedConfig / redisConfig structs; keep in sync if
// those change.
// Memory is allocated from the request pool (short-lived: feeds one cgo
// call). Redis fields: redisAddr/redisPassword/redisDB omitted when at
// defaults. Memcached fields: servers (array of host:port strings) is
// the only key. An empty servers array renders as `"servers":[]` so
// libgomesi rejects the config with a "servers required" error rather
// than silently picking a default server.
static const char *build_cache_config_json(mesi_config *conf, apr_pool_t *pool) {
    if (!conf->cache_backend) {
        return NULL;
    }
    if (strcmp(conf->cache_backend, "redis") == 0) {
        const char *addr = conf->cache_redis_addr
                           ? conf->cache_redis_addr
                           : "localhost:6379";
        // Escape any embedded '"' or '\' so a misconfig password can't
        // inject JSON keys.
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
        // Escape addr the same way (host:port shouldn't contain JSON
        // meta characters, but a hostile config could).
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
    if (strcmp(conf->cache_backend, "memcached") == 0) {
        // Render {"servers":["h:p","h:p",...]} where each host:port is
        // JSON-escaped. The servers array is rendered even when empty
        // so the libgomesi parser produces a deterministic error
        // ("servers required") instead of accepting a bare "{}", which
        // would silently default to localhost:11211.
        apr_array_header_t *arr = conf->cache_memcached_servers;
        const char **items = (arr && arr->nelts > 0)
                             ? (const char **)arr->elts
                             : NULL;
        if (items) {
            // Pre-size: prefix `{"servers":[` (12 bytes) + each item's
            // worst-case `"<escaped>"` (strlen*2 + 2) + (nelts - 1)
            // commas + `]}` (2 bytes) + NUL (1 byte).
            apr_size_t total = 12 + 2 + 1;  // prefix + ]} + NUL
            if (arr->nelts > 1) {
                total += (apr_size_t)(arr->nelts - 1);  // commas
            }
            for (int i = 0; i < arr->nelts; i++) {
                total += strlen(items[i]) * 2 + 2;  // worst-case escaped w/ quotes
            }
            char *buf = apr_palloc(pool, total);
            char *p = buf;
            memcpy(p, "{\"servers\":[", 12); p += 12;
            for (int i = 0; i < arr->nelts; i++) {
                if (i > 0) *p++ = ',';
                *p++ = '"';
                const char *r = items[i];
                while (*r) {
                    if (*r == '"' || *r == '\\') *p++ = '\\';
                    *p++ = *r++;
                }
                *p++ = '"';
            }
            *p++ = ']';
            *p++ = '}';
            *p = '\0';
            return buf;
        }
        // Explicit empty list — passes no servers. Failing fast at this
        // point is intentional: a silent localhost:11211 default would
        // mask operator misconfiguration.
        return apr_pstrdup(pool, "{\"servers\":[]}");
    }
    return NULL;  // "memory" or "" — no config blob needed.
}

// MESI_MAX_CACHE_CONFIG_JSON caps the rendered config blob so an
// operator who pastes a giant password or huge server list can't OOM
// the parser. 4 KB comfortably fits a host:port + a password, or a
// reasonable number of Memcached server entries.
#define MESI_MAX_CACHE_CONFIG_JSON 4096

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
        const char *cfg_json = build_cache_config_json(conf, r->pool);
        // config blobs are required for both redis and memcached;
        // build_cache_config_json now always produces one for those
        // backends (even `"{}"`), but kept the guard so future
        // memory-only paths are obvious to readers.
        if (!cfg_json) {
            ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, r,
                "mesi: cache backend %s lacks configuration; "
                "ESI will run without cache",
                conf->cache_backend);
            cache_initialized = 0;  // Allow next request to retry.
            return -1;
        }
        if (strlen(cfg_json) > MESI_MAX_CACHE_CONFIG_JSON) {
            ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, r,
                "mesi: rendered cache config JSON exceeds %d bytes; "
                "refusing to init cache",
                MESI_MAX_CACHE_CONFIG_JSON);
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

// mesi_init_http_client lazily initializes the shared, SSRF-safe HTTP
// client for this worker process when MesiSharedHTTPClient is On. Called
// once per process from mesi_response_filter on the first request that
// touches a config with the directive set. Returns 0 on success or
// "no shared client configured"; -1 if InitHTTPClient rejected the
// configuration (already logged). The effective MesiBlockPrivateIPs
// setting (default On) is baked into the transport at startup; changing
// it later requires a restart (documented). Guarded by
// http_client_initialized so repeated requests are no-ops.
static int mesi_init_http_client(mesi_config *conf, request_rec *r) {
    if (http_client_initialized) {
        return 0;
    }
    if (conf->shared_http_client != 1) {
        return 0;  // Directive off / unset — nothing to do.
    }
    http_client_initialized = 1;  // Mark before probing so a failing dlsym is not retried.

    if (!EsiInitHTTPClient) {
        if (go_module) {
            (void) dlerror();
            EsiInitHTTPClient = (InitHTTPClientFunc)
                dlsym(go_module, "InitHTTPClient");
            if (dlerror() != NULL) {
                EsiInitHTTPClient = NULL;
            }
        }
    }
    if (!EsiInitHTTPClient) {
        ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, r,
            "mesi: MesiSharedHTTPClient set but libgomesi lacks "
            "InitHTTPClient; shared client disabled. Upgrade libgomesi.so.");
        http_client_initialized = 0;  // Allow next request to retry.
        return -1;
    }

    int bp = (conf->block_private_ips != -1) ? conf->block_private_ips : 1;
    EsiInitHTTPClient(bp);
    ap_log_rerror(APLOG_MARK, APLOG_NOTICE, 0, r,
        "mesi: shared HTTP client initialized (blockPrivateIPs=%d)", bp);
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

static const char *set_allow_private_for_allowed(cmd_parms *cmd, void *cfg, int flag) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    conf->allow_private_ips_for_allowed = flag;
    return NULL;
}

static const char *set_shared_http_client(cmd_parms *cmd, void *cfg, int flag) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    conf->shared_http_client = flag;
    return NULL;
}

// Parse a non-negative decimal integer from arg. Reject empty input,
// non-digit characters (including '-', '+', '.') — fail-fast instead of
// silently coercing via strtol, and values outside [min, max].
// `directive` is the Apache directive name used in every error string
// (e.g. "MesiMaxDepth", "MesiCacheSize") so callers do not remap.
// Returns NULL on success (parsed value stored in *out) or an
// Apache-pool-allocated error string suitable as set_* return value.
static const char *parse_nonneg_int(apr_pool_t *pool, const char *arg,
                                    const char *directive,
                                    int min, int max, int *out) {
    const char *p = arg ? arg : "";
    // Skip leading spaces and tabs only (no newlines per Apache directive).
    while (*p == ' ' || *p == '\t') p++;
    if (*p == '\0') {
        return apr_psprintf(pool,
            "%s requires a non-negative integer argument", directive);
    }
    const char *digits = p;
    while (*p >= '0' && *p <= '9') p++;
    if (*p != '\0') {
        return apr_psprintf(pool,
            "%s must be a non-negative integer (got: %s)", directive, arg);
    }
    if (digits == p) {
        return apr_psprintf(pool,
            "%s must contain at least one digit (got: %s)", directive, arg);
    }
    // Compute length and compare without atoi to catch overflow cheaply.
    size_t n = (size_t)(p - digits);
    if (n > 9) {
        // 9 digits fits in 1_000_000_000; reject anything longer to
        // guarantee we stay inside int32 range (max is 2_147_483_647,
        // which is 10 digits, but we cap at MESI_MAX_* anyway).
        return apr_psprintf(pool,
            "%s value %s exceeds maximum allowed (%d)", directive, arg, max);
    }
    long val = 0;
    for (size_t i = 0; i < n; i++) {
        val = val * 10 + (digits[i] - '0');
    }
    if (val < min || val > max) {
        return apr_psprintf(pool,
            "%s value %s out of range [%d, %d]", directive, arg, min, max);
    }
    *out = (int)val;
    return NULL;
}

// MesiMaxDepth — ESI nesting depth. Uses parse_nonneg_int so "abc",
// "3foo", empty, decimals, and overflow are rejected (atoi would
// silently coerce those). Helper errors already name MesiMaxDepth.
static const char *set_max_depth(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int v = 0;
    const char *err = parse_nonneg_int(cmd->pool, arg, "MesiMaxDepth",
                                       0, MESI_MAX_MAX_DEPTH, &v);
    if (err) {
        return err;
    }
    conf->max_depth = v;
    return NULL;
}

// Parse a non-negative decimal integer into an apr_off_t (int64 on
// every platform Apache 2.4 supports). Same strict contract as
// parse_nonneg_int — empty input, non-digit characters (including
// '-', '+', '.') and values outside [min, max] are rejected with an
// error naming the directive — but that helper's 9-digit guard keeps
// it inside int32 range, which cannot express byte counts, and its
// `int *out` cannot store them. Here overflow is guarded per digit
// against `max` BEFORE the multiply (val > (max - d) / 10 means
// val*10 + d would exceed max), so no intermediate ever wraps, and
// the full 19-digit range below the cap parses. `directive` is the
// Apache directive name used in every error string.
// Returns NULL on success (parsed value stored in *out) or an
// Apache-pool-allocated error string suitable as set_* return value.
static const char *parse_nonneg_off(apr_pool_t *pool, const char *arg,
                                    const char *directive,
                                    apr_off_t min, apr_off_t max,
                                    apr_off_t *out) {
    const char *p = arg ? arg : "";
    // Skip leading spaces and tabs only (no newlines per Apache directive).
    while (*p == ' ' || *p == '\t') p++;
    if (*p == '\0') {
        return apr_psprintf(pool,
            "%s requires a non-negative integer argument", directive);
    }
    const char *digits = p;
    while (*p >= '0' && *p <= '9') p++;
    if (*p != '\0') {
        return apr_psprintf(pool,
            "%s must be a non-negative integer (got: %s)", directive, arg);
    }
    if (digits == p) {
        return apr_psprintf(pool,
            "%s must contain at least one digit (got: %s)", directive, arg);
    }
    apr_off_t val = 0;
    for (const char *q = digits; q < p; q++) {
        apr_off_t d = (apr_off_t)(*q - '0');
        if (val > (max - d) / 10) {
            return apr_psprintf(pool,
                "%s value %s exceeds maximum allowed (%" APR_INT64_T_FMT ")",
                directive, arg, (apr_int64_t)max);
        }
        val = val * 10 + d;
    }
    if (val < min || val > max) {
        return apr_psprintf(pool,
            "%s value %s out of range [%" APR_INT64_T_FMT ", %" APR_INT64_T_FMT "]",
            directive, arg, (apr_int64_t)min, (apr_int64_t)max);
    }
    *out = val;
    return NULL;
}

// MesiTimeout — global per-include fetch budget in seconds (#167).
// Parsed with parse_nonneg_int (NOT atoi — the issue's sketch used
// atoi, but the project forbids silent coercion: atoi would turn
// "abc"/"" into 0 and "2.5" into 2) with range
// [1, MESI_MAX_TIMEOUT_SECONDS]. 0 is deliberately REJECTED: the core
// fails every include immediately when Timeout <= 0 with
// ErrTimeBudgetExceeded (mesi/fetch.go) — it is NOT "no timeout"
// (same rationale as Caddy's "timeout must be positive"). The range
// matches libgomesi's config.ValidateTimeout, so Apache and the
// Go side can never disagree. Helper errors already name MesiTimeout.
static const char *set_timeout(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int v = 0;
    const char *err = parse_nonneg_int(cmd->pool, arg, "MesiTimeout",
                                       1, MESI_MAX_TIMEOUT_SECONDS, &v);
    if (err) {
        return err;
    }
    conf->timeout_seconds = v;
    return NULL;
}

// MesiMaxResponseSize — cap on a single <esi:include> response body
// in bytes (#169). Parsed with parse_nonneg_off (64-bit strict digit
// parser) with range [0, MESI_MAX_MAX_RESPONSE_SIZE]. NOT the issue's
// apr_strtoff sketch: with end=NULL apr_strtoff silently accepts
// trailing garbage ("100abc"), a leading '+' and leading whitespace
// — a malformed explicit value would pass config load — and NOT
// parse_nonneg_int, whose 9-digit int32 guard cannot express byte
// counts. 0 is a LEGITIMATE configured value: the core only limits
// the body when MaxResponseSize > 0 (mesi/fetch.go), so 0 means
// "unlimited" — the documented contract shared with Caddy
// `max_response_size 0`. Negatives are rejected (the core's > 0
// check would silently treat them like 0 = unlimited). Unset (-1
// sentinel) keeps the legacy parse path, where libgomesi leaves the
// field at 0 — byte-identical to pre-#169 Apache behaviour. The
// upper bound exists because the core computes MaxResponseSize+1 for
// its io.LimitReader (mesi/fetch.go) — MaxInt64+1 wraps negative and
// the include would silently render an empty body. Helper errors
// already name MesiMaxResponseSize.
static const char *set_max_response_size(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    apr_off_t v = 0;
    const char *err = parse_nonneg_off(cmd->pool, arg, "MesiMaxResponseSize",
                                       0, MESI_MAX_MAX_RESPONSE_SIZE, &v);
    if (err) {
        return err;
    }
    conf->max_response_size = v;
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
    const char *err = parse_nonneg_int(cmd->pool, arg, "MesiCacheSize",
                                       1, MESI_MAX_CACHE_SIZE, &v);
    if (err) {
        return err;
    }
    conf->cache_size = v;
    return NULL;
}

static const char *set_cache_ttl(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int v = 0;
    const char *err = parse_nonneg_int(cmd->pool, arg, "MesiCacheTTL",
                                       0, MESI_MAX_CACHE_TTL_SECONDS, &v);
    if (err) {
        return err;
    }
    conf->cache_ttl = v;
    return NULL;
}

// MesiCacheRedisAddr — host:port pair. Empty value clears any
// previously-set addr (treated as "use default localhost:6379").
// We use a tiny "loose" check: must contain ':' followed by digits
// (port 1..65535). Reject hostnames containing whitespace, control
// chars, etc. to keep the address safe to embed in JSON.
// Parse a non-negative decimal integer from [arg, end). Bounded,
// so the caller controls where parsing stops (e.g. for parsing a
// port within a "host:port" token whose ':port' is mid-string, or
// for parsing a TAKE1 arg that has no trailing NUL within the
// interesting byte range).
// Reject empty input, non-digit characters (including '-', '+', '.'),
// unsigned overflow, and values outside [min, max]. `directive` is
// the Apache directive name used in every error string.
// Returns NULL on success (parsed value stored in *out) or an
// Apache-pool-allocated error string suitable as set_* return value.
static const char *parse_nonneg_int_bounded(apr_pool_t *pool,
                                            const char *arg, const char *end,
                                            const char *directive,
                                            int min, int max, int *out) {
    if (!arg || !end || arg >= end) {
        return apr_psprintf(pool,
            "%s requires a non-negative integer argument", directive);
    }
    const char *p = arg;
    // Skip leading spaces and tabs only.
    while (p < end && (*p == ' ' || *p == '\t')) p++;
    if (p >= end) {
        return apr_psprintf(pool,
            "%s requires a non-negative integer argument", directive);
    }
    const char *digits = p;
    while (p < end && *p >= '0' && *p <= '9') p++;
    if (p != end) {
        return apr_psprintf(pool,
            "%s must be a non-negative integer (got: %.*s)",
            directive, (int)(end - arg), arg);
    }
    if (digits == p) {
        return apr_psprintf(pool,
            "%s must contain at least one digit", directive);
    }
    // 9 digits fits in 1_000_000_000; reject anything longer to
    // guarantee we stay inside int32 range.
    size_t n = (size_t)(p - digits);
    if (n > 9) {
        return apr_psprintf(pool,
            "%s value exceeds maximum allowed (%d)", directive, max);
    }
    long val = 0;
    for (size_t i = 0; i < n; i++) {
        val = val * 10 + (digits[i] - '0');
    }
    if (val < min || val > max) {
        return apr_psprintf(pool,
            "%s value out of range [%d, %d]", directive, min, max);
    }
    *out = (int)val;
    return NULL;
}

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
    // Validate port is a positive decimal in [1, 65535]. We've already
    // rejected whitespace/JSON-meta chars, so colon+1 is digits-only
    // up to the NUL terminator.
    int port = 0;
    apr_size_t port_len = strlen(colon + 1);
    const char *err = parse_nonneg_int_bounded(cmd->pool,
                                                colon + 1,
                                                colon + 1 + port_len,
                                                "MesiCacheRedisAddr",
                                                1, 65535, &port);
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
    const char *err = parse_nonneg_int(cmd->pool, arg, "MesiCacheRedisDB",
                                       0, MESI_MAX_REDIS_DB, &v);
    if (err) {
        return err;
    }
    conf->cache_redis_db = v;
    return NULL;
}

// set_cache_memcached_servers accepts a space-separated list of
// "host:port" entries used when MesiCacheBackend is memcached (#176).
// Each token must contain a colon followed by a port in [1, 65535];
// hostnames/ports with embedded whitespace, control chars, or JSON
// meta characters are rejected so the rendered JSON config is safe to
// pass to libgomesi. No silent fallback to localhost:11211 — if the
// directive is omitted, the empty server list is logged as a missing-
// config error at runtime and ESI runs without cache. AP_INIT_RAW_ARGS
// gives us the full line, so parsing is line-based (splitting on
// space/tab) just like set_allowed_hosts.
static const char *set_cache_memcached_servers(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    if (!arg) {
        return "MesiCacheMemcachedServers requires space-separated host:port entries";
    }
    // Each child-context config starts from a fresh array (see
    // create_server_config). We append to the array that this server
    // config owns, mirroring set_allowed_hosts behaviour.
    const char *tok;
    int count = 0;
    while (*arg) {
        while (*arg && (*arg == ' ' || *arg == '\t')) arg++;
        tok = arg;
        while (*arg && *arg != ' ' && *arg != '\t') arg++;
        if (tok == arg) {
            continue;
        }
        // Reject embedded whitespace/control chars/JSON-meta chars.
        // (The token was extracted by stopping on space/tab, so
        // whitespace inside the token is impossible; but we recheck
        // for control chars and JSON-meta to be safe.)
        int has_invalid = 0;
        for (const char *p = tok; p < arg; p++) {
            unsigned char c = (unsigned char)*p;
            if (c == '"' || c == '\\' || c < 0x20) {
                has_invalid = 1;
                break;
            }
        }
        if (has_invalid) {
            return apr_psprintf(cmd->pool,
                "MesiCacheMemcachedServers: invalid character in entry %.*s",
                (int)(arg - tok), tok);
        }
        // Find last ':' (matches redis-addr parser). IPv4/IPv6/hostname
        // forms all end with :port.
        const char *colon = NULL;
        for (const char *p = arg - 1; p >= tok; p--) {
            if (*p == ':') { colon = p; break; }
        }
        if (!colon || colon == tok || colon + 1 == arg) {
            return apr_psprintf(cmd->pool,
                "MesiCacheMemcachedServers: entry must be host:port (got: %.*s)",
                (int)(arg - tok), tok);
        }
        // Validate port over [colon+1, arg) so digits inside the host
        // (which can include '1', '0', ...) aren't accidentally
        // consumed. parse_nonneg_int_bounded stops exactly at `arg`.
        int port = 0;
        const char *err = parse_nonneg_int_bounded(cmd->pool, colon + 1, arg,
                                                    "MesiCacheMemcachedServers",
                                                    1, 65535, &port);
        if (err) {
            return apr_psprintf(cmd->pool,
                "MesiCacheMemcachedServers: port invalid in %.*s",
                (int)(arg - tok), tok);
        }
        if (count >= MESI_MAX_MEMCACHED_SERVERS) {
            return apr_psprintf(cmd->pool,
                "MesiCacheMemcachedServers: too many entries (max %d)",
                MESI_MAX_MEMCACHED_SERVERS);
        }
        const char **slot = apr_array_push(conf->cache_memcached_servers);
        // Copy into the server config's pool so it survives past the
        // current request (raw arg pointer is request-scoped).
        *slot = apr_pstrndup(cmd->pool, tok, arg - tok);
        count++;
    }
    if (count == 0) {
        // No tokens: explicit "MesiCacheMemcachedServers " (all whitespace)
        // is treated as a misconfig — we don't silently keep the prior
        // list (which would mask the operator's intent).
        return "MesiCacheMemcachedServers requires at least one host:port entry";
    }
    return NULL;
}

// MesiCacheKeyTemplate — template for cache keys. RSRC_CONF, take1.
// Caches are process-wide/shared, so without a template duplicate
// include URLs share one entry (URL-only DefaultCacheKey). When set,
// the value is passed verbatim to libgomesi ParseWithConfigCtx where
// mesi.BuildCacheKey evaluates ${url}, ${header:Name} (case-insensitive)
// and ${cookie:Name} (case-insensitive); unknown placeholders stay
// literal; NULL/empty falls back to DefaultCacheKey for backward compat.
// Validation: reject NUL bytes and control chars; embedded '"' and
// '\' are permitted (they are JSON-escaped in the request context,
// not in the template — the template itself travels as a plain C string
// to libgomesi). Length capped at MESI_MAX_CACHE_KEY_TEMPLATE to avoid
// unbounded allocations.
static const char *set_cache_key_template(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    if (!arg) {
        return "MesiCacheKeyTemplate requires an argument";
    }
    // Empty string explicitly disables templating (backward compat — same
    // as omitting the directive). Keep NULL sentinel so filter can
    // distinguish "unset" from "set to empty" without extra flags.
    if (arg[0] == '\0') {
        conf->cache_key_template = NULL;
        return NULL;
    }
    size_t len = strlen(arg);
    if (len > MESI_MAX_CACHE_KEY_TEMPLATE) {
        return apr_psprintf(cmd->pool,
            "MesiCacheKeyTemplate exceeds maximum length %d (got %zu)",
            MESI_MAX_CACHE_KEY_TEMPLATE, len);
    }
    // Reject NUL already handled by C string; reject control chars
    // other than spaces that are part of the template syntax? Templates
    // contain ':' and braces; spaces inside a template would be
    // intentional (e.g. "mesi:${url} ${header:X}"), so spaces are
    // allowed. Only reject controls < 0x20 except 	 is also control
    // — but a template may reasonably contain a space for separation,
    // so only reject 0x00-0x1F excluding 0x20. This matches the
    // php-ext validator which calls mesi_is_safe_string (rejects
    // controls and DEL) — but the template itself is not a header
    // name, so we only guard against controls that could corrupt
    // logging or confuse parsers. Allow '"' and '\' — they are
    // harmless in the plain C string passed to CGo.
    for (const char *c = arg; *c; c++) {
        unsigned char uc = (unsigned char)*c;
        if (uc < 0x20) {
            return apr_psprintf(cmd->pool,
                "MesiCacheKeyTemplate contains control character 0x%02x", uc);
        }
        if (uc == 0x7f) {
            return apr_psprintf(cmd->pool,
                "MesiCacheKeyTemplate contains DEL character");
        }
    }
    // Apache's ap_resolve_env (AH00111) replaces undefined ${VAR} with ""
    // before delivery — but it treats "$${url}" as "$" + "${url}", so the
    // second "${url}" is still seen and warned, and the stored value is the
    // literal "$${url}" (verbatim, not collapsed). BuildCacheKey looks for
    // "${url}" which is present at offset 1 with a stray "$" artifact
    // ("mesi:$${url}:..." → keys contain "$<url>"). Normalize "$${" → "${"
    // deterministically at config load so any position/repetition works and
    // keys are clean. After normalization, a mangled single-dollar "${url}"
    // would have become "::" or trailing ":" and is caught below.
    char *norm = NULL;
    if (strstr(arg, "$${") != NULL) {
        // Count occurrences to size correctly (each "$${" -> "${" saves 1 byte)
        size_t cnt = 0;
        for (const char *q = arg; (q = strstr(q, "$${")) != NULL; q += 3) cnt++;
        norm = apr_palloc(cmd->pool, strlen(arg) - cnt + 1);
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
        return apr_psprintf(cmd->pool,
            "MesiCacheKeyTemplate: Apache config interpolation replaced ${url} (AH00111); escape the dollar sign as $${url} in httpd.conf (got: %s)", arg);
    }
    if (arg[len - 1] == ':') {
        return apr_psprintf(cmd->pool,
            "MesiCacheKeyTemplate: Apache config interpolation replaced ${url} (AH00111); escape the dollar sign as $${url} in httpd.conf (got: %s)", arg);
    }
    conf->cache_key_template = apr_pstrdup(cmd->pool, arg);
    return NULL;
}

static void json_grow(char **buf, apr_size_t *cap, apr_size_t need, apr_size_t pos, apr_pool_t *pool) {
    if (pos + need < *cap) return;
    apr_size_t ncap = *cap * 2;
    while (pos + need >= ncap) ncap *= 2;
    char *n = apr_palloc(pool, ncap);
    memcpy(n, *buf, pos);
    *buf = n;
    *cap = ncap;
}

// JSON escaping for request context — mirrors php-ext mesi_dyn_json_append_escape
static void json_escape_append(char **buf, apr_size_t *cap, apr_size_t *pos, const char *src, apr_pool_t *pool) {
    for (const unsigned char *q = (const unsigned char *)src; *q; q++) {
        if (*q == '"') {
            json_grow(buf, cap, 2, *pos, pool);
            (*buf)[(*pos)++] = '\\'; (*buf)[(*pos)++] = '"';
        } else if (*q == '\\') {
            json_grow(buf, cap, 2, *pos, pool);
            (*buf)[(*pos)++] = '\\'; (*buf)[(*pos)++] = '\\';
        } else if (*q < 0x20) {
            static const char hex[] = "0123456789abcdef";
            json_grow(buf, cap, 6, *pos, pool);
            (*buf)[(*pos)++] = '\\'; (*buf)[(*pos)++] = 'u'; (*buf)[(*pos)++] = '0'; (*buf)[(*pos)++] = '0';
            (*buf)[(*pos)++] = hex[(*q >> 4) & 0xf]; (*buf)[(*pos)++] = hex[*q & 0xf];
        } else {
            json_grow(buf, cap, 1, *pos, pool);
            (*buf)[(*pos)++] = (char)*q;
        }
    }
}

static const char *build_request_ctx_json(request_rec *r, mesi_config *conf, apr_pool_t *pool) {
    if (!conf->cache_key_template || conf->cache_key_template[0] == '\0') {
        return "";
    }
    if (!strstr(conf->cache_key_template, "${header:") && !strstr(conf->cache_key_template, "${cookie:")) {
        return "";
    }
    apr_size_t cap = 512;
    char *buf = apr_palloc(pool, cap);
    apr_size_t pos = 0;
    buf[pos++] = '{';
    buf[pos++] = '"'; memcpy(buf+pos, "headers", 7); pos+=7; buf[pos++] = '"'; buf[pos++] = ':'; buf[pos++] = '{';
    int first_hdr = 1;
    if (r->headers_in) {
        const apr_array_header_t *arr = apr_table_elts(r->headers_in);
        const apr_table_entry_t *elts = (const apr_table_entry_t *)arr->elts;
        for (int i = 0; i < arr->nelts; i++) {
            const char *k = elts[i].key;
            const char *v = elts[i].val;
            if (!k || !v) continue;
            if (strcasecmp(k, "Cookie") == 0) continue;
            if (!first_hdr) { json_grow(&buf, &cap, 1, pos, pool); buf[pos++] = ','; }
            first_hdr = 0;
            json_grow(&buf, &cap, 2, pos, pool); buf[pos++] = '"';
            json_escape_append(&buf, &cap, &pos, k, pool);
            json_grow(&buf, &cap, 3, pos, pool); buf[pos++] = '"'; buf[pos++] = ':'; buf[pos++] = '"';
            json_escape_append(&buf, &cap, &pos, v, pool);
            json_grow(&buf, &cap, 1, pos, pool); buf[pos++] = '"';
        }
    }
    json_grow(&buf, &cap, 12, pos, pool); buf[pos++] = '}'; buf[pos++] = ','; buf[pos++] = '"'; memcpy(buf+pos, "cookies", 7); pos+=7; buf[pos++] = '"'; buf[pos++] = ':'; buf[pos++] = '[';
    int first_cookie = 1;
    const char *cookie_hdr = r->headers_in ? apr_table_get(r->headers_in, "Cookie") : NULL;
    if (cookie_hdr) {
        const char *c = cookie_hdr;
        while (*c) {
            while (*c == ' ' || *c == ';' || *c == '\t') c++;
            if (!*c) break;
            const char *name_start = c;
            while (*c && *c != '=' && *c != ';') c++;
            if (!*c || *c != '=') { while (*c && *c != ';') c++; continue; }
            apr_size_t name_len = c - name_start;
            c++;
            const char *val_start = c;
            while (*c && *c != ';') c++;
            apr_size_t val_len = c - val_start;
            while (name_len > 0 && (name_start[name_len-1] == ' ' || name_start[name_len-1] == '\t')) name_len--;
            while (name_len > 0 && (*name_start == ' ' || *name_start == '\t')) { name_start++; name_len--; }
            while (val_len > 0 && (val_start[val_len-1] == ' ' || val_start[val_len-1] == '\t')) val_len--;
            while (val_len > 0 && (*val_start == ' ' || *val_start == '\t')) { val_start++; val_len--; }
            if (name_len == 0) continue;
            char *name = apr_pstrndup(pool, name_start, name_len);
            char *val = apr_pstrndup(pool, val_start, val_len);
            if (!first_cookie) { json_grow(&buf, &cap, 1, pos, pool); buf[pos++] = ','; }
            first_cookie = 0;
            json_grow(&buf, &cap, 20, pos, pool);
            buf[pos++] = '{'; buf[pos++] = '"'; memcpy(buf+pos, "name",4); pos+=4; buf[pos++] = '"'; buf[pos++] = ':'; buf[pos++] = '"';
            json_escape_append(&buf, &cap, &pos, name, pool);
            json_grow(&buf, &cap, 11, pos, pool); buf[pos++] = '"'; buf[pos++] = ','; buf[pos++] = '"'; memcpy(buf+pos, "value",5); pos+=5; buf[pos++] = '"'; buf[pos++] = ':'; buf[pos++] = '"';
            json_escape_append(&buf, &cap, &pos, val, pool);
            json_grow(&buf, &cap, 3, pos, pool); buf[pos++] = '"'; buf[pos++] = '}';
        }
    }
    json_grow(&buf, &cap, 3, pos, pool); buf[pos++] = ']'; buf[pos++] = '}'; buf[pos++] = '\0';
    return buf;
}

// json_string renders s as a quoted, JSON-escaped string for the
// ParseJson config blob: '"' / '\\' escaped, bytes < 0x20 as \u00XX.
// Sized for the worst case (6 output bytes per input byte + quotes +
// NUL) because APR pools have no realloc.
static const char *json_string(apr_pool_t *pool, const char *s) {
    if (!s) {
        s = "";
    }
    apr_size_t len = strlen(s);
    char *buf = apr_palloc(pool, len * 6 + 3);
    char *w = buf;
    *w++ = '"';
    for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
        if (*p == '"') {
            *w++ = '\\'; *w++ = '"';
        } else if (*p == '\\') {
            *w++ = '\\'; *w++ = '\\';
        } else if (*p < 0x20) {
            static const char hex[] = "0123456789abcdef";
            *w++ = '\\'; *w++ = 'u'; *w++ = '0'; *w++ = '0';
            *w++ = hex[(*p >> 4) & 0xf]; *w++ = hex[*p & 0xf];
        } else {
            *w++ = (char)*p;
        }
    }
    *w++ = '"';
    *w = '\0';
    return buf;
}

// build_parse_json_config renders the fully-resolved per-request parse
// configuration into the JSON blob accepted by libgomesi's ParseJson
// entry point (#167). Only called when MesiTimeout or
// MesiMaxResponseSize is set (those are what route a request through
// ParseJson). Every other key mirrors exactly what the legacy
// positional path would pass, so behaviour is identical except for
// the two directives the blob carries. timeoutSeconds travels in
// SECONDS ("timeoutSeconds":N) and maxResponseSize in BYTES
// ("maxResponseSize":N); both keys are rendered ONLY when their
// directive is configured — an absent key resolves to the same
// documented default the positional path uses Go-side (30s /
// 0 = unlimited), so a config that sets only one directive produces
// exactly the legacy behaviour for the other. cacheKeyTemplate/
// requestCtx are included only when a template is configured,
// mirroring ParseWithConfigCtx's contract (absent/empty template →
// URL-only keys; requestCtx is passed through verbatim as
// pre-rendered JSON, omitted when build_request_ctx_json returned "").
static const char *build_parse_json_config(mesi_config *conf,
                                           int depth,
                                           const char *base_url,
                                           const char *allowed_hosts_str,
                                           int block_private,
                                           int allow_private_for_allowed,
                                           const char *ctx_json,
                                           apr_pool_t *pool) {
    const char *timeout_part = "";
    const char *mrs_part = "";
    const char *tmpl_part = "";
    const char *ctx_part = "";
    if (conf->timeout_seconds != -1) {
        timeout_part = apr_psprintf(pool, ",\"timeoutSeconds\":%d",
                                    conf->timeout_seconds);
    }
    if (conf->max_response_size != -1) {
        mrs_part = apr_psprintf(pool, ",\"maxResponseSize\":%" APR_INT64_T_FMT,
                                (apr_int64_t)conf->max_response_size);
    }
    if (conf->cache_key_template && conf->cache_key_template[0] != '\0') {
        tmpl_part = apr_psprintf(pool, ",\"cacheKeyTemplate\":%s",
                                 json_string(pool, conf->cache_key_template));
        if (ctx_json && ctx_json[0] != '\0') {
            ctx_part = apr_psprintf(pool, ",\"requestCtx\":%s", ctx_json);
        }
    }
    return apr_psprintf(pool,
        "{\"maxDepth\":%d,\"defaultUrl\":%s,\"allowedHosts\":%s,"
        "\"blockPrivateIPs\":%s,\"allowPrivateIPsForAllowedHosts\":%s"
        "%s%s%s%s}",
        depth,
        json_string(pool, base_url),
        json_string(pool, allowed_hosts_str),
        block_private ? "true" : "false",
        allow_private_for_allowed ? "true" : "false",
        timeout_part,
        mrs_part,
        tmpl_part,
        ctx_part);
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

    // Initialize the shared HTTP client on first request when
    // MesiSharedHTTPClient is On. The client lives across requests in this
    // worker process; once initialized, repeated calls are no-ops (guarded
    // by http_client_initialized). Errors are logged; on -1 we proceed with
    // per-include clients.
    if (conf->shared_http_client == 1) {
        (void) mesi_init_http_client(conf, f->r);
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
    int allow_private_for_allowed = (conf->allow_private_ips_for_allowed != -1)
        ? conf->allow_private_ips_for_allowed : 0;
    int depth = (conf->max_depth != -1) ? conf->max_depth : MESI_DEFAULT_MAX_DEPTH;

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

    // MesiTimeout (#167) / MesiMaxResponseSize (#169): when either is
    // set AND libgomesi exports ParseJson, the whole parse is routed
    // through the JSON entry point so timeoutSeconds / maxResponseSize
    // reach the core. The blob carries every other resolved setting
    // (depth, base URL, SSRF flags, optional cache key template +
    // request context), so behaviour matches the positional path
    // exactly except for the directives it carries — and each key is
    // only rendered when its directive is configured, so the other
    // directive's absent key keeps its positional-path default
    // (30s / unlimited). When the symbol is missing (older
    // libgomesi.so), fall through to the legacy chain below with a
    // logged warning per configured directive — the directive is
    // ignored (its pre-existing default applies), never a crash and
    // never a silently wrong config.
    // used_parse_json distinguishes "not attempted" from "ParseJson
    // returned NULL" — a NULL must NOT fall back silently; it fails the
    // request closed below (config errors are already logged Go-side).
    int used_parse_json = 0;
    if (conf->timeout_seconds != -1 || conf->max_response_size != -1) {
        if (EsiParseJson) {
            used_parse_json = 1;
            const char *req_ctx_json = build_request_ctx_json(f->r, conf, f->r->pool);
            const char *parse_cfg_json = build_parse_json_config(
                conf, depth, base_url, allowed_hosts_str, block_private,
                allow_private_for_allowed, req_ctx_json, f->r->pool);
            esi = EsiParseJson(html, (char *)parse_cfg_json);
        } else {
            if (conf->timeout_seconds != -1) {
                ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                    "mesi: MesiTimeout set but libgomesi lacks ParseJson; "
                    "MesiTimeout ignored (default 30s timeout applies). Upgrade libgomesi.so.");
            }
            if (conf->max_response_size != -1) {
                ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                    "mesi: MesiMaxResponseSize set but libgomesi lacks ParseJson; "
                    "MesiMaxResponseSize ignored (unlimited response size applies, "
                    "the pre-#169 behaviour). Upgrade libgomesi.so.");
            }
        }
    }

    if (!used_parse_json) {
        if (conf->cache_key_template && conf->cache_key_template[0] != '\0' && !EsiParseWithConfigCtx) {
            ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                "mesi: MesiCacheKeyTemplate set but libgomesi lacks ParseWithConfigCtx; templated keys disabled. Upgrade libgomesi.so.");
        }
        if (conf->cache_key_template && conf->cache_key_template[0] != '\0' && EsiParseWithConfigCtx) {
            const char *ctx_json = build_request_ctx_json(f->r, conf, f->r->pool);
            esi = EsiParseWithConfigCtx(html, depth, base_url, allowed_hosts_str,
                                        block_private, allow_private_for_allowed,
                                        (char *)conf->cache_key_template, (char *)ctx_json);
        } else if (EsiParseWithConfigEx) {
            esi = EsiParseWithConfigEx(html, depth, base_url, allowed_hosts_str,
                                       block_private, allow_private_for_allowed);
        } else if (EsiParseWithConfig) {
            if (allow_private_for_allowed) {
                ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                    "mesi: MesiAllowPrivateIPsForAllowedHosts set but libgomesi lacks ParseWithConfigEx; bypass disabled. Upgrade libgomesi.so.");
            }
            esi = EsiParseWithConfig(html, depth, base_url, allowed_hosts_str, block_private);
        } else {
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
                esi = EsiParse(html, depth, base_url);
            }
        }
    }

    apr_brigade_cleanup(ctx->bb);

    if (!esi) {
        ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, f->r,
            "mesi: libgomesi Parse returned NULL; failing request (fail closed)");
        f->r->status = HTTP_INTERNAL_SERVER_ERROR;
        apr_table_unset(f->r->headers_out, "Content-Length");
        APR_BRIGADE_INSERT_TAIL(ctx->bb, apr_bucket_eos_create(ctx->bb->bucket_alloc));
        return ap_pass_brigade(f->next, ctx->bb);
    }

    char *output = apr_pstrdup(f->r->pool, esi);
    if (EsiFreeString) {
        EsiFreeString(esi);
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
    AP_INIT_TAKE1("MesiMaxDepth", set_max_depth, NULL, RSRC_CONF, "Maximum ESI nesting depth (0..10000). Unset=5. 0=passthrough"),
    AP_INIT_TAKE1("MesiTimeout", set_timeout, NULL, RSRC_CONF, "ESI processing timeout per include in seconds (1..86400). Unset=30"),
    AP_INIT_TAKE1("MesiMaxResponseSize", set_max_response_size, NULL, RSRC_CONF, "Maximum ESI include response body size in bytes (0=unlimited). Unset=unlimited"),
    AP_INIT_RAW_ARGS("MesiAllowedHosts", set_allowed_hosts, NULL, RSRC_CONF, "Space-separated list of allowed hostnames for ESI includes"),
    AP_INIT_FLAG("MesiBlockPrivateIPs", set_block_private_ips, NULL, RSRC_CONF, "Enable or disable private IP blocking (default: On)"),
    AP_INIT_FLAG("MesiAllowPrivateIPsForAllowedHosts", set_allow_private_for_allowed, NULL, RSRC_CONF, "Allow private IP access for hosts in MesiAllowedHosts when MesiBlockPrivateIPs is On (default: Off)"),
    AP_INIT_FLAG("MesiSharedHTTPClient", set_shared_http_client, NULL, RSRC_CONF, "Share HTTP client across ESI includes for connection pooling (default: Off)"),
    AP_INIT_TAKE1("MesiCacheBackend", set_cache_backend, NULL, RSRC_CONF, "Cache backend: \"memory\", \"redis\", \"memcached\" (off when empty). Default: off"),
    AP_INIT_TAKE1("MesiCacheSize", set_cache_size, NULL, RSRC_CONF, "Memory cache max entries (1..1000000). Default: 10000"),
    AP_INIT_TAKE1("MesiCacheTTL", set_cache_ttl, NULL, RSRC_CONF, "Memory cache entry TTL in seconds (0..86400). Default: 0 (no expiry)"),
    AP_INIT_TAKE1("MesiCacheRedisAddr", set_cache_redis_addr, NULL, RSRC_CONF, "Redis server address for ESI caching (default: localhost:6379). Used when MesiCacheBackend is redis"),
    AP_INIT_TAKE1("MesiCacheRedisPassword", set_cache_redis_password, NULL, RSRC_CONF, "Redis AUTH password (default: none). Used when MesiCacheBackend is redis"),
    AP_INIT_TAKE1("MesiCacheRedisDB", set_cache_redis_db, NULL, RSRC_CONF, "Redis database number (0..15). Default: 0. Used when MesiCacheBackend is redis"),
    AP_INIT_RAW_ARGS("MesiCacheMemcachedServers", set_cache_memcached_servers, NULL, RSRC_CONF, "Space-separated list of Memcached servers (host:port). Used when MesiCacheBackend is memcached"),
    AP_INIT_TAKE1("MesiCacheKeyTemplate", set_cache_key_template, NULL, RSRC_CONF, "Cache key template: ${url}, ${header:Name}, ${cookie:Name} (default: mesi:${url}). Unknown placeholders stay literal."),
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
