/*
 * Shared mesi_config definition and pure directive logic for mod_mesi.
 *
 * The definitions live in this header (and not in mod_mesi.c) so the C unit
 * tests in test_directives.c exercise the real config, validators, setters
 * and merge rules instead of hand-copied mirrors (#558). Everything here is
 * Apache-free APR C: no cmd_parms, request_rec or server_rec types. mod_mesi.c
 * keeps only the thin cmd_parms glue that fetches the per-server config and
 * forwards to these functions, so a production validator change can never
 * drift from the unit tests.
 *
 * All helpers are static inline: the module and the unit harness each get
 * their own internal-linkage copy, so neither exports any new symbol.
 */
#ifndef MOD_MESI_CONFIG_H
#define MOD_MESI_CONFIG_H

#include <stddef.h>
#include <string.h>
#include <strings.h>

#include <apr_pools.h>
#include <apr_strings.h>
#include <apr_tables.h>

/* Cache merge lives in cache_config.h so the tests exercise the same
 * NULL-vs-"" selection (#617). */
#include "cache_config.h"

typedef struct {
    int enable_mesi;
    apr_array_header_t *allowed_hosts;
    int block_private_ips; // -1=unset, 0=off, 1=on
    // Allow hosts in allowed_hosts to bypass BlockPrivateIPs (SSRF dial
    // block) when they resolve to private/reserved IPs. -1=unset,
    // 0=off, 1=on. Only effective when BOTH block_private_ips is on AND
    // allowed_hosts is set. Default (unset → off) keeps private IPs
    // always blocked regardless of allowed_hosts membership.
    int allow_private_ips_for_allowed; // -1=unset, 0=off, 1=on
    // Share a single http.Client across all <esi:include> fetches in this
    // worker process for TCP/TLS connection pooling. -1=unset, 0=off, 1=on.
    // Default (unset → off) keeps the original per-include client creation
    // so behaviour is unchanged unless an operator opts in. The shared
    // client is created in mesi_child_init via libgomesi InitHTTPClient and
    // honours the effective MesiBlockPrivateIPs setting at startup.
    int shared_http_client; // -1=unset, 0=off, 1=on
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
    const char *cache_key_template; // NULL/empty = DefaultCacheKey
    // ESI nesting depth (#166). -1 = unset (filter uses 5). Explicit 0
    // is valid passthrough (no ESI fetch). Range [0, MESI_MAX_MAX_DEPTH]
    // matches mesi.MaxMaxDepth / Caddy.
    int max_depth; // -1=unset, >=0 = configured
    // Global per-include fetch budget in seconds (#167). -1 = unset:
    // the filter stays on the legacy parse path and libgomesi applies
    // its default 30s (config.DefaultTimeoutSeconds — the
    // MESI_DEFAULT_TIMEOUT_SECONDS macro below only documents that
    // value, the filter never substitutes it). Range [1,
    // MESI_MAX_TIMEOUT_SECONDS]; 0 is REJECTED at config load: the
    // core treats Timeout <= 0 as "budget already exhausted" and fails
    // every include immediately with ErrTimeBudgetExceeded
    // (mesi/fetch.go) — it does NOT mean "no timeout".
    int timeout_seconds; // -1=unset, >=1 = configured
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
    apr_off_t max_response_size; // -1=unset, >=0 = configured (bytes)
    // Cap on the parent (origin) response body that is buffered for ESI
    // processing, in bytes. -1 = unset and 0 = unlimited (no limit,
    // pre-change behaviour). Over the limit the response is handled per
    // on_oversize.
    apr_off_t max_body_size; // -1=unset, >=0 = configured (bytes)
    int on_oversize;         // -1=unset, MESI_ON_OVERSIZE_PASS (default) or _ERROR
    // Cap on concurrent <esi:include> HTTP fetches within one page
    // render (one MESIParse call — per Apache worker thread under
    // MPM worker/event, not process-wide) (#170). -1 = unset: the
    // filter stays on the legacy parse path and libgomesi leaves
    // EsiParserConfig.MaxConcurrentRequests at 0, which the core
    // treats as "unlimited" (mesi/parser.go only installs the
    // admission-control semaphore when > 0) — byte-identical to
    // pre-#170 Apache behaviour. Explicit 0 is a legitimate
    // configured value with the same "unlimited" meaning (documented
    // contract shared with Caddy `max_concurrent_requests 0`), so the
    // unset sentinel MUST stay -1 for 0 to survive the merge below.
    // Range [0, MESI_MAX_MAX_CONCURRENT_REQUESTS].
    int max_concurrent_requests; // -1=unset, >=0 = configured
    // Cap on the include worker pool that drains ESI jobs within one
    // page render (one MESIParse call — each nesting level spawns its
    // own pool and inherits the cap; per Apache worker thread under
    // MPM worker/event, not process-wide) (#171). -1 = unset: the
    // filter stays on the legacy parse path and libgomesi leaves
    // EsiParserConfig.MaxWorkers at 0, which the core treats as the
    // library default runtime.NumCPU()*4 (mesi/parser.go substitutes
    // it for any value <= 0) — byte-identical to pre-#171 Apache
    // behaviour. Explicit 0 is a legitimate configured value with the
    // same "library default" meaning (documented contract shared with
    // Caddy `max_workers 0`), so the unset sentinel MUST stay -1 for
    // 0 to survive the merge below. Range [0,
    // MESI_MAX_MAX_WORKERS].
    int max_workers; // -1=unset, >=0 = configured
    // Debug logging from the Go parser (#173). -1=unset, 0=off, 1=on.
    // Default (unset → off) keeps the parser silent, byte-identical to
    // previous behaviour. When On, the directive routes the parse
    // through ParseJson with "debug":true, enabling mesi.EsiParserConfig.
    // Debug. The core's logger writes to stderr, which Apache captures
    // in its error log (ErrorLog); output includes fetch URLs, so it is
    // a troubleshooting aid, not a production setting.
    int debug; // -1=unset, 0=off, 1=on
    // Only process HTML when the upstream response signals ESI (#172).
    // -1=unset, 0=off, 1=on. Default (unset → off) keeps parsing every
    // text/html response, byte-identical to previous behaviour. When On,
    // only a response whose `Edge-control` header carries `dca=esi`
    // (case-insensitive, comma-separated list — the core's
    // mesi.IsEsiResponse contract) is buffered and parsed; every other
    // HTML response is streamed through untouched, without buffering and
    // without the module's Surrogate-Capability header. The flag is also
    // sent to libgomesi through ParseJson so fetched fragments follow the
    // same rule.
    int parse_on_header; // -1=unset, 0=off, 1=on
    // Marker rendered in place of a failed <esi:include> when the tag
    // has neither onerror="continue" nor a fallback body (#165). NULL
    // (unset) or "" (explicit empty) = silent, the pre-#165 behaviour.
    // Raw operator-supplied HTML: inserted into the response verbatim.
    // Travels to libgomesi through ParseJson as "includeErrorMarker".
    const char *include_error_marker;
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
// include used to silently render an EMPTY body instead of failing (the
// core rejects that value itself since #448), so
// the largest safe value is MaxInt64 - 1. apr_off_t is int64 on every
// platform Apache 2.4 supports. Keep in sync with
// libgomesi/internal/config/max_response_size.go.
#define MESI_MAX_MAX_RESPONSE_SIZE ((apr_off_t)9223372036854775806LL)
// Cap on the MesiMaxConcurrentRequests directive (#170). Matches
// libgomesi's config.MaxMaxConcurrentRequests. Neither the core
// (mesi.EsiParserConfig.MaxConcurrentRequests is a bare int, "0 =
// unlimited") nor Caddy (max_concurrent_requests via strconv.Atoi)
// defines a maximum, so the bound is derived from the transport: the
// field is a C `int` (32-bit on every platform Apache 2.4 supports)
// and the shared strict parser parse_nonneg_int guards at 9 digits —
// 999999999 is the largest value both Apache and the Go-side check
// can represent without a wrap, so the two sides can never disagree
// (same keep-in-sync pattern as MESI_MAX_TIMEOUT_SECONDS /
// MESI_MAX_MAX_RESPONSE_SIZE). The value bounds a `chan struct{}`
// admission semaphore (zero-size elements), so the cap exists for
// portability, not memory. Keep in sync with
// libgomesi/internal/config/max_concurrent_requests.go.
#define MESI_MAX_MAX_CONCURRENT_REQUESTS 999999999
// Cap on the MesiMaxWorkers directive (#171). Matches libgomesi's
// config.MaxMaxWorkers. Neither the core (mesi.EsiParserConfig.
// MaxWorkers is a bare int, "Zero means runtime.NumCPU()*4") nor Caddy
// (max_workers via an uncapped strconv.Atoi — the bare-Atoi gap
// family tracked in #452) defines a maximum, so the bound is derived
// from the transport: the field is a C `int` (32-bit on every platform
// Apache 2.4 supports) and the shared strict parser parse_nonneg_int
// guards at 9 digits — 999999999 is the largest value both Apache and
// the Go-side check can represent without a wrap, so the two sides can
// never disagree (same keep-in-sync pattern as MESI_MAX_TIMEOUT_SECONDS
// / MESI_MAX_MAX_RESPONSE_SIZE / MESI_MAX_MAX_CONCURRENT_REQUESTS).
// The value only bounds a drain pool the core additionally clamps to
// the job count (min(MaxWorkers, jobs), mesi/parser.go), so an
// over-large value is a no-op — the cap exists for portability, not
// semantics. Keep in sync with libgomesi/internal/config/max_workers.go.
#define MESI_MAX_MAX_WORKERS 999999999

// MesiOnOversize values.
#define MESI_ON_OVERSIZE_PASS 0
#define MESI_ON_OVERSIZE_ERROR 1

// mesi_config_init initialises conf to the documented "unset" sentinels and
// allocates the two lists. The caller may pass zeroed memory (apr_pcalloc or
// memset); the function zeroes it anyway so both callers share one contract.
static inline void mesi_config_init(mesi_config *conf, apr_pool_t *p) {
    memset(conf, 0, sizeof(*conf));
    conf->enable_mesi = 0;
    conf->allowed_hosts = apr_array_make(p, 4, sizeof(const char *));
    conf->block_private_ips = -1;             // -1 = unset, default will be applied in filter
    conf->allow_private_ips_for_allowed = -1; // -1 = unset, default off
    conf->shared_http_client = -1;            // -1 = unset, default off
    // NULL = unset/inherit; "" = explicitly disabled. Keeping the two
    // distinct lets a vhost disable an inherited global backend (#617).
    conf->cache_backend = NULL;
    conf->cache_size = 0;
    conf->cache_ttl = -1; // -1 = unset (no expiry)
    conf->cache_redis_addr = NULL;
    conf->cache_redis_password = NULL;
    conf->cache_redis_db = -1; // -1 = unset, default 0 in libgomesi
    // Memcached: empty list means "no server list configured". The
    // mesi_set_cache_memcached_servers directive is the only path that adds
    // entries; an empty list at request time triggers the runtime
    // fail-fast error rather than silently picking some default server.
    conf->cache_memcached_servers = apr_array_make(p, 2, sizeof(const char *));
    conf->cache_key_template = NULL;
    conf->max_depth = -1;               // -1 = unset, default 5 applied in filter
    conf->timeout_seconds = -1;         // -1 = unset: legacy parse path, libgomesi applies its 30s Go-side (macro documents it only)
    conf->max_response_size = -1;       // -1 = unset: legacy parse path, libgomesi leaves 0 = unlimited (pre-#169 behaviour)
    conf->max_body_size = -1;           // -1 = unset: no limit (0 is unlimited too)
    conf->on_oversize = -1;             // -1 = unset: pass
    conf->max_concurrent_requests = -1; // -1 = unset: legacy parse path, libgomesi leaves 0 = unlimited (pre-#170 behaviour)
    conf->max_workers = -1;             // -1 = unset: legacy parse path, libgomesi leaves 0 = library default NumCPU*4 (pre-#171 behaviour)
    conf->debug = -1;                   // -1 = unset: parser stays silent (pre-#173 behaviour)
    conf->parse_on_header = -1;         // -1 = unset: always parse HTML (pre-#172 behaviour)
    conf->include_error_marker = NULL;  // NULL = silent (pre-#165 behaviour)
}

// mesi_merge_configs applies the Apache server-config merge rules: the child
// (add) wins when it explicitly configured a value, otherwise the parent
// (base) value is inherited. `merged` is separate so the tests can assert on
// the result without touching either input.
static inline void mesi_merge_configs(mesi_config *base, mesi_config *add,
                                      mesi_config *merged) {
    merged->enable_mesi = (add->enable_mesi != 0) ? add->enable_mesi : base->enable_mesi;
    merged->allowed_hosts = (add->allowed_hosts->nelts > 0) ? add->allowed_hosts : base->allowed_hosts;
    merged->block_private_ips = (add->block_private_ips != -1) ? add->block_private_ips : base->block_private_ips;
    merged->allow_private_ips_for_allowed = (add->allow_private_ips_for_allowed != -1)
                                                ? add->allow_private_ips_for_allowed
                                                : base->allow_private_ips_for_allowed;
    merged->shared_http_client = (add->shared_http_client != -1)
                                     ? add->shared_http_client
                                     : base->shared_http_client;
    // Cache config: delegate to cache_config.h so the unit tests exercise
    // the real NULL-vs-"" selection instead of a mirror. NULL means
    // "unset, inherit from base"; a non-NULL value — including ""
    // ("explicitly disabled") — wins, so a vhost can turn off a globally
    // configured cache (#617).
    mesi_cache_config base_cache = {
        .backend = base->cache_backend,
        .size = base->cache_size,
        .ttl = base->cache_ttl,
        .redis_addr = base->cache_redis_addr,
        .redis_password = base->cache_redis_password,
        .redis_db = base->cache_redis_db,
        .memcached_servers = base->cache_memcached_servers,
        .key_template = base->cache_key_template,
    };
    mesi_cache_config add_cache = {
        .backend = add->cache_backend,
        .size = add->cache_size,
        .ttl = add->cache_ttl,
        .redis_addr = add->cache_redis_addr,
        .redis_password = add->cache_redis_password,
        .redis_db = add->cache_redis_db,
        .memcached_servers = add->cache_memcached_servers,
        .key_template = add->cache_key_template,
    };
    mesi_cache_config merged_cache;
    mesi_merge_cache_config(&merged_cache, &base_cache, &add_cache);
    merged->cache_backend = merged_cache.backend;
    merged->cache_size = merged_cache.size;
    merged->cache_ttl = merged_cache.ttl;
    merged->cache_redis_addr = merged_cache.redis_addr;
    merged->cache_redis_password = merged_cache.redis_password;
    merged->cache_redis_db = merged_cache.redis_db;
    merged->cache_memcached_servers = merged_cache.memcached_servers;
    merged->cache_key_template = merged_cache.key_template;
    // Max depth: child wins when explicitly set; -1 sentinel inherits.
    // Explicit 0 (passthrough) is a configured value and must win.
    merged->max_depth = (add->max_depth != -1) ? add->max_depth : base->max_depth;
    // Timeout: child wins when explicitly set; -1 sentinel inherits
    // (0 can never be stored — set_timeout rejects it — so the -1
    // sentinel is unambiguous).
    merged->timeout_seconds = (add->timeout_seconds != -1) ? add->timeout_seconds : base->timeout_seconds;
    // Max response size: child wins when explicitly set; -1 sentinel
    // inherits. Unlike timeout, 0 IS storable (explicit "unlimited"),
    // and -1 can never be stored (set_max_response_size rejects it),
    // so the sentinel stays unambiguous and a vhost's explicit 0
    // overrides a global limit.
    merged->max_response_size = (add->max_response_size != -1) ? add->max_response_size : base->max_response_size;
    merged->max_body_size = (add->max_body_size != -1) ? add->max_body_size : base->max_body_size;
    merged->on_oversize = (add->on_oversize != -1) ? add->on_oversize : base->on_oversize;
    // Max concurrent requests: child wins when explicitly set; -1
    // sentinel inherits. Unlike timeout, 0 IS storable (explicit
    // "unlimited"), and -1 can never be stored
    // (set_max_concurrent_requests rejects it), so the sentinel stays
    // unambiguous and a vhost's explicit 0 overrides a global cap.
    merged->max_concurrent_requests = (add->max_concurrent_requests != -1)
                                          ? add->max_concurrent_requests
                                          : base->max_concurrent_requests;
    // Max workers: child wins when explicitly set; -1 sentinel
    // inherits. Unlike timeout, 0 IS storable (explicit "library
    // default"), and -1 can never be stored (set_max_workers rejects
    // it), so the sentinel stays unambiguous and a vhost's explicit 0
    // overrides an inherited cap.
    merged->max_workers = (add->max_workers != -1)
                              ? add->max_workers
                              : base->max_workers;
    // Debug: child wins when explicitly set (On or Off); -1 inherits.
    merged->debug = (add->debug != -1) ? add->debug : base->debug;
    // Parse-on-header: child wins when explicitly set (On or Off); -1 inherits.
    merged->parse_on_header = (add->parse_on_header != -1)
                                  ? add->parse_on_header
                                  : base->parse_on_header;
    // Include error marker: child wins when explicitly set (including an
    // explicit empty string, whose non-NULL pointer clears an inherited
    // marker); NULL sentinel inherits.
    merged->include_error_marker = add->include_error_marker
                                       ? add->include_error_marker
                                       : base->include_error_marker;
}

// mesi_unicode_space returns the width in bytes of the UTF-8 rune starting
// at p when it is a Unicode whitespace rune that libgomesi's strings.Fields
// treats as a separator (Go's unicode.IsSpace), 0 otherwise. Covers every
// non-ASCII whitespace rune: U+0085, U+00A0, U+1680, U+2000..U+200A, U+2028,
// U+2029, U+202F, U+205F and U+3000. Any other byte — including truncated or
// invalid UTF-8, which Go decodes as U+FFFD (not a space) — forms a hostname
// token, so it is not whitespace here. Mirrors ngx_http_mesi_unicode_space
// in the nginx module (#354).
static inline apr_size_t mesi_unicode_space(const unsigned char *p, apr_size_t len) {
    if (len >= 2 && p[0] == 0xc2 && (p[1] == 0x85 || p[1] == 0xa0)) {
        return 2; // U+0085 NEL, U+00A0 no-break space
    }
    if (len >= 3 && p[0] == 0xe1 && p[1] == 0x9a && p[2] == 0x80) {
        return 3; // U+1680 ogham space mark
    }
    if (len >= 3 && p[0] == 0xe2 && p[1] == 0x80 &&
        ((p[2] >= 0x80 && p[2] <= 0x8a) // U+2000..U+200A
         || p[2] == 0xa8                // U+2028 line separator
         || p[2] == 0xa9                // U+2029 paragraph separator
         || p[2] == 0xaf))              // U+202F narrow no-break space
    {
        return 3;
    }
    if (len >= 3 && p[0] == 0xe2 && p[1] == 0x81 && p[2] == 0x9f) {
        return 3; // U+205F medium mathematical space
    }
    if (len >= 3 && p[0] == 0xe3 && p[1] == 0x80 && p[2] == 0x80) {
        return 3; // U+3000 ideographic space
    }
    return 0;
}

// mesi_ascii_space reports whether c is ASCII whitespace under Go's
// strings.Fields tokenization: space, tab, CR, LF, VT and FF. Every
// caller that tokenizes a directive value uses this helper (and
// mesi_unicode_space) so no third whitespace table can drift from
// libgomesi (#358, #559).
static inline int mesi_ascii_space(unsigned char c) {
    return c == ' ' || c == '\t' || c == '\r' || c == '\n' ||
           c == '\v' || c == '\f';
}

// mesi_has_hostname_token reports whether s contains at least one token
// under Go's strings.Fields tokenization, which libgomesi applies to the
// joined allowlist: ASCII whitespace (space, tab, CR, LF, VT, FF) and every
// Unicode whitespace rune are separators, any other byte (including invalid
// UTF-8) is part of a token.
static inline int mesi_has_hostname_token(const char *s) {
    const unsigned char *p = (const unsigned char *)s;
    apr_size_t len = strlen(s);
    apr_size_t i = 0;
    while (i < len) {
        unsigned char c = p[i];
        apr_size_t ws_width;
        if (mesi_ascii_space(c)) {
            i++;
            continue;
        }
        ws_width = mesi_unicode_space(p + i, len - i);
        if (ws_width == 0) {
            return 1; // a non-whitespace rune: the value has a hostname token
        }
        i += ws_width;
    }
    return 0;
}

// mesi_next_field advances *pp past separators and returns the start of
// the next token, storing its byte length in *tok_len (NULL at end of
// input). The tokenization is exactly mesi_has_hostname_token's, i.e.
// Go's strings.Fields (the rule libgomesi applies to allowedHosts):
// the full ASCII whitespace set and every Unicode whitespace rune
// separate tokens, while any other byte —
// including invalid or truncated UTF-8, which Go decodes as U+FFFD, not
// a space — is part of a token.
static inline const char *mesi_next_field(const char **pp, apr_size_t *tok_len) {
    const unsigned char *p = (const unsigned char *)*pp;
    apr_size_t remaining = strlen(*pp);
    const unsigned char *start;
    while (remaining > 0) {
        apr_size_t ws_width;
        if (mesi_ascii_space(*p)) {
            p++;
            remaining--;
            continue;
        }
        ws_width = mesi_unicode_space(p, remaining);
        if (ws_width == 0) {
            break;
        }
        p += ws_width;
        remaining -= ws_width;
    }
    if (remaining == 0) {
        *pp = (const char *)p;
        return NULL;
    }
    start = p;
    while (remaining > 0) {
        apr_size_t ws_width;
        if (mesi_ascii_space(*p)) {
            break;
        }
        ws_width = mesi_unicode_space(p, remaining);
        if (ws_width != 0) {
            break;
        }
        p++;
        remaining--;
    }
    *tok_len = (apr_size_t)(p - start);
    *pp = (const char *)p;
    return (const char *)start;
}

// Parse a non-negative decimal integer from arg. Reject empty input,
// non-digit characters (including '-', '+', '.') — fail-fast instead of
// silently coercing via strtol, and values outside [min, max].
// `directive` is the Apache directive name used in every error string
// (e.g. "MesiMaxDepth", "MesiCacheSize") so callers do not remap.
// Returns NULL on success (parsed value stored in *out) or an
// Apache-pool-allocated error string suitable as set_* return value.
static inline const char *parse_nonneg_int(apr_pool_t *pool, const char *arg,
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
static inline const char *parse_nonneg_off(apr_pool_t *pool, const char *arg,
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
static inline const char *parse_nonneg_int_bounded(apr_pool_t *pool,
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

// mesi_header_signals_esi reports whether an Edge-control header value
// signals ESI processing, mirroring mesi.IsEsiResponse exactly: the value
// is a comma-separated list, matching is case-insensitive, and whitespace
// around each element is ignored. NULL, empty and non-matching values
// (for example "max-age=60") do not signal ESI.
static inline int mesi_header_signals_esi(const char *value) {
    if (!value) {
        return 0;
    }
    const char *p = value;
    for (;;) {
        while (mesi_ascii_space((unsigned char)*p)) p++;
        const char *start = p;
        while (*p && *p != ',') p++;
        const char *end = p;
        while (end > start && mesi_ascii_space((unsigned char)end[-1])) end--;
        if ((size_t)(end - start) == sizeof("dca=esi") - 1 &&
            strncasecmp(start, "dca=esi", sizeof("dca=esi") - 1) == 0) {
            return 1;
        }
        if (*p == '\0') {
            return 0;
        }
        p++; // Skip the comma and try the next element.
    }
}

static inline const char *mesi_set_enable_mesi(mesi_config *conf, int flag) {
    conf->enable_mesi = flag;
    return NULL;
}

static inline const char *mesi_set_block_private_ips(mesi_config *conf, int flag) {
    conf->block_private_ips = flag;
    return NULL;
}

static inline const char *mesi_set_allow_private_for_allowed(mesi_config *conf, int flag) {
    conf->allow_private_ips_for_allowed = flag;
    return NULL;
}

static inline const char *mesi_set_shared_http_client(mesi_config *conf, int flag) {
    conf->shared_http_client = flag;
    return NULL;
}

static inline const char *mesi_set_debug(mesi_config *conf, int flag) {
    conf->debug = flag;
    return NULL;
}

static inline const char *mesi_set_parse_on_header(mesi_config *conf, int flag) {
    conf->parse_on_header = flag;
    return NULL;
}

// MesiAllowedHosts — AP_INIT_RAW_ARGS, RSRC_CONF. Reject values without any
// hostname (#358) because an empty or whitespace-only value (ASCII or
// Unicode, e.g. U+00A0) would tokenize to an empty allowlist in libgomesi
// and silently disable the restriction the operator intended (fail-open).
// Reject quote characters (#532): Apache does not unquote RAW_ARGS, so
// `"backend other"` would store the hosts `"backend` and `other"`, which
// never match and silently block every include. Hosts are written unquoted,
// separated by spaces.
static inline const char *mesi_set_allowed_hosts(mesi_config *conf, apr_pool_t *pool,
                                                 const char *arg) {
    const char *host;
    if (!arg || !mesi_has_hostname_token(arg)) {
        return "MesiAllowedHosts must contain at least one hostname";
    }
    if (strchr(arg, '"') || strchr(arg, '\'')) {
        return "MesiAllowedHosts: quote characters are not allowed in host names; "
               "write the hosts unquoted, separated by spaces";
    }
    while (*arg) {
        // Skip whitespace (space, tab).
        while (*arg && (*arg == ' ' || *arg == '\t')) arg++;
        host = arg;
        // Find end of token (space or tab).
        while (*arg && *arg != ' ' && *arg != '\t') arg++;
        if (host != arg) {
            const char **new_host = apr_array_push(conf->allowed_hosts);
            *new_host = apr_pstrndup(pool, host, arg - host);
        }
    }
    return NULL;
}

// MesiMaxDepth — ESI nesting depth. Uses parse_nonneg_int so "abc",
// "3foo", empty, decimals, and overflow are rejected (atoi would
// silently coerce those). Helper errors already name MesiMaxDepth.
static inline const char *mesi_set_max_depth(mesi_config *conf, apr_pool_t *pool,
                                             const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiMaxDepth",
                                       0, MESI_MAX_MAX_DEPTH, &v);
    if (err) {
        return err;
    }
    conf->max_depth = v;
    return NULL;
}

// The tokenizer Apache uses for AP_INIT_RAW_ARGS values. In the module it is
// ap_getword_conf(); the unit harness passes an equivalent stand-in so the
// shared validation below still runs against the real code.
typedef const char *(*mesi_conf_word_fn)(apr_pool_t *pool, const char **line);

// MesiIncludeErrorMarker — raw HTML rendered in place of a failed
// <esi:include> that has neither onerror="continue" nor a fallback body
// (#165). AP_INIT_RAW_ARGS, server context. Unset (NULL) and explicit ""
// both mean silent (the pre-#165 behaviour); an explicit empty value is
// still stored as a non-NULL pointer so a vhost can clear a marker
// inherited from the global config.
//
// Exactly one token is accepted; a marker containing spaces must be
// quoted, and trailing tokens (`"" extra`) fail config load instead of
// being silently folded into the marker. The value is inserted into the
// response verbatim and is JSON-escaped by mesi_json_string() when it
// travels through ParseJson, so quotes/backslashes/control characters
// cannot break the config blob.
static inline const char *mesi_set_include_error_marker(mesi_config *conf,
                                                        apr_pool_t *pool,
                                                        const char *arg,
                                                        mesi_conf_word_fn get_word) {
    const char *p = arg;
    // Apache trims config lines, but skip any leading whitespace anyway
    // so a whitespace-only value is reported as missing, not as an empty
    // marker (the same ASCII whitespace set ap_getword_conf() uses).
    while (p && *p && (*p == ' ' || *p == '\t' || *p == '\n' || *p == '\r' || *p == '\v' || *p == '\f')) {
        p++;
    }
    if (!p || *p == '\0') {
        return "MesiIncludeErrorMarker requires exactly one argument "
               "(quote a marker containing spaces; use \"\" for explicit silent)";
    }
    const char *rest = p;
    const char *marker = get_word(pool, &rest);
    // The tokenizer already skipped whitespace after the token: a
    // non-empty rest is a trailing argument. `"" ""` is rejected too,
    // because the second (empty) token is still present in the raw line.
    if (*rest != '\0') {
        return "MesiIncludeErrorMarker takes exactly one argument "
               "(quote a marker containing spaces; use \"\" for explicit silent)";
    }
    // `marker` lives in the caller's pool; store it directly (same lifetime
    // as the server config). It is non-NULL even for `""`.
    conf->include_error_marker = marker;
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
static inline const char *mesi_set_timeout(mesi_config *conf, apr_pool_t *pool,
                                           const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiTimeout",
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
// trailing garbage ("100abc") and a leading '+'
// — malformed explicit values would pass config load — and NOT
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
// the include used to silently render an empty body; the core
// rejects that value itself since #448. Helper errors
// already name MesiMaxResponseSize.
static inline const char *mesi_set_max_response_size(mesi_config *conf, apr_pool_t *pool,
                                                     const char *arg) {
    apr_off_t v = 0;
    const char *err = parse_nonneg_off(pool, arg, "MesiMaxResponseSize",
                                       0, MESI_MAX_MAX_RESPONSE_SIZE, &v);
    if (err) {
        return err;
    }
    conf->max_response_size = v;
    return NULL;
}

// MesiMaxBodySize — cap on the parent response body buffered for ESI
// processing, in bytes. 0 = unlimited. Same strict parser as
// MesiMaxResponseSize.
static inline const char *mesi_set_max_body_size(mesi_config *conf, apr_pool_t *pool,
                                                 const char *arg) {
    apr_off_t v = 0;
    const char *err = parse_nonneg_off(pool, arg, "MesiMaxBodySize",
                                       0, MESI_MAX_MAX_RESPONSE_SIZE, &v);
    if (err) {
        return err;
    }
    conf->max_body_size = v;
    return NULL;
}

// MesiOnOversize — what to do when the body exceeds MesiMaxBodySize:
// "pass" (default) sends it unprocessed, "error" answers 502.
static inline const char *mesi_set_on_oversize(mesi_config *conf, apr_pool_t *pool,
                                               const char *arg) {
    if (strcasecmp(arg, "pass") == 0) {
        conf->on_oversize = MESI_ON_OVERSIZE_PASS;
    } else if (strcasecmp(arg, "error") == 0) {
        conf->on_oversize = MESI_ON_OVERSIZE_ERROR;
    } else {
        return apr_psprintf(pool, "MesiOnOversize must be 'pass' or 'error' (got: %s)", arg);
    }
    return NULL;
}

// MesiMaxConcurrentRequests — cap on concurrent <esi:include> HTTP
// fetches within one page render (one MESIParse call) (#170). Parsed
// with parse_nonneg_int — NOT the issue's atoi sketch: atoi silently
// coerces "abc"/"" to 0 (= unlimited) and accepts trailing garbage
// ("3foo" → 3), so a malformed explicit value would pass config load
// as a documented one — with range [0,
// MESI_MAX_MAX_CONCURRENT_REQUESTS]. 0 is a LEGITIMATE configured
// value: the core only installs the admission-control semaphore when
// MaxConcurrentRequests > 0 (mesi/parser.go), so 0 means "unlimited"
// — the documented contract shared with Caddy
// `max_concurrent_requests 0`. Negatives are rejected (the core would
// only warn "max_concurrent_requests_invalid" and normalize them to
// 0 = unlimited, mesi/parser.go / #329 — a malformed explicit value
// must never pass as the documented one). Unset (-1 sentinel) keeps
// the legacy parse path, where libgomesi leaves the field at 0 —
// byte-identical to pre-#170 Apache behaviour. Helper errors already
// name MesiMaxConcurrentRequests.
static inline const char *mesi_set_max_concurrent_requests(mesi_config *conf,
                                                           apr_pool_t *pool,
                                                           const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiMaxConcurrentRequests",
                                       0, MESI_MAX_MAX_CONCURRENT_REQUESTS, &v);
    if (err) {
        return err;
    }
    conf->max_concurrent_requests = v;
    return NULL;
}

// MesiMaxWorkers — cap on the include worker pool that drains ESI jobs
// within one page render (one MESIParse call; each nesting level
// spawns its own pool and inherits the cap) (#171). Parsed with
// parse_nonneg_int — NOT the issue's atoi sketch: atoi silently
// coerces "abc"/"" to 0 (= the library default) and accepts trailing
// garbage ("4foo" → 4), so a malformed explicit value would pass
// config load as a documented one — with range [0,
// MESI_MAX_MAX_WORKERS]. 0 is a LEGITIMATE configured value: the core
// substitutes runtime.NumCPU()*4 for any value <= 0 (mesi/parser.go),
// so 0 means "library default" — the documented contract shared with
// Caddy `max_workers 0`. Negatives are rejected (the core would
// silently substitute that default with NO warning — there is no
// #329-style warn+normalize for MaxWorkers — so a malformed explicit
// value must never pass as the documented "library default"). Unset
// (-1 sentinel) keeps the legacy parse path, where libgomesi leaves
// the field at 0 — byte-identical to pre-#171 Apache behaviour.
// Helper errors already name MesiMaxWorkers. Distinction from
// MesiMaxConcurrentRequests: this bounds the CPU-side drain pool (goroutines
// processing tokens/includes, also bounding fetches at that level),
// that one bounds concurrent HTTP fetch slots (an admission semaphore).
static inline const char *mesi_set_max_workers(mesi_config *conf, apr_pool_t *pool,
                                               const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiMaxWorkers",
                                       0, MESI_MAX_MAX_WORKERS, &v);
    if (err) {
        return err;
    }
    conf->max_workers = v;
    return NULL;
}

// MesiCache* directives that document an explicit empty value are
// AP_INIT_RAW_ARGS: TAKE1 rejects `""` before the setter runs (#617, the
// #165/PR #607 class). Parse exactly one token so a missing argument, an
// unquoted value containing spaces, or a trailing token still fails config
// load. `""` yields a non-NULL empty string.
static inline const char *mesi_parse_one_conf_word(apr_pool_t *pool,
                                                   const char *raw,
                                                   const char *directive,
                                                   const char **out,
                                                   mesi_conf_word_fn get_word) {
    const char *p = raw;
    while (p && *p && mesi_ascii_space((unsigned char)*p)) {
        p++;
    }
    if (!p || *p == '\0') {
        return apr_psprintf(pool,
                            "%s requires exactly one argument "
                            "(quote values containing spaces; use \"\" for an explicit empty value)",
                            directive);
    }
    const char *rest = p;
    const char *word = get_word(pool, &rest);
    if (*rest != '\0') {
        return apr_psprintf(pool,
                            "%s takes exactly one argument "
                            "(quote values containing spaces; use \"\" for an explicit empty value)",
                            directive);
    }
    *out = word;
    return NULL;
}

static inline const char *mesi_set_cache_backend(mesi_config *conf, apr_pool_t *pool,
                                                 const char *arg,
                                                 mesi_conf_word_fn get_word) {
    const char *value = NULL;
    const char *err = mesi_parse_one_conf_word(pool, arg, "MesiCacheBackend", &value,
                                               get_word);
    if (err) {
        return err;
    }
    // Reject anything outside the supported set so a typo doesn't silently
    // fall back to "no cache" (which would change behavior without
    // operator awareness). Backends: "memory", "redis", "memcached".
    if (strcmp(value, "memory") == 0) {
        conf->cache_backend = "memory";
        return NULL;
    }
    if (strcmp(value, "redis") == 0) {
        conf->cache_backend = "redis";
        return NULL;
    }
    if (strcmp(value, "memcached") == 0) {
        conf->cache_backend = "memcached";
        return NULL;
    }
    if (value[0] == '\0') {
        // Non-NULL empty: explicitly disable. The merge treats it as a
        // configured value, so a vhost can clear an inherited backend.
        conf->cache_backend = "";
        return NULL;
    }
    return apr_psprintf(pool,
                        "MesiCacheBackend: unknown backend %s "
                        "(supported: \"memory\", \"redis\", \"memcached\", or empty)",
                        value);
}

static inline const char *mesi_set_cache_size(mesi_config *conf, apr_pool_t *pool,
                                              const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiCacheSize",
                                       1, MESI_MAX_CACHE_SIZE, &v);
    if (err) {
        return err;
    }
    conf->cache_size = v;
    return NULL;
}

static inline const char *mesi_set_cache_ttl(mesi_config *conf, apr_pool_t *pool,
                                             const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, "MesiCacheTTL",
                                       0, MESI_MAX_CACHE_TTL_SECONDS, &v);
    if (err) {
        return err;
    }
    conf->cache_ttl = v;
    return NULL;
}

// MesiCacheRedisAddr — host:port pair. Empty value explicitly clears any
// previously-set addr (a non-NULL "" lets a vhost reset an inherited
// address; libgomesi then falls back to localhost:6379, #617).
// Must contain ':' followed by a valid port (1..65535). Reject characters
// that would be unsafe to embed in JSON.
static inline const char *mesi_set_cache_redis_addr(mesi_config *conf, apr_pool_t *pool,
                                                    const char *arg,
                                                    mesi_conf_word_fn get_word) {
    const char *value = NULL;
    const char *err = mesi_parse_one_conf_word(pool, arg, "MesiCacheRedisAddr", &value,
                                               get_word);
    if (err) {
        return err;
    }
    // Empty arg → explicit clear. Store the non-NULL empty string, not
    // NULL: the merge uses NULL for "unset/inherit", so NULL would silently
    // keep a parent's address (#617).
    if (value[0] == '\0') {
        conf->cache_redis_addr = "";
        return NULL;
    }
    // Disallow embedded whitespace, control chars, or JSON meta chars
    // in the address — the value gets serialized into JSON.
    for (const char *p = value; *p; p++) {
        unsigned char c = (unsigned char)*p;
        if (c == ' ' || c == '\t' || c == '"' || c == '\\' || c < 0x20) {
            return apr_psprintf(pool,
                                "MesiCacheRedisAddr: invalid character %d in %s",
                                (int)c, value);
        }
    }
    // Find last ':' (IPv6 addresses use [...] or no port — we keep it
    // simple: must contain a colon, port part must be digits in 1..65535).
    const char *colon = strrchr(value, ':');
    if (!colon || colon == value || *(colon + 1) == '\0') {
        return apr_psprintf(pool,
                            "MesiCacheRedisAddr: must be host:port (got: %s)", value);
    }
    // Validate port is a positive decimal in [1, 65535]. We've already
    // rejected whitespace/JSON-meta chars, so colon+1 is digits-only
    // up to the NUL terminator.
    int port = 0;
    apr_size_t port_len = strlen(colon + 1);
    const char *perr = parse_nonneg_int_bounded(pool,
                                                colon + 1,
                                                colon + 1 + port_len,
                                                "MesiCacheRedisAddr",
                                                1, 65535, &port);
    if (perr) {
        return apr_psprintf(pool,
                            "MesiCacheRedisAddr: port invalid: %s", value);
    }
    conf->cache_redis_addr = apr_pstrdup(pool, value);
    return NULL;
}

// MesiCacheRedisPassword — raw Redis AUTH password. We do NOT log
// the password value on error (don't leak creds into error.log).
// Empty arg explicitly clears the password (non-NULL empty, so a
// vhost can override an inherited password); unset inherits.
static inline const char *mesi_set_cache_redis_password(mesi_config *conf, apr_pool_t *pool,
                                                        const char *arg,
                                                        mesi_conf_word_fn get_word) {
    const char *value = NULL;
    const char *err = mesi_parse_one_conf_word(pool, arg, "MesiCacheRedisPassword",
                                               &value, get_word);
    if (err) {
        return err;
    }
    // Reject embedded control chars (< 0x20). Quotes/backslashes are
    // explicitly escaped by build_redis_config_json, so they're OK.
    for (const char *p = value; *p; p++) {
        unsigned char c = (unsigned char)*p;
        if (c < 0x20) {
            return apr_psprintf(pool,
                                "MesiCacheRedisPassword: invalid control character 0x%02x in value",
                                (unsigned)c);
        }
    }
    conf->cache_redis_password = apr_pstrdup(pool, value);
    return NULL;
}

// MesiCacheRedisDB — Redis logical database number. 0..15 (Redis
// default config; redis.conf "databases 16"). Negatives are rejected.
static inline const char *mesi_set_cache_redis_db(mesi_config *conf, apr_pool_t *pool,
                                                  const char *arg) {
    int v = -1;
    const char *err = parse_nonneg_int(pool, arg, "MesiCacheRedisDB",
                                       0, MESI_MAX_REDIS_DB, &v);
    if (err) {
        return err;
    }
    conf->cache_redis_db = v;
    return NULL;
}

// mesi_set_cache_memcached_servers accepts a space-separated list of
// "host:port" entries used when MesiCacheBackend is memcached (#176).
// Each token must contain a colon followed by a port in [1, 65535];
// hostnames/ports with embedded whitespace, control chars, quote
// characters or JSON meta characters are rejected so the rendered
// JSON config is safe to pass to libgomesi. No silent fallback to
// localhost:11211 — if the directive is omitted, the empty server list
// is logged as a missing-config error at runtime and ESI runs without
// cache. AP_INIT_RAW_ARGS gives us the full line, so parsing is
// line-based and tokenizes with Go's strings.Fields rule
// (mesi_next_field): ASCII whitespace and every Unicode whitespace
// rune separate entries, while invalid UTF-8 stays part of a token
// (#559). A Unicode whitespace byte must never be stored inside an
// entry: the entry is rendered verbatim as one element of the
// `servers` JSON array, so its host can never resolve and the cache
// fails silently at request time, with no config-load diagnostic.
static inline const char *mesi_set_cache_memcached_servers(mesi_config *conf,
                                                           apr_pool_t *pool,
                                                           const char *arg) {
    if (!arg) {
        return "MesiCacheMemcachedServers requires space-separated host:port entries";
    }
    // Each child-context config starts from a fresh array (see
    // mesi_config_init). We append to the array that this server
    // config owns, mirroring mesi_set_allowed_hosts behaviour.
    const char *tok;
    apr_size_t tok_len;
    int count = 0;
    while ((tok = mesi_next_field(&arg, &tok_len)) != NULL) {
        const char *end = tok + tok_len;
        // Reject control chars (including DEL, which `c < 0x20` missed
        // before #559) and JSON-meta chars. Quote characters get their
        // own message (#542): the directive is AP_INIT_RAW_ARGS, so
        // Apache does not unquote the line and 'mc':11211 stores the
        // host 'mc', which can never resolve. Say so instead of the
        // generic "invalid character", the same way MesiAllowedHosts
        // does after #532.
        int has_quote = 0;
        int has_invalid = 0;
        for (const char *p = tok; p < end; p++) {
            unsigned char c = (unsigned char)*p;
            if (c == '"' || c == '\'') {
                has_quote = 1;
                break;
            }
            if (c == '\\' || c < 0x20 || c == 0x7f) {
                has_invalid = 1;
                break;
            }
        }
        if (has_quote) {
            return apr_psprintf(pool,
                                "MesiCacheMemcachedServers: quote characters are not allowed in entries; "
                                "write the host:port entries unquoted, separated by spaces (got: %.*s)",
                                (int)tok_len, tok);
        }
        if (has_invalid) {
            return apr_psprintf(pool,
                                "MesiCacheMemcachedServers: invalid character in entry %.*s",
                                (int)tok_len, tok);
        }
        // Find last ':' (matches redis-addr parser). IPv4/IPv6/hostname
        // forms all end with :port.
        const char *colon = NULL;
        for (const char *p = end - 1; p >= tok; p--) {
            if (*p == ':') {
                colon = p;
                break;
            }
        }
        if (!colon || colon == tok || colon + 1 == end) {
            return apr_psprintf(pool,
                                "MesiCacheMemcachedServers: entry must be host:port (got: %.*s)",
                                (int)tok_len, tok);
        }
        // Validate port over [colon+1, end) so digits inside the host
        // (which can include '1', '0', ...) aren't accidentally
        // consumed. parse_nonneg_int_bounded stops exactly at `end`.
        int port = 0;
        const char *err = parse_nonneg_int_bounded(pool, colon + 1, end,
                                                   "MesiCacheMemcachedServers",
                                                   1, 65535, &port);
        if (err) {
            return apr_psprintf(pool,
                                "MesiCacheMemcachedServers: port invalid in %.*s",
                                (int)tok_len, tok);
        }
        if (count >= MESI_MAX_MEMCACHED_SERVERS) {
            return apr_psprintf(pool,
                                "MesiCacheMemcachedServers: too many entries (max %d)",
                                MESI_MAX_MEMCACHED_SERVERS);
        }
        const char **slot = apr_array_push(conf->cache_memcached_servers);
        // Copy into the config pool so it survives past the current
        // request (raw arg pointer is request-scoped).
        *slot = apr_pstrndup(pool, tok, tok_len);
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
static inline const char *mesi_set_cache_key_template(mesi_config *conf, apr_pool_t *pool,
                                                      const char *arg) {
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
        return apr_psprintf(pool,
                            "MesiCacheKeyTemplate exceeds maximum length %d (got %zu)",
                            MESI_MAX_CACHE_KEY_TEMPLATE, len);
    }
    // Reject NUL already handled by C string; reject control chars
    // other than spaces that are part of the template syntax? Templates
    // contain ':' and braces; spaces inside a template would be
    // intentional (e.g. "mesi:${url} ${header:X}"), so spaces are
    // allowed. Only reject controls < 0x20 (a template may reasonably
    // contain a space for separation), and DEL. This matches the
    // php-ext validator which calls mesi_is_safe_string (rejects
    // controls and DEL). Allow '"' and '\' — they are harmless in the
    // plain C string passed to CGo.
    for (const char *c = arg; *c; c++) {
        unsigned char uc = (unsigned char)*c;
        if (uc < 0x20) {
            return apr_psprintf(pool,
                                "MesiCacheKeyTemplate contains control character 0x%02x", uc);
        }
        if (uc == 0x7f) {
            return apr_psprintf(pool,
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
        norm = apr_palloc(pool, strlen(arg) - cnt + 1);
        char *w = norm;
        const char *r = arg;
        while (*r) {
            if (r[0] == '$' && r[1] == '$' && r[2] == '{') {
                *w++ = '$';
                *w++ = '{';
                r += 3;
            } else {
                *w++ = *r++;
            }
        }
        *w = '\0';
        arg = norm;
        len = strlen(arg);
    }
    if (strstr(arg, "::") != NULL) {
        return apr_psprintf(pool,
                            "MesiCacheKeyTemplate: Apache config interpolation replaced ${url} (AH00111); escape the dollar sign as $${url} in httpd.conf (got: %s)", arg);
    }
    if (arg[len - 1] == ':') {
        return apr_psprintf(pool,
                            "MesiCacheKeyTemplate: Apache config interpolation replaced ${url} (AH00111); escape the dollar sign as $${url} in httpd.conf (got: %s)", arg);
    }
    conf->cache_key_template = apr_pstrdup(pool, arg);
    return NULL;
}

#endif /* MOD_MESI_CONFIG_H */
