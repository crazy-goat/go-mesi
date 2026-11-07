#include "httpd.h"
#include "http_config.h"
#include "http_protocol.h"
#include "http_request.h"
#include "http_core.h"
#include "http_log.h"
#include "util_filter.h"
#include "apr_strings.h"

#include "filter_lifecycle.h"
#include "cache_config.h"
#include "parse_json_config.h"
#include "mesi_config.h"
#include "mesi_pure.h"

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
    apr_off_t total; // bytes accumulated so far (for MesiMaxBodySize)
    int passthrough; // 1 = oversize in pass mode: forward everything untouched
} response_filter_ctx;

module AP_MODULE_DECLARE_DATA mesi_module;

static void *create_server_config(apr_pool_t *p, server_rec *s) {
    (void)s;
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    mesi_config_init(conf, p);
    return conf;
}

static void *merge_server_config(apr_pool_t *p, void *basev, void *addv) {
    mesi_config *conf = apr_pcalloc(p, sizeof(*conf));
    mesi_merge_configs((mesi_config *)basev, (mesi_config *)addv, conf);
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
    (void)dlerror();
    EsiParse = (ParseFunc)dlsym(go_module, "Parse");
    if (dlerror() != NULL) {
        EsiParse = NULL;
        (void)dlerror();
    }
    EsiParseWithConfig = (ParseWithConfigFunc)dlsym(go_module, "ParseWithConfig");
    if (dlerror() != NULL) {
        EsiParseWithConfig = NULL;
        (void)dlerror();
    }
    // ParseWithConfigEx is optional: it adds the allowPrivateIPsForAllowedHosts
    // parameter. When present, the filter uses it so the
    // MesiAllowPrivateIPsForAllowedHosts directive takes effect. Older
    // libgomesi builds without it fall back to ParseWithConfig (bypass
    // disabled) — the directive is then a no-op with a logged warning.
    EsiParseWithConfigEx = (ParseWithConfigExFunc)dlsym(go_module, "ParseWithConfigEx");
    if (dlerror() != NULL) {
        EsiParseWithConfigEx = NULL;
        (void)dlerror();
    }
    // ParseWithConfigCtx is optional: it adds cache_key_template +
    // requestCtxJSON. When present, the filter uses it so the
    // MesiCacheKeyTemplate directive takes effect. Older libgomesi
    // builds without it fall back to ParseWithConfigEx / ParseWithConfig
    // (templated keys disabled) — template is ignored with no error.
    EsiParseWithConfigCtx = (ParseWithConfigCtxFunc)dlsym(go_module, "ParseWithConfigCtx");
    if (dlerror() != NULL) {
        EsiParseWithConfigCtx = NULL;
        (void)dlerror();
    }
    // ParseJson is optional: JSON config entry point that carries the
    // timeoutSeconds (#167), maxResponseSize (#169),
    // maxConcurrentRequests (#170), maxWorkers (#171), debug (#173) and
    // parseOnHeader (#172) fields. When present AND MesiTimeout,
    // MesiMaxResponseSize, MesiMaxConcurrentRequests, MesiMaxWorkers,
    // MesiDebug or MesiParseOnHeader is set, the filter uses it so the
    // directives take effect (the blob also carries cache-key templating
    // when configured, and each of the six keys is only rendered when its
    // own directive is set).
    // Older libgomesi builds without it fall back to the existing
    // ParseWithConfigCtx/Ex/Config chain and the configured
    // directives are ignored with a logged warning — never a crash,
    // never a silently wrong config (same graceful-fallback pattern
    // as ParseWithConfigEx above; Apache and libgomesi normally ship
    // together).
    EsiParseJson = (ParseJsonFunc)dlsym(go_module, "ParseJson");
    if (dlerror() != NULL) {
        EsiParseJson = NULL;
        (void)dlerror();
    }
    EsiFreeString = (FreeFunc)dlsym(go_module, "FreeString");
    if (dlerror() != NULL) {
        EsiFreeString = NULL;
        (void)dlerror();
    }
    EsiInitCache = (InitCacheFunc)dlsym(go_module, "InitCache");
    if (dlerror() != NULL) {
        EsiInitCache = NULL;
        (void)dlerror();
    }
    EsiInitCacheWithConfig = (InitCacheWithConfigFunc)dlsym(go_module, "InitCacheWithConfig");
    if (dlerror() != NULL) {
        EsiInitCacheWithConfig = NULL;
        (void)dlerror();
    }
    EsiFreeCache = (FreeCacheFunc)dlsym(go_module, "FreeCache");
    if (dlerror() != NULL) {
        EsiFreeCache = NULL;
        (void)dlerror();
    }
    EsiInitHTTPClient = (InitHTTPClientFunc)dlsym(go_module, "InitHTTPClient");
    if (dlerror() != NULL) {
        EsiInitHTTPClient = NULL;
        (void)dlerror();
    }
    EsiFreeHTTPClient = (FreeHTTPClientFunc)dlsym(go_module, "FreeHTTPClient");
    if (dlerror() != NULL) {
        EsiFreeHTTPClient = NULL;
        (void)dlerror();
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
        const char *addr = (conf->cache_redis_addr && conf->cache_redis_addr[0] != '\0')
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
            apr_size_t esc_cap = pwd_len * 2 + 3; // + '"' + '"' + NUL
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
            apr_size_t total = 12 + 2 + 1; // prefix + ]} + NUL
            if (arr->nelts > 1) {
                total += (apr_size_t)(arr->nelts - 1); // commas
            }
            for (int i = 0; i < arr->nelts; i++) {
                total += strlen(items[i]) * 2 + 2; // worst-case escaped w/ quotes
            }
            char *buf = apr_palloc(pool, total);
            char *p = buf;
            memcpy(p, "{\"servers\":[", 12);
            p += 12;
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
    return NULL; // "memory" or "" — no config blob needed.
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
        return 0; // Cache disabled — nothing to do.
    }
    cache_initialized = 1; // Mark before probing so a failing dlsym is not retried.

    int size = conf->cache_size > 0 ? conf->cache_size : MESI_DEFAULT_CACHE_SIZE;
    int ttl = conf->cache_ttl >= 0 ? conf->cache_ttl : 0;

    int needs_config = (strcmp(conf->cache_backend, "redis") == 0) || (strcmp(conf->cache_backend, "memcached") == 0);
    if (needs_config) {
        // Resolve InitCacheWithConfig lazily — mirrors the InitCache
        // fallback below in case the first request arrives before
        // child_init finished probing all symbols.
        if (!EsiInitCacheWithConfig) {
            if (go_module) {
                (void)dlerror();
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
            (void)dlerror();
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
            cache_initialized = 0; // Allow next request to retry.
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
        cache_initialized = 0; // Allow next request to retry.
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
        return 0; // Directive off / unset — nothing to do.
    }
    http_client_initialized = 1; // Mark before probing so a failing dlsym is not retried.

    if (!EsiInitHTTPClient) {
        if (go_module) {
            (void)dlerror();
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
        http_client_initialized = 0; // Allow next request to retry.
        return -1;
    }

    int bp = (conf->block_private_ips != -1) ? conf->block_private_ips : 1;
    EsiInitHTTPClient(bp);
    ap_log_rerror(APLOG_MARK, APLOG_NOTICE, 0, r,
                  "mesi: shared HTTP client initialized (blockPrivateIPs=%d)", bp);
    return 0;
}

// Thin Apache glue: resolve the per-server config and forward to the shared
// pure validator in mesi_config.h (#558). Keep these wrappers trivial so the
// unit tests exercise the same code the module runs.
#define MESI_CONF_FROM_CMD(cmd) \
    ((mesi_config *)ap_get_module_config((cmd)->server->module_config, &mesi_module))

// Adapter for the shared RAW_ARGS single-token parsers: the tokenizer is the
// only Apache-specific part of those directives.
static const char *mesi_ap_getword_conf(apr_pool_t *pool, const char **line) {
    return ap_getword_conf(pool, line);
}

static const char *set_enable_mesi(cmd_parms *cmd, void *cfg, int flag) {
    (void)cfg;
    return mesi_set_enable_mesi(MESI_CONF_FROM_CMD(cmd), flag);
}

static const char *set_allowed_hosts(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_allowed_hosts(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_block_private_ips(cmd_parms *cmd, void *cfg, int flag) {
    (void)cfg;
    return mesi_set_block_private_ips(MESI_CONF_FROM_CMD(cmd), flag);
}

static const char *set_allow_private_for_allowed(cmd_parms *cmd, void *cfg, int flag) {
    (void)cfg;
    return mesi_set_allow_private_for_allowed(MESI_CONF_FROM_CMD(cmd), flag);
}

static const char *set_shared_http_client(cmd_parms *cmd, void *cfg, int flag) {
    (void)cfg;
    return mesi_set_shared_http_client(MESI_CONF_FROM_CMD(cmd), flag);
}

static const char *set_debug(cmd_parms *cmd, void *cfg, int flag) {
    (void)cfg;
    return mesi_set_debug(MESI_CONF_FROM_CMD(cmd), flag);
}

static const char *set_parse_on_header(cmd_parms *cmd, void *cfg, int flag) {
    (void)cfg;
    return mesi_set_parse_on_header(MESI_CONF_FROM_CMD(cmd), flag);
}

static const char *set_max_depth(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_max_depth(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_include_error_marker(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_include_error_marker(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg,
                                         mesi_ap_getword_conf);
}

static const char *set_timeout(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_timeout(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_max_response_size(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_max_response_size(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_max_body_size(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_max_body_size(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_on_oversize(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_on_oversize(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_max_concurrent_requests(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_max_concurrent_requests(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_max_workers(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_max_workers(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_cache_backend(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_cache_backend(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg,
                                  mesi_ap_getword_conf);
}

static const char *set_cache_size(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_cache_size(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_cache_ttl(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_cache_ttl(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_cache_redis_addr(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_cache_redis_addr(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg,
                                     mesi_ap_getword_conf);
}

static const char *set_cache_redis_password(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_cache_redis_password(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg,
                                         mesi_ap_getword_conf);
}

static const char *set_cache_redis_db(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_cache_redis_db(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_cache_memcached_servers(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_cache_memcached_servers(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
}

static const char *set_cache_key_template(cmd_parms *cmd, void *cfg, const char *arg) {
    (void)cfg;
    return mesi_set_cache_key_template(MESI_CONF_FROM_CMD(cmd), cmd->pool, arg);
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
            (*buf)[(*pos)++] = '\\';
            (*buf)[(*pos)++] = '"';
        } else if (*q == '\\') {
            json_grow(buf, cap, 2, *pos, pool);
            (*buf)[(*pos)++] = '\\';
            (*buf)[(*pos)++] = '\\';
        } else if (*q < 0x20) {
            static const char hex[] = "0123456789abcdef";
            json_grow(buf, cap, 6, *pos, pool);
            (*buf)[(*pos)++] = '\\';
            (*buf)[(*pos)++] = 'u';
            (*buf)[(*pos)++] = '0';
            (*buf)[(*pos)++] = '0';
            (*buf)[(*pos)++] = hex[(*q >> 4) & 0xf];
            (*buf)[(*pos)++] = hex[*q & 0xf];
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
    buf[pos++] = '"';
    memcpy(buf + pos, "headers", 7);
    pos += 7;
    buf[pos++] = '"';
    buf[pos++] = ':';
    buf[pos++] = '{';
    int first_hdr = 1;
    if (r->headers_in) {
        const apr_array_header_t *arr = apr_table_elts(r->headers_in);
        const apr_table_entry_t *elts = (const apr_table_entry_t *)arr->elts;
        for (int i = 0; i < arr->nelts; i++) {
            const char *k = elts[i].key;
            const char *v = elts[i].val;
            if (!k || !v) continue;
            if (strcasecmp(k, "Cookie") == 0) continue;
            if (!first_hdr) {
                json_grow(&buf, &cap, 1, pos, pool);
                buf[pos++] = ',';
            }
            first_hdr = 0;
            json_grow(&buf, &cap, 2, pos, pool);
            buf[pos++] = '"';
            json_escape_append(&buf, &cap, &pos, k, pool);
            json_grow(&buf, &cap, 3, pos, pool);
            buf[pos++] = '"';
            buf[pos++] = ':';
            buf[pos++] = '"';
            json_escape_append(&buf, &cap, &pos, v, pool);
            json_grow(&buf, &cap, 1, pos, pool);
            buf[pos++] = '"';
        }
    }
    json_grow(&buf, &cap, 12, pos, pool);
    buf[pos++] = '}';
    buf[pos++] = ',';
    buf[pos++] = '"';
    memcpy(buf + pos, "cookies", 7);
    pos += 7;
    buf[pos++] = '"';
    buf[pos++] = ':';
    buf[pos++] = '[';
    int first_cookie = 1;
    const char *cookie_hdr = r->headers_in ? apr_table_get(r->headers_in, "Cookie") : NULL;
    if (cookie_hdr) {
        const char *c = cookie_hdr;
        while (*c) {
            while (*c == ' ' || *c == ';' || *c == '\t') c++;
            if (!*c) break;
            const char *name_start = c;
            while (*c && *c != '=' && *c != ';') c++;
            if (!*c || *c != '=') {
                while (*c && *c != ';') c++;
                continue;
            }
            apr_size_t name_len = c - name_start;
            c++;
            const char *val_start = c;
            while (*c && *c != ';') c++;
            apr_size_t val_len = c - val_start;
            while (name_len > 0 && (name_start[name_len - 1] == ' ' || name_start[name_len - 1] == '\t')) name_len--;
            while (name_len > 0 && (*name_start == ' ' || *name_start == '\t')) {
                name_start++;
                name_len--;
            }
            while (val_len > 0 && (val_start[val_len - 1] == ' ' || val_start[val_len - 1] == '\t')) val_len--;
            while (val_len > 0 && (*val_start == ' ' || *val_start == '\t')) {
                val_start++;
                val_len--;
            }
            if (name_len == 0) continue;
            char *name = apr_pstrndup(pool, name_start, name_len);
            char *val = apr_pstrndup(pool, val_start, val_len);
            if (!first_cookie) {
                json_grow(&buf, &cap, 1, pos, pool);
                buf[pos++] = ',';
            }
            first_cookie = 0;
            json_grow(&buf, &cap, 20, pos, pool);
            buf[pos++] = '{';
            buf[pos++] = '"';
            memcpy(buf + pos, "name", 4);
            pos += 4;
            buf[pos++] = '"';
            buf[pos++] = ':';
            buf[pos++] = '"';
            json_escape_append(&buf, &cap, &pos, name, pool);
            json_grow(&buf, &cap, 11, pos, pool);
            buf[pos++] = '"';
            buf[pos++] = ',';
            buf[pos++] = '"';
            memcpy(buf + pos, "value", 5);
            pos += 5;
            buf[pos++] = '"';
            buf[pos++] = ':';
            buf[pos++] = '"';
            json_escape_append(&buf, &cap, &pos, val, pool);
            json_grow(&buf, &cap, 3, pos, pool);
            buf[pos++] = '"';
            buf[pos++] = '}';
        }
    }
    json_grow(&buf, &cap, 3, pos, pool);
    buf[pos++] = ']';
    buf[pos++] = '}';
    buf[pos++] = '\0';
    return buf;
}

// build_parse_json_config renders the fully-resolved per-request parse
// configuration into the JSON blob accepted by libgomesi's ParseJson
// entry point (#167). Only called when MesiTimeout,
// MesiMaxResponseSize, MesiMaxConcurrentRequests, MesiMaxWorkers,
// MesiDebug or a non-empty MesiIncludeErrorMarker is set (those are what
// route a request through ParseJson).
// Every other key mirrors exactly what the legacy positional path would
// pass, so behaviour is identical except for the directives the blob
// carries.
// timeoutSeconds travels in SECONDS ("timeoutSeconds":N),
// maxResponseSize in BYTES ("maxResponseSize":N),
// maxConcurrentRequests as a plain count ("maxConcurrentRequests":N)
// and maxWorkers as a plain count ("maxWorkers":N); all four keys are
// rendered ONLY when their directive is configured — an absent key
// resolves to the same documented default the positional path uses
// Go-side (30s / 0 = unlimited / 0 = unlimited / 0 = library default
// NumCPU*4), so a config that sets only one directive produces exactly
// the legacy behaviour for the others.
// debug is rendered only when MesiDebug is configured ("debug":true or
// false), so an unset directive omits the key and keeps the silent
// positional-path default (#173). parseOnHeader is rendered only when
// MesiParseOnHeader is configured ("parseOnHeader":true or false), so an
// unset directive omits the key and keeps the always-parse positional
// default (#172).
// cacheKeyTemplate/requestCtx are included only when a template is
// configured, mirroring ParseWithConfigCtx's contract (absent/empty
// template → URL-only keys; requestCtx is passed through verbatim as
// pre-rendered JSON, omitted when build_request_ctx_json returned "").
// includeErrorMarker is included only when the directive is set to a
// non-empty value (#165) — absent/empty means silent, so the key stays
// out of the blob and the core leaves its zero value untouched.
// The blob itself is rendered by mesi_build_parse_json_config()
// (parse_json_config.h) so the C unit tests exercise the real assembly
// instead of a copy; see that header for the placeholder/argument-safety
// rationale.
static const char *build_parse_json_config(mesi_config *conf,
                                           int depth,
                                           const char *base_url,
                                           const char *allowed_hosts_str,
                                           int block_private,
                                           int allow_private_for_allowed,
                                           const char *ctx_json,
                                           apr_pool_t *pool) {
    mesi_parse_json_config cfg = {
        .depth = depth,
        .base_url = base_url,
        .allowed_hosts = allowed_hosts_str,
        .block_private_ips = block_private,
        .allow_private_for_allowed = allow_private_for_allowed,
        .request_ctx_json = ctx_json,
        .timeout_seconds = conf->timeout_seconds,
        .max_response_size = conf->max_response_size,
        .max_concurrent_requests = conf->max_concurrent_requests,
        .max_workers = conf->max_workers,
        .debug = conf->debug,
        .parse_on_header = conf->parse_on_header,
        .include_error_marker = conf->include_error_marker,
        .cache_key_template = conf->cache_key_template,
    };
    return mesi_build_parse_json_config(pool, &cfg);
}

// mesi_fixups advertises this surrogate's ESI capability to the origin as a
// request header. It runs after authentication and authorization (a denied
// request never advertises anything) and before the content handler (so
// mod_proxy, CGI and friends forward it). The Edge Architecture
// Specification defines Surrogate-Capability as a request header and the
// response-side header is Surrogate-Control, so this deliberately does not
// touch headers_out: the module never adds the header to the client
// response. Existing client capability sets are preserved (#97).
static int mesi_fixups(request_rec *r) {
    mesi_config *conf = (mesi_config *)ap_get_module_config(r->server->module_config, &mesi_module);
    if (conf->enable_mesi && r->headers_in) {
        apr_table_set(r->headers_in, "Surrogate-Capability",
                      mesi_surrogate_capability(
                          r->pool,
                          apr_table_get(r->headers_in, "Surrogate-Capability")));
    }
    return DECLINED;
}

// mesi_is_dynamic_request reports whether the final Content-Type is decided
// by a handler rather than by the static-file type map. mod_proxy is the
// important case: mod_mime may have guessed a type from the proxy filename
// ("proxy:http://host/path.txt") before insert_filter runs, and the origin
// can still return text/html. An explicitly configured (non-default) handler
// is treated the same way: its output type is not known yet either.
static int mesi_is_dynamic_request(request_rec *r) {
    if (r->proxyreq != PROXYREQ_NONE) {
        return 1;
    }
    return r->handler != NULL && r->handler[0] != '\0' &&
           strcmp(r->handler, "default-handler") != 0;
}

// mesi_insert_filter runs from ap_invoke_handler, after the request passed
// authentication, type_checker and fixups, but before the content handler.
// It replaces the old post_read_request registration (#97): requests that
// are never handled (auth-denied, translate-phase redirects) no longer get
// the filter, and static non-HTML responses skip it entirely. Subrequests
// keep the pre-#97 behaviour (no filter).
//
// The Content-Type is not trusted here for a proxy or dynamic request: it
// may be only an extension-derived guess. Those responses get the filter
// and mesi_response_filter applies the final content-type/status gate on the
// first brigade, together with 3xx/4xx/5xx/204/304 handling.
static void mesi_insert_filter(request_rec *r) {
    mesi_config *conf = (mesi_config *)ap_get_module_config(r->server->module_config, &mesi_module);
    if (!mesi_insert_filter_wanted(conf->enable_mesi, r->status,
                                   r->content_type, r->header_only,
                                   r->main != NULL,
                                   mesi_is_dynamic_request(r))) {
        return;
    }
    ap_add_output_filter("MESI_RESPONSE", NULL, r, r->connection);
}

static char *build_base_url(request_rec *r, apr_pool_t *pool) {
    const char *scheme = ap_http_scheme(r);
    // Prefer the configured canonical name over the client-supplied Host
    // header; mesi_base_url_host also maps an empty result to "localhost".
    const char *host =
        mesi_base_url_host(r->server->server_hostname, ap_get_server_name(r));
    // Use canonical port from server config, not client-supplied
    apr_port_t port = r->server->port ? r->server->port : ap_get_server_port(r);

    return mesi_build_base_url(scheme, host, port, pool);
}

// A response body with a Content-Encoding other than "identity" is
// compressed or otherwise transformed. Parsing those bytes as HTML would
// corrupt the body (a gzip stream is not NUL-terminated text), so the
// filter must pass the response through untouched. nginx has the
// equivalent check in its head filter (ngx_test_content_compression).
static int mesi_is_encoded_response(request_rec *r) {
    const char *ce = apr_table_get(r->headers_out, "Content-Encoding");
    if (!ce) return 0;
    while (*ce == ' ' || *ce == '\t') ce++;
    if (strncasecmp(ce, "identity", sizeof("identity") - 1) != 0) return 1;
    ce += sizeof("identity") - 1;
    while (*ce == ' ' || *ce == '\t') ce++;
    // "identity" alone means no transformation; anything else ("identity,
    // gzip", a parameter, ...) means the body may be encoded.
    return *ce != '\0';
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
    mesi_config *conf = (mesi_config *)ap_get_module_config(f->r->server->module_config, &mesi_module);
    if (!conf->enable_mesi) {
        return ap_pass_brigade(f->next, bb);
    }

    // 204/304 cannot carry a body. Apache hands the handler body to
    // output filters even for HEAD, so parsing it would fetch includes
    // that are never sent. An encoded body cannot be parsed as HTML.
    // All of these pass through untouched. mesi_response_is_eligible
    // already covers non-HTML and non-2xx/204 statuses (#97); the explicit
    // checks below keep the #98 HEAD/304/encoding contract.
    int bodiless = f->r->header_only || f->r->status == HTTP_NO_CONTENT ||
                   f->r->status == HTTP_NOT_MODIFIED;
    if (!mesi_response_is_eligible(f->r->content_type, f->r->status) ||
        bodiless || mesi_is_encoded_response(f->r)) {
        ap_remove_output_filter(f);
        return ap_pass_brigade(f->next, bb);
    }

    // MesiParseOnHeader (#172): when On, only an HTML response whose
    // upstream headers carry `Edge-control: dca=esi` reaches the buffering
    // and parsing path. The check runs before the filter context is
    // created and before any bucket is accumulated, so an unsignalled page
    // is streamed through unchanged — not buffered, not limited by
    // MesiMaxBodySize, not parsed — with no ESI capability header.
    int parse_on_header = (conf->parse_on_header != -1) ? conf->parse_on_header : 0;
    if (parse_on_header &&
        !mesi_header_signals_esi(apr_table_get(f->r->headers_out, "Edge-control"))) {
        ap_remove_output_filter(f);
        return ap_pass_brigade(f->next, bb);
    }

    response_filter_ctx *ctx = f->ctx;
    if (!ctx) {
        ctx = apr_pcalloc(f->r->pool, sizeof(*ctx));
        ctx->bb = apr_brigade_create(f->r->pool, f->c->bucket_alloc);
        f->ctx = ctx;
    }

    if (ctx->passthrough) {
        return ap_pass_brigade(f->next, bb);
    }

    apr_off_t limit = (conf->max_body_size > 0) ? conf->max_body_size : 0;
    int oversize = 0;
    // Declared Content-Length over the limit: decide before buffering.
    // Bodiless responses (HEAD, 204, 304) were passed through above and
    // never reach this point.
    if (limit > 0) {
        apr_off_t declared = f->r->clength;
        const char *cl = apr_table_get(f->r->headers_out, "Content-Length");
        apr_off_t hdr = 0;
        if (cl && apr_strtoff(&hdr, cl, NULL, 10) == APR_SUCCESS && hdr > declared) {
            declared = hdr;
        }
        if (declared > limit) {
            oversize = 1;
        }
    }

    // Move all buckets from the incoming brigade to our accumulation brigade.
    // Track whether we've seen the end-of-stream (EOS) marker.
    int seen_eos = 0;
    apr_bucket *b;
    while (!oversize && (b = APR_BRIGADE_FIRST(bb)) != APR_BRIGADE_SENTINEL(bb)) {
        if (APR_BUCKET_IS_EOS(b)) {
            seen_eos = 1;
            apr_bucket_delete(b);
            continue;
        }
        if (limit > 0 && !APR_BUCKET_IS_METADATA(b)) {
            apr_size_t blen = b->length;
            if (b->length == (apr_size_t)-1) {
                // Unknown length (pipe, socket): reading splits the bucket
                // and gives the real size.
                const char *data;
                if (apr_bucket_read(b, &data, &blen, APR_BLOCK_READ) != APR_SUCCESS) {
                    blen = 0;
                }
            }
            ctx->total += (apr_off_t)blen;
            if (ctx->total > limit) {
                oversize = 1;
                break;
            }
        }
        // The data is held across filter calls, so transient buckets (the
        // proxy reuses their memory after passing the brigade) must be
        // copied to the request pool first.
        if (!APR_BUCKET_IS_METADATA(b)) {
            apr_bucket_setaside(b, f->r->pool);
        }
        APR_BUCKET_REMOVE(b);
        APR_BRIGADE_INSERT_TAIL(ctx->bb, b);
    }

    if (oversize) {
        if (conf->on_oversize == MESI_ON_OVERSIZE_ERROR) {
            ap_log_rerror(APLOG_MARK, APLOG_ERR, 0, f->r,
                          "mesi: response body exceeds MesiMaxBodySize (%" APR_OFF_T_FMT " bytes), returning 502",
                          limit);
            apr_brigade_cleanup(ctx->bb);
            apr_brigade_cleanup(bb);
            // Nothing has been sent yet (everything is buffered here), so
            // an error bucket turns the response into a 502.
            apr_bucket_brigade *eb = apr_brigade_create(f->r->pool, f->c->bucket_alloc);
            APR_BRIGADE_INSERT_TAIL(eb, ap_bucket_error_create(HTTP_BAD_GATEWAY, NULL, f->r->pool, f->c->bucket_alloc));
            APR_BRIGADE_INSERT_TAIL(eb, apr_bucket_eos_create(f->c->bucket_alloc));
            ap_remove_output_filter(f);
            return ap_pass_brigade(f->next, eb);
        }
        ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                      "mesi: response body exceeds MesiMaxBodySize (%" APR_OFF_T_FMT " bytes), skipping ESI processing",
                      limit);
        // Send what we buffered, then the rest, untouched.
        ctx->passthrough = 1;
        APR_BRIGADE_CONCAT(ctx->bb, bb);
        return ap_pass_brigade(f->next, ctx->bb);
    }

    if (!seen_eos) {
        return APR_SUCCESS; // Not the last brigade — wait for more data
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
        (void)mesi_init_cache(conf, f->r);
    }

    // Initialize the shared HTTP client on first request when
    // MesiSharedHTTPClient is On. The client lives across requests in this
    // worker process; once initialized, repeated calls are no-ops (guarded
    // by http_client_initialized). Errors are logged; on -1 we proceed with
    // per-include clients.
    if (conf->shared_http_client == 1) {
        (void)mesi_init_http_client(conf, f->r);
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
                                        ? conf->allow_private_ips_for_allowed
                                        : 0;
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

    // MesiTimeout (#167) / MesiMaxResponseSize (#169) /
    // MesiMaxConcurrentRequests (#170) / MesiMaxWorkers (#171) /
    // MesiDebug (#173) / MesiParseOnHeader (#172) /
    // MesiIncludeErrorMarker (#165): when any is set AND libgomesi
    // exports ParseJson, the whole parse is routed through the JSON entry
    // point so timeoutSeconds / maxResponseSize / maxConcurrentRequests /
    // maxWorkers / debug / parseOnHeader / includeErrorMarker reach the
    // core. The blob carries every other resolved setting (depth, base
    // URL, SSRF flags, optional cache key template + request context), so
    // behaviour matches the positional path exactly except for the
    // directives it carries — and each key is only rendered when its
    // directive is configured, so the other
    // directives' absent keys keep their positional-path defaults
    // (30s / unlimited / unlimited / library default / debug off /
    // silent). When the symbol is missing (older libgomesi.so), fall
    // through to the legacy chain below with a logged warning per
    // configured directive — the directive is ignored (its pre-existing default
    // applies), never a crash and never a silently wrong config.
    // used_parse_json distinguishes "not attempted" from "ParseJson
    // returned NULL" — a NULL must NOT fall back silently; it fails the
    // request closed below (config errors are already logged Go-side).
    int used_parse_json = 0;
    if (conf->timeout_seconds != -1 || conf->max_response_size != -1 || conf->max_concurrent_requests != -1 || conf->max_workers != -1 || conf->debug != -1 || conf->parse_on_header != -1 || (conf->include_error_marker && conf->include_error_marker[0] != '\0')) {
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
            if (conf->max_concurrent_requests != -1) {
                ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                              "mesi: MesiMaxConcurrentRequests set but libgomesi lacks ParseJson; "
                              "MesiMaxConcurrentRequests ignored (unlimited concurrent requests "
                              "apply, the pre-#170 behaviour). Upgrade libgomesi.so.");
            }
            if (conf->max_workers != -1) {
                ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                              "mesi: MesiMaxWorkers set but libgomesi lacks ParseJson; "
                              "MesiMaxWorkers ignored (library default NumCPU*4 worker pool "
                              "applies, the pre-#171 behaviour). Upgrade libgomesi.so.");
            }
            if (conf->debug != -1) {
                ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                              "mesi: MesiDebug set but libgomesi lacks ParseJson; "
                              "MesiDebug ignored (parser stays silent, the pre-#173 behaviour). "
                              "Upgrade libgomesi.so.");
            }
            if (conf->parse_on_header != -1) {
                ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                              "mesi: MesiParseOnHeader set but libgomesi lacks ParseJson; "
                              "MesiParseOnHeader ignored for fetched fragments (the top-level "
                              "response filter still honours it; nested includes always parse, "
                              "the pre-#172 behaviour). Upgrade libgomesi.so.");
            }
            if (conf->include_error_marker && conf->include_error_marker[0] != '\0') {
                ap_log_rerror(APLOG_MARK, APLOG_WARNING, 0, f->r,
                              "mesi: MesiIncludeErrorMarker set but libgomesi lacks ParseJson; "
                              "MesiIncludeErrorMarker ignored (failed includes stay silent, "
                              "the pre-#165 behaviour). Upgrade libgomesi.so.");
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
            int has_security_config = (conf->allowed_hosts && conf->allowed_hosts->nelts > 0) || (conf->block_private_ips != -1 && conf->block_private_ips == 1);
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
    ap_hook_fixups(mesi_fixups, NULL, NULL, APR_HOOK_MIDDLE);
    ap_hook_insert_filter(mesi_insert_filter, NULL, NULL, APR_HOOK_MIDDLE);
    ap_register_output_filter("MESI_RESPONSE", mesi_response_filter, NULL, AP_FTYPE_CONTENT_SET);
}

static const command_rec mesi_directives[] = {
    AP_INIT_FLAG("EnableMesi", set_enable_mesi, NULL, RSRC_CONF, "Enable or disable the Mesi module"),
    AP_INIT_TAKE1("MesiMaxDepth", set_max_depth, NULL, RSRC_CONF, "Maximum ESI nesting depth (0..10000). Unset=5. 0=passthrough"),
    AP_INIT_RAW_ARGS("MesiIncludeErrorMarker", set_include_error_marker, NULL, RSRC_CONF, "Raw HTML rendered for a failed ESI include with no onerror/fallback (quote values with spaces; \"\" = explicitly silent; unset = inherit)"),
    AP_INIT_TAKE1("MesiTimeout", set_timeout, NULL, RSRC_CONF, "ESI processing timeout per include in seconds (1..86400). Unset=30"),
    AP_INIT_TAKE1("MesiMaxResponseSize", set_max_response_size, NULL, RSRC_CONF, "Maximum ESI include response body size in bytes (0=unlimited). Unset=unlimited"),
    AP_INIT_TAKE1("MesiMaxBodySize", set_max_body_size, NULL, RSRC_CONF, "Maximum parent response body size in bytes buffered for ESI processing (0=unlimited). Unset=unlimited"),
    AP_INIT_TAKE1("MesiOnOversize", set_on_oversize, NULL, RSRC_CONF, "Action when the body exceeds MesiMaxBodySize: pass (send unprocessed, default) or error (502)"),
    AP_INIT_TAKE1("MesiMaxConcurrentRequests", set_max_concurrent_requests, NULL, RSRC_CONF, "Maximum concurrent ESI include fetches per page render (0=unlimited). Unset=0 (unlimited)"),
    AP_INIT_TAKE1("MesiMaxWorkers", set_max_workers, NULL, RSRC_CONF, "Maximum token-processing goroutines per parse level (0=NumCPU*4 library default). Unset=0 (library default)"),
    AP_INIT_RAW_ARGS("MesiAllowedHosts", set_allowed_hosts, NULL, RSRC_CONF, "Space-separated list of allowed hostnames for ESI includes"),
    AP_INIT_FLAG("MesiBlockPrivateIPs", set_block_private_ips, NULL, RSRC_CONF, "Enable or disable private IP blocking (default: On)"),
    AP_INIT_FLAG("MesiAllowPrivateIPsForAllowedHosts", set_allow_private_for_allowed, NULL, RSRC_CONF, "Allow private IP access for hosts in MesiAllowedHosts when MesiBlockPrivateIPs is On (default: Off)"),
    AP_INIT_FLAG("MesiSharedHTTPClient", set_shared_http_client, NULL, RSRC_CONF, "Share HTTP client across ESI includes for connection pooling (default: Off)"),
    AP_INIT_FLAG("MesiDebug", set_debug, NULL, RSRC_CONF, "Enable debug logging from the ESI parser (default: Off). Output goes to stderr, captured in the Apache error log"),
    AP_INIT_FLAG("MesiParseOnHeader", set_parse_on_header, NULL, RSRC_CONF, "Only process ESI when the HTML response carries Edge-control: dca=esi (default: Off)"),
    AP_INIT_RAW_ARGS("MesiCacheBackend", set_cache_backend, NULL, RSRC_CONF, "Cache backend: \"memory\", \"redis\", \"memcached\" (\"\" explicitly disables; unset inherits). Default: off"),
    AP_INIT_TAKE1("MesiCacheSize", set_cache_size, NULL, RSRC_CONF, "Memory cache max entries (1..1000000). Default: 10000"),
    AP_INIT_TAKE1("MesiCacheTTL", set_cache_ttl, NULL, RSRC_CONF, "Memory cache entry TTL in seconds (0..86400). Default: 0 (no expiry)"),
    AP_INIT_RAW_ARGS("MesiCacheRedisAddr", set_cache_redis_addr, NULL, RSRC_CONF, "Redis server address for ESI caching (\"\" resets to the libgomesi default localhost:6379; unset inherits). Used when MesiCacheBackend is redis"),
    AP_INIT_RAW_ARGS("MesiCacheRedisPassword", set_cache_redis_password, NULL, RSRC_CONF, "Redis AUTH password (\"\" disables auth; unset inherits). Used when MesiCacheBackend is redis"),
    AP_INIT_TAKE1("MesiCacheRedisDB", set_cache_redis_db, NULL, RSRC_CONF, "Redis database number (0..15). Default: 0. Used when MesiCacheBackend is redis"),
    AP_INIT_RAW_ARGS("MesiCacheMemcachedServers", set_cache_memcached_servers, NULL, RSRC_CONF, "Space-separated list of Memcached servers (host:port). Used when MesiCacheBackend is memcached"),
    AP_INIT_TAKE1("MesiCacheKeyTemplate", set_cache_key_template, NULL, RSRC_CONF, "Cache key template: ${url}, ${header:Name}, ${cookie:Name} (default: mesi:${url}). Unknown placeholders stay literal."),
    {NULL}};

module AP_MODULE_DECLARE_DATA mesi_module = {
    STANDARD20_MODULE_STUFF,
    NULL, // no per-dir config (server-level only)
    NULL, // no per-dir merge
    create_server_config,
    merge_server_config,
    mesi_directives,
    register_hooks};
