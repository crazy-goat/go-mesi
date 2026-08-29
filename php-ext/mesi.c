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
 * PHP worker don't wipe out their cache by re-issuing InitCacheWithConfig.
 * libgomesi's InitCacheWithConfig always replaces sharedCache with a
 * freshly-built instance — that's correct semantics for ONE-shot init
 * by long-running embedders (nginx, Apache, CLI), but our PHP extension
 * is called many times per request so we'd lose every cache entry on
 * each call.
 *
 * cfg_json is built per backend: "{}" for memory/no-cache, a custom
 * blob for redis (redisAddr/redisPassword/redisDB) and memcached
 * (servers array). cfg_json[0] == '\0' is the "no cache" sentinel —
 * matches libgomesi semantics where `InitCacheWithConfig("", ..., "")`
 * returns 0 and leaves sharedCache == nil.
 */
#define MESI_CFG_MAX 4096            /* mirrors Apache MESI_MAX_CACHE_CONFIG_JSON */
#define MESI_BACKEND_MAX 16

typedef struct {
    char    backend[MESI_BACKEND_MAX]; /* "", "memory", "redis", "memcached" */
    long    size;
    long    ttl;
    char    cfg_json[MESI_CFG_MAX];   /* render of build_cache_config_json() */
} mesi_cache_state_t;

static mesi_cache_state_t g_cache_state = {"", -1, -1, {0}};

/*
 * Track the last blockPrivateIPs value we passed to InitHTTPClient so we
 * only re-create the shared HTTP client (and its SSRF-safe transport)
 * when the requested value actually changes. libgomesi's applySharedConfig
 * always wires the shared client into the parse config, so the dial-time
 * private-IP blocking is governed entirely by the transport created here —
 * ParseWithConfig's own blockPrivateIPs flag is only honoured when the
 * shared client carries the matching transport. The state starts at 0,
 * mirroring MINIT's InitHTTPClient(0) (blocking OFF until the first
 * parse_with_config call opts in).
 */
static int g_http_block_private_ips = 0;

static int mesi_cache_state_matches(const char *backend, long size, long ttl,
                                    const char *cfg_json) {
    if (backend[0] == '\0' && g_cache_state.backend[0] == '\0') {
        return g_cache_state.size == size
            && g_cache_state.ttl == ttl
            && strcmp(g_cache_state.cfg_json, cfg_json) == 0;
    }
    return strcmp(g_cache_state.backend, backend) == 0
        && g_cache_state.size == size
        && g_cache_state.ttl == ttl
        && strcmp(g_cache_state.cfg_json, cfg_json) == 0;
}

static void mesi_cache_state_record(const char *backend, long size, long ttl,
                                    const char *cfg_json) {
    strncpy(g_cache_state.backend, backend, sizeof(g_cache_state.backend) - 1);
    g_cache_state.backend[sizeof(g_cache_state.backend) - 1] = '\0';
    g_cache_state.size = size;
    g_cache_state.ttl = ttl;
    strncpy(g_cache_state.cfg_json, cfg_json, sizeof(g_cache_state.cfg_json) - 1);
    g_cache_state.cfg_json[sizeof(g_cache_state.cfg_json) - 1] = '\0';
}

/*
 * parse_with_config() caches results within a single PHP worker process.
 *
 * The PHP extension stores minimal persistent state — mostly a remembered
 * "last cache config" (g_cache_state) so we never call InitCacheWithConfig
 * twice with the same parameters; that would otherwise drop every
 * previously cached entry. The actual cache itself lives inside libgomesi.
 *
 * The legacy `parse(input, max_depth, default_url)` entrypoint remains
 * the recommended way for callers that do not need caching — it never
 * touches the cache and is unchanged.
 */

/*
 * Validation helpers — keep inputs deterministic. We reject any byte that
 * would force JSON escaping rather than escape it at runtime, so a hostile
 * password never injects JSON keys. The trade-off (no `"`, no `\\`, no
 * control chars in user-supplied values) matches Apache mod_mesi.c.
 */
static int mesi_is_safe_string(const char *s) {
    if (s == NULL) return 1;
    for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
        if (*p < 0x20) return 0;                       /* control chars */
        if (*p == 0x7f) return 0;                      /* DEL */
        if (*p == ' ' || *p == '\t') return 0;         /* OWS, see RFC 7230 */
        if (*p == '"' || *p == '\\') return 0;        /* JSON meta */
    }
    return 1;
}

/* Parse an unsigned decimal integer in [min, max] from `arg` (NUL-terminated).
 * Returns 1 on success and stores the parsed value in *out. Returns 0
 * otherwise; *out is left untouched. Whitespace is rejected — callers
 * feed us post-trim PHP strings. */
static int mesi_parse_uint_bounded(const char *arg, long min, long max, long *out) {
    if (!arg || !*arg) return 0;
    if (*arg < '0' || *arg > '9') return 0;  /* first byte must be a digit */
    long v = 0;
    for (const char *p = arg; *p; p++) {
        if (*p < '0' || *p > '9') return 0;  /* reject mid-string alpha/decimals */
        if (v > (LONG_MAX / 10) - 10) return 0;  /* overflow guard */
        v = v * 10 + (*p - '0');
        if (v > max) return 0;
    }
    if (v < min) return 0;
    *out = v;
    return 1;
}

/* Append one byte to dst at *pos. Returns 0 on overflow. */
static int mesi_putc(char *dst, size_t cap, size_t *pos, char c) {
    if (*pos + 1 >= cap) return 0;
    dst[(*pos)++] = c;
    return 1;
}

/* Append a JSON-escaped string (no surrounding quotes) into dst at *pos.
 * We never expect to escape because callers pass only "safe" strings —
 * this is a defence-in-depth caller-side guarantee, not a runtime path.
 * If a byte outside the safe set appears (e.g. control char slipped past
 * upstream), we encode \u00XX as a deterministic fallback so the JSON
 * is still valid. */
