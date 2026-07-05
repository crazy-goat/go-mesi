#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include <php.h>
#include <string.h>
#include "../libgomesi/libgomesi.h"

ZEND_BEGIN_ARG_INFO_EX(arginfo_parse, 0, 0, 3)
    ZEND_ARG_INFO(0, input)
    ZEND_ARG_INFO(0, max_depth)
    ZEND_ARG_INFO(0, default_url)
ZEND_END_ARG_INFO()

ZEND_BEGIN_ARG_INFO_EX(arginfo_parse_with_config, 0, 0, 4)
    ZEND_ARG_INFO(0, input)
    ZEND_ARG_INFO(0, max_depth)
    ZEND_ARG_INFO(0, default_url)
    ZEND_ARG_INFO(0, config)
ZEND_END_ARG_INFO()

/*
 * In-process state: track the last cache configuration we passed to
 * libgomesi so that repeated parse_with_config() calls within the same
 * PHP worker don't wipe out their in-memory cache by re-issuing InitCache.
 * libgomesi's InitCache always replaces sharedCache with a freshly-built
 * instance — that's correct semantics for ONE-shot init by long-running
 * embedders (nginx, Apache, CLI), but our PHP extension is called many
 * times per request so we'd lose every cache entry on each call.
 *
 * Empty cache_backend ("no cache") is also tracked so a first call
 * without a backend followed by a later call with a backend triggers
 * a single InitCache.
 */
typedef struct {
    char    backend[16];   /* "memory" or empty; no other values accepted */
    long    size;
    long    ttl;
} mesi_cache_state_t;

static mesi_cache_state_t g_cache_state = {"", -1, -1};

static int mesi_cache_state_matches(const char *backend, long size, long ttl) {
    if (g_cache_state.backend[0] == '\0' && backend[0] == '\0') {
        return g_cache_state.size == size && g_cache_state.ttl == ttl;
    }
    return strcmp(g_cache_state.backend, backend) == 0
        && g_cache_state.size == size
        && g_cache_state.ttl == ttl;
}

static void mesi_cache_state_record(const char *backend, long size, long ttl) {
    strncpy(g_cache_state.backend, backend, sizeof(g_cache_state.backend) - 1);
    g_cache_state.backend[sizeof(g_cache_state.backend) - 1] = '\0';
    g_cache_state.size = size;
    g_cache_state.ttl = ttl;
}

/*
 * parse_with_config() caches results within a single PHP worker process.
 *
 * The PHP extension stores minimal persistent state — mostly a remembered
 * "last cache config" (g_cache_state) so we never call InitCache twice
 * with the same parameters; that would otherwise drop every previously
 * cached entry. The actual cache itself lives inside libgomesi.
 *
 * The legacy `parse(input, max_depth, default_url)` entrypoint remains
 * the recommended way for callers that do not need caching — it never
 * touches the cache and is unchanged.
 */

PHP_FUNCTION(parse) {
    char *input, *default_url;
    size_t input_len, default_url_len;
    zend_long max_depth;

    ZEND_PARSE_PARAMETERS_START(3, 3)
        Z_PARAM_STRING(input, input_len)
        Z_PARAM_LONG(max_depth)
        Z_PARAM_STRING(default_url, default_url_len)
    ZEND_PARSE_PARAMETERS_END();

    char* result = Parse(input, max_depth, default_url);
    RETVAL_STRING(result);
    FreeString(result);
}

/*
 * parse_with_config() accepts a config array with optional keys:
 *   cache_backend: must be "memory" or absent/empty — anything else is
 *                  rejected with an E_WARNING to prevent silent cache
 *                  disablement from a typo.
 *   cache_size:    integer in [1, 1_000_000]. 0 or negative is treated
 *                  as "use the default" (10000) on the first init so a
 *                  caller that legitimately omits the default doesn't
 *                  have to know it. Any value outside the documented
 *                  range is rejected with E_WARNING.
 *   cache_ttl:     non-negative integer in [0, 86_400] seconds; 0 means
 *                  "no TTL". Any value outside the documented range is
 *                  rejected with E_WARNING.
 *
 * Validation strictly mirrors libgomesi's InitCache contract — we
 * detect the same bad inputs libgomesi would silently coerce, so a
 * misconfigured cache_backend never appears to "succeed" while caching
 * nothing.
 */
PHP_FUNCTION(parse_with_config) {
    char *input, *default_url;
    size_t input_len, default_url_len;
    zend_long max_depth;
    zval *config = NULL;

    ZEND_PARSE_PARAMETERS_START(4, 4)
        Z_PARAM_STRING(input, input_len)
        Z_PARAM_LONG(max_depth)
        Z_PARAM_STRING(default_url, default_url_len)
        Z_PARAM_ARRAY(config)
    ZEND_PARSE_PARAMETERS_END();

    /* Validate cache_backend (if present) — strict white-list. */
    const char *cache_backend = "";
    long cache_size = 0;     /* 0 == "not specified" → use default */
    long cache_ttl = 0;
    if (config != NULL && Z_TYPE_P(config) == IS_ARRAY) {
        zval *val;

        val = zend_hash_str_find(Z_ARRVAL_P(config), "cache_backend", sizeof("cache_backend") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) != IS_STRING) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_backend must be a string");
                RETURN_FALSE;
            }
            const char *raw = Z_STRVAL_P(val);
            if (strcmp(raw, "") != 0 && strcmp(raw, "memory") != 0) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): unsupported cache_backend '%s' "
                    "(only 'memory' is currently exposed to PHP)", raw);
                RETURN_FALSE;
            }
            cache_backend = raw;
        }

        val = zend_hash_str_find(Z_ARRVAL_P(config), "cache_size", sizeof("cache_size") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) != IS_LONG) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_size must be an integer");
                RETURN_FALSE;
            }
            long v = Z_LVAL_P(val);
            if (v < 1 || v > 1000000) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_size %ld is out of range [1, 1000000]",
                    v);
                RETURN_FALSE;
            }
            cache_size = v;
        }

        val = zend_hash_str_find(Z_ARRVAL_P(config), "cache_ttl", sizeof("cache_ttl") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) != IS_LONG) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_ttl must be an integer");
                RETURN_FALSE;
            }
            long v = Z_LVAL_P(val);
            if (v < 0 || v > 86400) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_ttl %ld is out of range [0, 86400]",
                    v);
                RETURN_FALSE;
            }
            cache_ttl = v;
        }
    }

    /* Resolve defaults for unspecified keys. */
    if (cache_size == 0) {
        cache_size = 10000;
    }

    /* If config matches the last successful init, do NOT call InitCache
     * again — that would replace sharedCache with a fresh, empty
     * instance and silently disable the cache. */
    if (!mesi_cache_state_matches(cache_backend, cache_size, cache_ttl)) {
        int cache_rc = InitCache((char*)cache_backend, (int)cache_size, (int)cache_ttl);
        if (cache_rc != 0) {
            php_error_docref(NULL, E_WARNING,
                "mesi\\parse_with_config(): InitCache('%s', %ld, %ld) failed",
                cache_backend, cache_size, cache_ttl);
            RETURN_FALSE;
        }
        mesi_cache_state_record(cache_backend, cache_size, cache_ttl);
    }

    char* result = Parse(input, max_depth, default_url);
    RETVAL_STRING(result);
    FreeString(result);
}

PHP_MINIT_FUNCTION(mesi) {
    InitHTTPClient(0);
    return SUCCESS;
}

PHP_MSHUTDOWN_FUNCTION(mesi) {
    FreeHTTPClient();
    FreeCache();
    /* g_cache_state will be re-initialised on next module load. */
    g_cache_state.backend[0] = '\0';
    g_cache_state.size = -1;
    g_cache_state.ttl = -1;
    return SUCCESS;
}

zend_function_entry mesi_functions[] = {
    ZEND_NS_FE("mesi", parse, arginfo_parse)
    ZEND_NS_FE("mesi", parse_with_config, arginfo_parse_with_config)
    PHP_FE_END
};

zend_module_entry mesi_module_entry = {
    STANDARD_MODULE_HEADER,
    "mesi",
    mesi_functions,
    PHP_MINIT(mesi),
    PHP_MSHUTDOWN(mesi),
    NULL,
    NULL,
    NULL,
    "0.1",
    STANDARD_MODULE_PROPERTIES
};

ZEND_GET_MODULE(mesi)