static int mesi_json_append_escape(char *dst, size_t cap, size_t *pos, const char *src) {
    for (const unsigned char *p = (const unsigned char *)src; *p; p++) {
        if (*p == '"') {
            if (!mesi_putc(dst, cap, pos, '\\')) return 0;
            if (!mesi_putc(dst, cap, pos, '"')) return 0;
        } else if (*p == '\\') {
            if (!mesi_putc(dst, cap, pos, '\\')) return 0;
            if (!mesi_putc(dst, cap, pos, '\\')) return 0;
        } else if (*p < 0x20) {
            static const char hex[] = "0123456789abcdef";
            if (!mesi_putc(dst, cap, pos, '\\')) return 0;
            if (!mesi_putc(dst, cap, pos, 'u')) return 0;
            if (!mesi_putc(dst, cap, pos, '0')) return 0;
            if (!mesi_putc(dst, cap, pos, '0')) return 0;
            if (!mesi_putc(dst, cap, pos, hex[(*p >> 4) & 0xf])) return 0;
            if (!mesi_putc(dst, cap, pos, hex[*p & 0xf])) return 0;
        } else {
            if (!mesi_putc(dst, cap, pos, (char)*p)) return 0;
        }
    }
    return 1;
}

/* Append a JSON-escaped host:port token (with surrounding ""). */
static int mesi_json_append_str(char *dst, size_t cap, size_t *pos, const char *src) {
    if (!mesi_putc(dst, cap, pos, '"')) return 0;
    if (!mesi_json_append_escape(dst, cap, pos, src)) return 0;
    if (!mesi_putc(dst, cap, pos, '"')) return 0;
    return 1;
}

/* Cookie value validator: allow space (0x20) but reject control chars,
 * DEL, '"' and '\'. Tab (0x09) is already <0x20 so rejected. */
static int mesi_is_safe_cookie_value(const char *s) {
    if (s == NULL) return 1;
    for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
        if (*p < 0x20) return 0;
        if (*p == 0x7f) return 0;
        if (*p == '"' || *p == '\\') return 0;
    }
    return 1;
}

/* Dynamic buffer helpers for requestCtxJSON — grow via realloc, never
 * silently truncate. */
static int mesi_dyn_ensure(char **buf, size_t *cap, size_t pos, size_t need) {
    if (pos + need < *cap) return 1;
    size_t new_cap = *cap ? *cap * 2 : 256;
    while (pos + need >= new_cap) new_cap *= 2;
    char *n = (char *)realloc(*buf, new_cap);
    if (!n) return 0;
    *buf = n;
    *cap = new_cap;
    return 1;
}

static int mesi_dyn_putc(char **buf, size_t *cap, size_t *pos, char c) {
    if (!mesi_dyn_ensure(buf, cap, *pos, 2)) return 0;
    (*buf)[(*pos)++] = c;
    return 1;
}

static int mesi_dyn_json_append_escape(char **buf, size_t *cap, size_t *pos, const char *src) {
    for (const unsigned char *p = (const unsigned char *)src; *p; p++) {
        if (*p == '"') {
            if (!mesi_dyn_putc(buf, cap, pos, '\\')) return 0;
            if (!mesi_dyn_putc(buf, cap, pos, '"')) return 0;
        } else if (*p == '\\') {
            if (!mesi_dyn_putc(buf, cap, pos, '\\')) return 0;
            if (!mesi_dyn_putc(buf, cap, pos, '\\')) return 0;
        } else if (*p < 0x20) {
            static const char hex[] = "0123456789abcdef";
            if (!mesi_dyn_putc(buf, cap, pos, '\\')) return 0;
            if (!mesi_dyn_putc(buf, cap, pos, 'u')) return 0;
            if (!mesi_dyn_putc(buf, cap, pos, '0')) return 0;
            if (!mesi_dyn_putc(buf, cap, pos, '0')) return 0;
            if (!mesi_dyn_putc(buf, cap, pos, hex[(*p >> 4) & 0xf])) return 0;
            if (!mesi_dyn_putc(buf, cap, pos, hex[*p & 0xf])) return 0;
        } else {
            if (!mesi_dyn_putc(buf, cap, pos, (char)*p)) return 0;
        }
    }
    return 1;
}

static int mesi_dyn_json_append_str(char **buf, size_t *cap, size_t *pos, const char *src) {
    if (!mesi_dyn_putc(buf, cap, pos, '"')) return 0;
    if (!mesi_dyn_json_append_escape(buf, cap, pos, src)) return 0;
    if (!mesi_dyn_putc(buf, cap, pos, '"')) return 0;
    return 1;
}

/*
 * build_cache_config_json renders the validated PHP-side cache options
 * into a JSON blob that libgomesi's InitCacheWithConfig accepts.
 *   backend == "" -> "{}" (no cache)
 *   backend == "memory" -> "{}" (config is optional for memory)
 *   backend == "redis" -> {"redisAddr":..., "redisPassword":..., "redisDB":N}
 *   backend == "memcached" -> {"servers":["h:p", ...]}
 *
 * Caller-side invariants (enforced by parse_with_config()):
 *   - host:port strings contain no whitespace, control chars, or JSON
 *     metacharacters and have a port in [1, 65535].
 *   - redis db is in [0, 15].
 *   - memcached servers array is non-empty.
 *
 * Returns 0 on success and the rendered JSON in `out`. Returns -1 on
 * overflow; the caller should treat overflow as configuration error.
 */
static int build_cache_config_json(const char *backend,
                                   const char *cache_redis_addr,
                                   const char *cache_redis_password,
                                   long cache_redis_db,
                                   zval *cache_memcached_servers,
                                   long cache_redis_db_set,
                                   char *out, size_t cap) {
    size_t pos = 0;
    out[0] = '\0';
    if (backend[0] == '\0' || strcmp(backend, "memory") == 0) {
        if (cap < 3) return -1;
        out[0] = '{'; out[1] = '}'; out[2] = '\0';
        return 0;
    }
    if (strcmp(backend, "redis") == 0) {
        /* redisAddr: required non-empty (already validated host:port) */
        if (cap < 32) return -1;
        memcpy(out + pos, "{\"redisAddr\":", 13); pos += 13;
        if (!mesi_json_append_str(out, cap, &pos, cache_redis_addr)) return -1;
        /* redisPassword: optional. Empty/missing -> "" */
        memcpy(out + pos, ",\"redisPassword\":", 17); pos += 17;
        if (!mesi_json_append_str(out, cap, &pos,
                cache_redis_password ? cache_redis_password : "")) return -1;
        /* redisDB: omit when unset; emit as int when explicitly set. */
        if (cache_redis_db_set) {
            int n = snprintf(out + pos, cap - pos, ",\"redisDB\":%ld", cache_redis_db);
            if (n < 0 || (size_t)n >= cap - pos) return -1;
            pos += (size_t)n;
        }
        if (pos + 2 >= cap) return -1;
        out[pos++] = '}';
        out[pos] = '\0';
        return 0;
    }
    if (strcmp(backend, "memcached") == 0) {
        if (!cache_memcached_servers || Z_TYPE_P(cache_memcached_servers) != IS_ARRAY) {
            /* validation should have rejected this — fail loudly */
            return -1;
        }
        if (cap < 16) return -1;
        memcpy(out + pos, "{\"servers\":[", 12); pos += 12;
        HashTable *ht = Z_ARRVAL_P(cache_memcached_servers);
        zval *val;
        int first = 1;
        ZEND_HASH_FOREACH_VAL(ht, val) {
            if (Z_TYPE_P(val) != IS_STRING) {
                /* validation should have rejected this — fail loudly */
                return -1;
            }
            if (!first) {
                if (pos + 1 >= cap) return -1;
                out[pos++] = ',';
            }
            if (!mesi_json_append_str(out, cap, &pos, Z_STRVAL_P(val))) return -1;
            first = 0;
        } ZEND_HASH_FOREACH_END();
        if (pos + 2 >= cap) return -1;
        out[pos++] = ']';
        out[pos++] = '}';
        out[pos] = '\0';
        return 0;
    }
    return -1;
}

/* parse_host_port validates a "host:port" string. Both sides must be
 * non-empty, port is digits in [1, 65535]. Rejects embedded whitespace,
 * control chars, and JSON-meta (which would invalidate an unescaped
 * embed in the JSON blob). Returns 1 on success. */
static int parse_host_port(const char *s) {
    if (!s || !*s) return 0;
    if (!mesi_is_safe_string(s)) return 0;
    const char *colon = strrchr(s, ':');
    if (!colon || colon == s || *(colon + 1) == '\0') return 0;
    long port = 0;
    if (!mesi_parse_uint_bounded(colon + 1, 1, 65535, &port)) return 0;
    return 1;
}

/* Return the byte length of the UTF-8 sequence at p if it decodes to a
 * rune that Go's strings.Fields treats as whitespace (the same set nginx's
 * mesi_allowed_hosts validator mirrors, see #354), else 0. Keeps the
 * PHP-side list tokenization identical to libgomesi's, so a value made
 * solely of Unicode whitespace can never slip past validation and
 * silently become an empty allowlist (= allow all hosts). p is followed
 * by a NUL terminator; reads never go past it (a truncated sequence
 * simply reports 0). */
static int mesi_unicode_ws_len(const unsigned char *p) {
    if (p[0] == 0xC2 && (p[1] == 0x85 || p[1] == 0xA0)) return 2;  /* U+0085, U+00A0 */
    if (p[0] == 0xE1 && p[1] == 0x9A && p[2] == 0x80) return 3;     /* U+1680 */
    if (p[0] == 0xE2 && p[1] == 0x80) {
        if ((p[2] >= 0x80 && p[2] <= 0x8A) || p[2] == 0xA8
            || p[2] == 0xA9 || p[2] == 0xAF) return 3;              /* U+2000-U+200A, U+2028, U+2029, U+202F */
    }
    if (p[0] == 0xE2 && p[1] == 0x81 && p[2] == 0x9F) return 3;     /* U+205F */
    if (p[0] == 0xE3 && p[1] == 0x80 && p[2] == 0x80) return 3;     /* U+3000 */
    return 0;
}

/* Does s contain at least one byte that is not whitespace under Go's
 * strings.Fields definition (space plus the Unicode set above)? Note
 * that tab/CR/LF and other control characters never reach this helper —
 * parse_with_config() rejects them earlier — so only the literal space
 * needs an explicit check here. Returns 1 if a hostname token is
 * present, 0 for empty/whitespace-only. */
static int mesi_allowed_hosts_has_token(const char *s) {
    const unsigned char *p = (const unsigned char *)s;
    while (*p) {
        if (*p == ' ') { p++; continue; }
        if (p[0] == 0xC2 || p[0] == 0xE1 || p[0] == 0xE2 || p[0] == 0xE3) {
            int n = mesi_unicode_ws_len(p);
            if (n > 0) { p += n; continue; }
        }
        return 1;
    }
    return 0;
}

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
 *   cache_backend:        "memory" | "redis" | "memcached" | "" (off).
 *                         Anything else rejects with E_WARNING.
 *   cache_size:           integer in [1, 1_000_000]. 0 == use default 10000.
 *   cache_ttl:            integer in [0, 86_400] seconds. 0 == no TTL.
 *   cache_redis_addr:     string "host:port" with port in [1, 65535].
 *                         Required for backend=redis; rejected for others.
 *   cache_redis_password: string without control chars / " / \. Allowed for
 *                         backend=redis; rejected for others.
 *   cache_redis_db:       integer in [0, 15]. Allowed for backend=redis.
 *   cache_memcached_servers: array of "host:port" strings. Required for
 *                            backend=memcached (non-empty); rejected for
 *                            others.
 *   block_private_ips:       bool. When true (the default when the key is
 *                            absent — secure by default), the shared HTTP
 *                            client is (re)built with a dial-time transport
 *                            that blocks connections to private/reserved IP
 *                            ranges, preventing SSRF via DNS rebinding. A
 *                            non-boolean value is rejected with E_WARNING.
 *   allowed_hosts:           string. Space-separated hostname whitelist
 *                            restricting which <esi:include> destinations
 *                            are fetched (e.g. "backend.internal
 *                            cdn.example.com"). Empty string (or absent)
 *                            allows all hosts — backward compatible.
 *                            Matching is exact or subdomain-suffix with a
 *                            '.' boundary, per the shared core — the PHP
 *                            layer only passes the list through to
 *                            libgomesi's ParseWithConfig. The whitelist
 *                            check runs before the dial-time private-IP
 *                            block and does NOT bypass it: includes to
 *                            private/reserved IPs still need
 *                            block_private_ips=false. Non-string values,
 *                            control characters, embedded NULs and
 *                            whitespace-only values (ASCII or Unicode) are
 *                            rejected with E_WARNING — a whitespace-only
 *                            value would silently tokenize to an empty
 *                            allowlist (= allow all hosts), the same
 *                            fail-open typo nginx hardened against (#354).
 *   allow_private_ips_for_allowed_hosts: bool. When true, hosts listed in
 *                            allowed_hosts may resolve to private/reserved
 *                            IP addresses — the dial-time block is bypassed
 *                            for them (per-host, in the shared core). Only
 *                            effective when BOTH block_private_ips=true
 *                            AND a non-empty allowed_hosts are set;
 *                            otherwise a no-op. Trusts DNS — a compromised
 *                            entry in allowed_hosts can reach internal/
 *                            private addresses (same security profile as
 *                            Apache MesiAllowPrivateIPsForAllowedHosts,
 *                            #168). Under the hood the parse detaches from
 *                            the shared HTTP client (whose transport bakes
 *                            block_private_ips at startup — attaching it
 *                            would silently negate the bypass), so
 *                            bypass-flagged parses use per-request clients
 *                            without connection pooling; the shared client
 *                            keeps serving all other parses. Absent =>
 *                            false (no bypass). A non-boolean value is
 *                            rejected with E_WARNING.
 *   cache_key_template:      string. Cache key template with placeholders
 *                            ${url}, ${header:Name}, ${cookie:Name}.
 *                            Empty/absent => default URL-only key
 *                            (mesi.DefaultCacheKey). Non-string values are
 *                            rejected with E_WARNING. Non-empty values must
 *                            pass mesi_is_safe_string (no control chars,
 *                            space/tab, DEL, '"' or '\') — same validator as
 *                            cache_redis_addr. Unknown placeholders are left
 *                            literal; case-insensitive header/cookie lookup
 *                            via mesi.BuildCacheKey. Only meaningful with a
 *                            cache backend; when cache_backend resolves to
 *                            "" the template is silently IGNORED (no warning
 *                            — parity with CLI/Traefik #246). Empty string
 *                            = unset.
 *   request_headers:         optional array. String keys (header names) =>
 *                            string or array-of-string values. Keys and all
 *                            values must pass mesi_is_safe_string. Any
 *                            violation (wrong type, bad chars, non-string
 *                            key) => E_WARNING + false. Empty array = no
 *                            headers. Only rendered into requestCtxJSON when
 *                            a non-empty cache_key_template is set and
 *                            backend != "".
 *   request_cookies:         optional array. String keys (cookie names,
 *                            non-empty, no spaces/control) => string values
 *                            (no control chars, no '"' or '\'). Values
 *                            deliberately use mesi_is_safe_cookie_value so
 *                            spaces are allowed in cookie values, unlike
 *                            header names/values and cookie names which use
 *                            the stricter mesi_is_safe_string set — do not
 *                            "fix" the divergence. Violations => E_WARNING
 *                            + false. Empty array = no cookies. Only
 *                            rendered into requestCtxJSON when a non-empty
 *                            cache_key_template is set and backend != "".
 *
 * Validation strictly mirrors libgomesi's InitCacheWithConfig contract —
 * we detect the same bad inputs libgomesi would silently ignore or silently
 * coerce, so a misconfigured cache_backend never appears to "succeed"
 * while caching nothing.
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

    const char *cache_backend = "";
    long cache_size = 0;     /* 0 == "not specified" → use default */
    long cache_ttl = 0;
    const char *cache_redis_addr = NULL;
    const char *cache_redis_password = NULL;
    long cache_redis_db = 0;
    long cache_redis_db_set = 0;  /* distinguish explicit 0 from "unset" */
    zval *cache_memcached_servers = NULL;

    /* block_private_ips: secure by default. Absent => true. */
    int block_private_ips = 1;
    /* allow_private_ips_for_allowed_hosts: no bypass by default (false). */
    int allow_private_ips_for_allowed_hosts = 0;

    /* allowed_hosts: hostname whitelist; empty (absent key) = all hosts
     * allowed. Passed verbatim to libgomesi ParseWithConfig. */
    const char *allowed_hosts = "";

    const char *cache_key_template = NULL;
    zval *request_headers = NULL;
    zval *request_cookies = NULL;

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
            if (strcmp(raw, "") != 0
                && strcmp(raw, "memory") != 0
                && strcmp(raw, "redis") != 0
                && strcmp(raw, "memcached") != 0) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): unsupported cache_backend '%s' "
                    "(allowed: 'memory', 'redis', 'memcached', or empty)", raw);
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

        /* Redis-only keys are only consulted when cache_backend == "redis".
         * Reading them for other backends would silently encourage
         * mismatched config; we reject (E_WARNING) instead. */
        val = zend_hash_str_find(Z_ARRVAL_P(config), "cache_redis_addr", sizeof("cache_redis_addr") - 1);
        if (val != NULL) {
            if (strcmp(cache_backend, "redis") != 0) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_addr is only valid with "
                    "cache_backend='redis' (got '%s')", cache_backend);
                RETURN_FALSE;
            }
            if (Z_TYPE_P(val) != IS_STRING) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_addr must be a string");
                RETURN_FALSE;
            }
            if (!parse_host_port(Z_STRVAL_P(val))) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_addr must be host:port with "
                    "port in [1, 65535] and no whitespace, control chars, '\"' or '\\\\' "
                    "(got: '%s')", Z_STRVAL_P(val));
                RETURN_FALSE;
            }
            cache_redis_addr = Z_STRVAL_P(val);
        }

        val = zend_hash_str_find(Z_ARRVAL_P(config), "cache_redis_password", sizeof("cache_redis_password") - 1);
        if (val != NULL) {
            if (strcmp(cache_backend, "redis") != 0) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_password is only valid with "
                    "cache_backend='redis' (got '%s')", cache_backend);
                RETURN_FALSE;
            }
            if (Z_TYPE_P(val) != IS_STRING) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_password must be a string");
                RETURN_FALSE;
            }
            if (!mesi_is_safe_string(Z_STRVAL_P(val))) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_password contains invalid characters "
                    "(no control chars, '\"' or '\\\\' allowed)");
                RETURN_FALSE;
            }
            cache_redis_password = Z_STRVAL_P(val);
        }

        val = zend_hash_str_find(Z_ARRVAL_P(config), "cache_redis_db", sizeof("cache_redis_db") - 1);
        if (val != NULL) {
            if (strcmp(cache_backend, "redis") != 0) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_db is only valid with "
                    "cache_backend='redis' (got '%s')", cache_backend);
                RETURN_FALSE;
            }
            if (Z_TYPE_P(val) != IS_LONG) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_db must be an integer");
                RETURN_FALSE;
            }
            long v = Z_LVAL_P(val);
            if (v < 0 || v > 15) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_redis_db %ld is out of range [0, 15]",
                    v);
                RETURN_FALSE;
            }
            cache_redis_db = v;
            cache_redis_db_set = 1;
        }

        val = zend_hash_str_find(Z_ARRVAL_P(config), "cache_memcached_servers", sizeof("cache_memcached_servers") - 1);
        if (val != NULL) {
            if (strcmp(cache_backend, "memcached") != 0) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_memcached_servers is only valid with "
                    "cache_backend='memcached' (got '%s')", cache_backend);
                RETURN_FALSE;
            }
            if (Z_TYPE_P(val) != IS_ARRAY) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_memcached_servers must be an array");
                RETURN_FALSE;
            }
            HashTable *ht = Z_ARRVAL_P(val);
            if (zend_hash_num_elements(ht) == 0) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_memcached_servers is required for "
                    "memcached backend and must contain at least one host:port entry");
                RETURN_FALSE;
            }
            /* Validate each entry up front. */
            zval *elt;
            ZEND_HASH_FOREACH_VAL(ht, elt) {
                if (Z_TYPE_P(elt) != IS_STRING || !parse_host_port(Z_STRVAL_P(elt))) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): every cache_memcached_servers entry "
                        "must be host:port with port in [1, 65535] and no whitespace, "
                        "control chars, '\"' or '\\\\' (got: '%s')",
                        Z_TYPE_P(elt) == IS_STRING ? Z_STRVAL_P(elt) : "<non-string>");
                    RETURN_FALSE;
                }
            } ZEND_HASH_FOREACH_END();
            cache_memcached_servers = val;
        }

        /* block_private_ips: SSRF dial-time private-IP blocking.
         * Accepted as a bool or int (0/1); any other type is rejected so a
         * typo never silently disables SSRF protection. Absent => keep the
         * secure default (true). */
        val = zend_hash_str_find(Z_ARRVAL_P(config), "block_private_ips", sizeof("block_private_ips") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) == IS_TRUE || Z_TYPE_P(val) == IS_FALSE) {
                block_private_ips = (Z_TYPE_P(val) == IS_TRUE);
            } else if (Z_TYPE_P(val) == IS_LONG) {
                block_private_ips = (Z_LVAL_P(val) != 0);
            } else {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): block_private_ips must be a boolean "
                    "(true/false) or integer (non-zero = block)");
                RETURN_FALSE;
            }
        }

        /* allowed_hosts: hostname whitelist (space-separated string, passed
         * verbatim to libgomesi's ParseWithConfig allowedHosts argument).
         * Absent/empty => no restriction (all hosts allowed). Validation is
         * deliberately strict: a typo must never silently fail open. */
        val = zend_hash_str_find(Z_ARRVAL_P(config), "allowed_hosts", sizeof("allowed_hosts") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) != IS_STRING) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): allowed_hosts must be a string "
                    "(space-separated hostnames, e.g. 'backend.internal cdn.example.com')");
                RETURN_FALSE;
            }
            const char *raw = Z_STRVAL_P(val);
            size_t raw_len = Z_STRLEN_P(val);
            /* Reject embedded NULs: the C string handed to libgomesi would
             * silently truncate at the first NUL, dropping the rest of the
             * list. */
            if (memchr(raw, '\0', raw_len) != NULL) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): allowed_hosts contains a NUL byte");
                RETURN_FALSE;
            }
            /* Reject control characters — never valid in hostnames. */
            for (size_t i = 0; i < raw_len; i++) {
                unsigned char c = (unsigned char)raw[i];
                if (c < 0x20 || c == 0x7f) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): allowed_hosts contains control characters");
                    RETURN_FALSE;
                }
            }
            /* Reject empty/whitespace-only values — unless the string is
             * completely empty, which is the documented "no restriction"
             * (backward compatible). libgomesi splits with strings.Fields,
             * so a whitespace-only value would silently become an empty
             * allowlist = allow ALL hosts — the same fail-open typo nginx
             * hardened against in #354. */
            if (raw_len > 0 && !mesi_allowed_hosts_has_token(raw)) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): allowed_hosts must contain at least one "
                    "hostname (whitespace-only values would silently allow all hosts)");
                RETURN_FALSE;
            }
            allowed_hosts = raw;
        }

        /* allow_private_ips_for_allowed_hosts: per-host private-IP bypass
         * for allowed_hosts entries. Accepted as bool or int (0/1); any
         * other type is rejected so a typo never silently enables a
         * security bypass (SSRF protection relies on it never being set
         * by accident). Absent => false (no bypass). The bypass only
         * takes effect when BOTH block_private_ips=true AND a non-empty
         * allowed_hosts are set — enforced by the shared core (and by
         * libgomesi detaching the shared client for bypass parses). */
        val = zend_hash_str_find(Z_ARRVAL_P(config), "allow_private_ips_for_allowed_hosts",
                                 sizeof("allow_private_ips_for_allowed_hosts") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) == IS_TRUE || Z_TYPE_P(val) == IS_FALSE) {
                allow_private_ips_for_allowed_hosts = (Z_TYPE_P(val) == IS_TRUE);
            } else if (Z_TYPE_P(val) == IS_LONG) {
                allow_private_ips_for_allowed_hosts = (Z_LVAL_P(val) != 0);
            } else {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): allow_private_ips_for_allowed_hosts must be "
                    "a boolean (true/false) or integer (non-zero = enabled)");
                RETURN_FALSE;
            }
        }

        /* cache_key_template: optional string; empty = unset. Must pass
         * mesi_is_safe_string (no control chars, space/tab, DEL, '"' or '\').
         * Non-string => E_WARNING. Empty string is treated as absent.
         * When cache_backend == "" the template is silently ignored (no
         * warning) — parity with CLI/Traefik #246; we still validate the
         * string type/safe chars before ignoring so typos are not hidden
         * when a backend is present, but a valid template with no backend
         * is a no-op. */
        val = zend_hash_str_find(Z_ARRVAL_P(config), "cache_key_template", sizeof("cache_key_template") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) != IS_STRING) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): cache_key_template must be a string");
                RETURN_FALSE;
            }
            const char *raw = Z_STRVAL_P(val);
            size_t raw_len = Z_STRLEN_P(val);
            if (raw_len == 0) {
                cache_key_template = NULL;
            } else {
                if (memchr(raw, '\0', raw_len) != NULL) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): cache_key_template contains a NUL byte");
                    RETURN_FALSE;
                }
                if (!mesi_is_safe_string(raw)) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): cache_key_template contains invalid characters "
                        "(no control chars, spaces, '\"' or '\\\\' allowed)");
                    RETURN_FALSE;
                }
                cache_key_template = raw;
            }
        }

        /* request_headers: optional array, string keys => string or
         * array-of-string values. Keys and all string values must pass
         * mesi_is_safe_string. Empty array = no headers. */
        val = zend_hash_str_find(Z_ARRVAL_P(config), "request_headers", sizeof("request_headers") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) != IS_ARRAY) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): request_headers must be an array");
                RETURN_FALSE;
            }
            HashTable *ht = Z_ARRVAL_P(val);
            zend_string *k;
            zval *hv;
            ZEND_HASH_FOREACH_STR_KEY_VAL(ht, k, hv) {
                if (k == NULL) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): request_headers keys must be strings");
                    RETURN_FALSE;
                }
                const char *key_str = ZSTR_VAL(k);
                if (ZSTR_LEN(k) == 0 || !mesi_is_safe_string(key_str)) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): request_headers key '%s' contains invalid characters "
                        "(no control chars, spaces, '\"' or '\\\\' allowed, non-empty)", key_str);
                    RETURN_FALSE;
                }
                if (Z_TYPE_P(hv) == IS_STRING) {
                    if (!mesi_is_safe_string(Z_STRVAL_P(hv))) {
                        php_error_docref(NULL, E_WARNING,
                            "mesi\\parse_with_config(): request_headers value for '%s' contains invalid characters "
                            "(no control chars, spaces, '\"' or '\\\\' allowed)", key_str);
                        RETURN_FALSE;
                    }
                } else if (Z_TYPE_P(hv) == IS_ARRAY) {
                    HashTable *inner = Z_ARRVAL_P(hv);
                    zval *elt;
                    ZEND_HASH_FOREACH_VAL(inner, elt) {
                        if (Z_TYPE_P(elt) != IS_STRING) {
                            php_error_docref(NULL, E_WARNING,
                                "mesi\\parse_with_config(): request_headers array value for '%s' must contain only strings", key_str);
                            RETURN_FALSE;
                        }
                        if (!mesi_is_safe_string(Z_STRVAL_P(elt))) {
                            php_error_docref(NULL, E_WARNING,
                                "mesi\\parse_with_config(): request_headers array value for '%s' contains invalid characters "
                                "(no control chars, spaces, '\"' or '\\\\' allowed)", key_str);
                            RETURN_FALSE;
                        }
                    } ZEND_HASH_FOREACH_END();
                } else {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): request_headers value for '%s' must be a string or array of strings", key_str);
                    RETURN_FALSE;
                }
            } ZEND_HASH_FOREACH_END();
            request_headers = val;
        }

        /* request_cookies: optional array, string keys (cookie names,
         * non-empty, no spaces/control) => string values (allow spaces but
         * no control chars, '"' or '\'). */
        val = zend_hash_str_find(Z_ARRVAL_P(config), "request_cookies", sizeof("request_cookies") - 1);
        if (val != NULL) {
            if (Z_TYPE_P(val) != IS_ARRAY) {
                php_error_docref(NULL, E_WARNING,
                    "mesi\\parse_with_config(): request_cookies must be an array");
                RETURN_FALSE;
            }
            HashTable *ht = Z_ARRVAL_P(val);
            zend_string *k;
            zval *hv;
            ZEND_HASH_FOREACH_STR_KEY_VAL(ht, k, hv) {
                if (k == NULL) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): request_cookies keys must be strings");
                    RETURN_FALSE;
                }
                const char *key_str = ZSTR_VAL(k);
                if (ZSTR_LEN(k) == 0 || !mesi_is_safe_string(key_str)) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): request_cookies key '%s' contains invalid characters "
                        "(no control chars, spaces, '\"' or '\\\\' allowed, non-empty)", key_str);
                    RETURN_FALSE;
                }
                if (Z_TYPE_P(hv) != IS_STRING) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): request_cookies value for '%s' must be a string", key_str);
                    RETURN_FALSE;
                }
                if (!mesi_is_safe_cookie_value(Z_STRVAL_P(hv))) {
                    php_error_docref(NULL, E_WARNING,
                        "mesi\\parse_with_config(): request_cookies value for '%s' contains invalid characters "
                        "(no control chars, '\"' or '\\\\' allowed)", key_str);
                    RETURN_FALSE;
                }
            } ZEND_HASH_FOREACH_END();
            request_cookies = val;
        }

        /* Backend-specific requirements: redis requires addr; memcached
         * requires servers. Detected after per-key parsing so a stray
         * key doesn't by itself trigger the error. */
        if (strcmp(cache_backend, "redis") == 0
            && cache_redis_addr == NULL) {
            php_error_docref(NULL, E_WARNING,
                "mesi\\parse_with_config(): cache_redis_addr is required for "
                "cache_backend='redis'");
            RETURN_FALSE;
        }
        if (strcmp(cache_backend, "memcached") == 0
            && cache_memcached_servers == NULL) {
            php_error_docref(NULL, E_WARNING,
                "mesi\\parse_with_config(): cache_memcached_servers is required for "
                "cache_backend='memcached' and must contain at least one host:port entry");
            RETURN_FALSE;
        }
    }

    /* Resolve defaults for unspecified keys. cache_size == 0 means the
     * user did not pass an explicit value; substitute the documented
     * default (10000) — same contract as libgomesi's memory backend. */
    if (cache_size == 0) {
        cache_size = 10000;
    }

    /* Build the JSON blob InitCacheWithConfig expects. */
    char config_json[MESI_CFG_MAX];
    if (build_cache_config_json(cache_backend,
                                cache_redis_addr,
                                cache_redis_password,
                                cache_redis_db,
                                cache_memcached_servers,
                                cache_redis_db_set,
                                config_json, sizeof(config_json)) != 0) {
        php_error_docref(NULL, E_WARNING,
            "mesi\\parse_with_config(): failed to render cache config JSON "
            "(backend=%s)", cache_backend);
        RETURN_FALSE;
    }

    /* If config matches the last successful init, do NOT call InitCache* —
     * that would replace sharedCache with a fresh, empty instance and
     * silently disable the cache. */
    if (!mesi_cache_state_matches(cache_backend, cache_size, cache_ttl, config_json)) {
        int cache_rc = InitCacheWithConfig((char*)cache_backend,
                                           (int)cache_size,
                                           (int)cache_ttl,
                                           config_json);
        if (cache_rc != 0) {
            php_error_docref(NULL, E_WARNING,
                "mesi\\parse_with_config(): InitCacheWithConfig('%s', %ld, %ld, '%s') failed",
                cache_backend, cache_size, cache_ttl, config_json);
            RETURN_FALSE;
        }
        mesi_cache_state_record(cache_backend, cache_size, cache_ttl, config_json);
    }

    /* (Re)build the shared HTTP client's SSRF-safe transport only when the
     * requested block_private_ips value differs from the one currently in
     * effect. InitHTTPClient swaps in a fresh transport, so we avoid doing
     * it on every call — mirroring the cache-state tracking above. */
    if (block_private_ips != g_http_block_private_ips) {
        InitHTTPClient(block_private_ips ? 1 : 0);
        g_http_block_private_ips = block_private_ips;
    }

    /* Decide template and request context for ParseWithConfigCtx.
     * When cache_backend == "" the template is silently IGNORED (parity
     * with CLI/Traefik #246 — no warning). Otherwise render
     * requestCtxJSON from request_headers/request_cookies. */
    const char *tmpl_for_ctx = "";
    char *ctx_json = NULL;
    char *ctx_json_buf = NULL;

    if (cache_key_template != NULL && cache_backend[0] != '\0') {
        tmpl_for_ctx = cache_key_template;
        /* Render {"headers":{...},"cookies":[...]} — grow/realloc, never
         * truncate. On render failure emit E_WARNING + false (loud). */
        char *buf = NULL;
        size_t cap = 0, pos = 0;
        buf = (char *)malloc(256);
        if (!buf) {
            php_error_docref(NULL, E_WARNING,
                "mesi\\parse_with_config(): failed to allocate request context JSON");
            RETURN_FALSE;
        }
        cap = 256; pos = 0;
        if (!mesi_dyn_putc(&buf, &cap, &pos, '{')) goto ctx_oom;
        /* headers */
        {
            if (!mesi_dyn_putc(&buf, &cap, &pos, '"')) goto ctx_oom;
            const char *hk = "headers"; for (const char*p=hk;*p;p++) if(!mesi_dyn_putc(&buf,&cap,&pos,*p)) goto ctx_oom;
            if (!mesi_dyn_putc(&buf, &cap, &pos, '"')) goto ctx_oom;
            if (!mesi_dyn_putc(&buf, &cap, &pos, ':')) goto ctx_oom;
            if (!mesi_dyn_putc(&buf, &cap, &pos, '{')) goto ctx_oom;
            int first = 1;
            if (request_headers) {
                zend_string *k; zval *hv;
                ZEND_HASH_FOREACH_STR_KEY_VAL(Z_ARRVAL_P(request_headers), k, hv) {
                    const char *key_str = ZSTR_VAL(k);
                    if (!first) { if (!mesi_dyn_putc(&buf, &cap, &pos, ',')) goto ctx_oom; }
                    first = 0;
                    if (!mesi_dyn_json_append_str(&buf, &cap, &pos, key_str)) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, ':')) goto ctx_oom;
                    if (Z_TYPE_P(hv) == IS_STRING) {
                        if (!mesi_dyn_json_append_str(&buf, &cap, &pos, Z_STRVAL_P(hv))) goto ctx_oom;
                    } else { /* array-of-string */
                        if (!mesi_dyn_putc(&buf, &cap, &pos, '[')) goto ctx_oom;
                        int inner_first = 1;
                        HashTable *inner = Z_ARRVAL_P(hv);
                        zval *elt;
                        ZEND_HASH_FOREACH_VAL(inner, elt) {
                            if (!inner_first) { if (!mesi_dyn_putc(&buf, &cap, &pos, ',')) goto ctx_oom; }
                            inner_first = 0;
                            if (!mesi_dyn_json_append_str(&buf, &cap, &pos, Z_STRVAL_P(elt))) goto ctx_oom;
                        } ZEND_HASH_FOREACH_END();
                        if (!mesi_dyn_putc(&buf, &cap, &pos, ']')) goto ctx_oom;
                    }
                } ZEND_HASH_FOREACH_END();
            }
            if (!mesi_dyn_putc(&buf, &cap, &pos, '}')) goto ctx_oom;
        }
        if (!mesi_dyn_putc(&buf, &cap, &pos, ',')) goto ctx_oom;
        /* cookies */
        {
            if (!mesi_dyn_putc(&buf, &cap, &pos, '"')) goto ctx_oom;
            const char *ck = "cookies"; for (const char*p=ck;*p;p++) if(!mesi_dyn_putc(&buf,&cap,&pos,*p)) goto ctx_oom;
            if (!mesi_dyn_putc(&buf, &cap, &pos, '"')) goto ctx_oom;
            if (!mesi_dyn_putc(&buf, &cap, &pos, ':')) goto ctx_oom;
            if (!mesi_dyn_putc(&buf, &cap, &pos, '[')) goto ctx_oom;
            int first = 1;
            if (request_cookies) {
                zend_string *k; zval *hv;
                ZEND_HASH_FOREACH_STR_KEY_VAL(Z_ARRVAL_P(request_cookies), k, hv) {
                    const char *key_str = ZSTR_VAL(k);
                    if (!first) { if (!mesi_dyn_putc(&buf, &cap, &pos, ',')) goto ctx_oom; }
                    first = 0;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, '{')) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, '"')) goto ctx_oom;
                    const char *nk="name"; for(const char*p=nk;*p;p++) if(!mesi_dyn_putc(&buf,&cap,&pos,*p)) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, '"')) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, ':')) goto ctx_oom;
                    if (!mesi_dyn_json_append_str(&buf, &cap, &pos, key_str)) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, ',')) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, '"')) goto ctx_oom;
                    const char *vk="value"; for(const char*p=vk;*p;p++) if(!mesi_dyn_putc(&buf,&cap,&pos,*p)) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, '"')) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, ':')) goto ctx_oom;
                    if (!mesi_dyn_json_append_str(&buf, &cap, &pos, Z_STRVAL_P(hv))) goto ctx_oom;
                    if (!mesi_dyn_putc(&buf, &cap, &pos, '}')) goto ctx_oom;
                } ZEND_HASH_FOREACH_END();
            }
            if (!mesi_dyn_putc(&buf, &cap, &pos, ']')) goto ctx_oom;
        }
        if (!mesi_dyn_putc(&buf, &cap, &pos, '}')) goto ctx_oom;
        if (!mesi_dyn_ensure(&buf, &cap, pos, 1)) goto ctx_oom;
        buf[pos] = '\0';
        ctx_json_buf = buf;
        ctx_json = buf;
        goto ctx_done;
ctx_oom:
        free(buf);
        php_error_docref(NULL, E_WARNING,
            "mesi\\parse_with_config(): failed to render request context JSON (allocation failure)");
        RETURN_FALSE;
ctx_done: ;
    } else {
        ctx_json = (char*)"";
    }

    char* result = ParseWithConfigCtx(input, max_depth, default_url, (char*)allowed_hosts,
                                      block_private_ips ? 1 : 0,
                                      allow_private_ips_for_allowed_hosts ? 1 : 0,
                                      (char*)tmpl_for_ctx,
                                      ctx_json && *ctx_json ? ctx_json : (char*)"");
    if (ctx_json_buf) free(ctx_json_buf);
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
    g_cache_state.cfg_json[0] = '\0';
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
