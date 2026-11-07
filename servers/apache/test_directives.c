/*
 * Unit tests for the Apache module directive parsing, config merge and
 * response-filter lifecycle.
 *
 * Compile: gcc -o test_directives test_directives.c -I/usr/include/apr-1.0 -lapr-1 -laprutil-1
 * Run: ./test_directives
 *
 * The helpers under test are the real production definitions in
 * mesi_config.h / cache_config.h / filter_lifecycle.h / mesi_pure.h /
 * parse_json_config.h, shared with mod_mesi.c (#558), not copies.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <assert.h>
#include <apr_general.h>
#include <apr_pools.h>
#include <apr_tables.h>
#include <apr_strings.h>

#include "mesi_config.h"
#include "parse_json_config.h"
#include "cache_config.h"
#include "filter_lifecycle.h"
#include "mesi_pure.h"

static apr_pool_t *pool;
static int tests_passed = 0;
static int tests_failed = 0;

#define TEST(name) static void test_##name()
#define RUN_TEST(name)                     \
    do {                                   \
        printf("  Testing %s... ", #name); \
        test_##name();                     \
        printf("PASS\n");                  \
        tests_passed++;                    \
    } while (0)

#define ASSERT_EQ(a, b) assert((a) == (b))
#define ASSERT_STR_EQ(a, b) assert(strcmp((a), (b)) == 0)
#define ASSERT_STR_CONTAINS(haystack, needle) assert(strstr((haystack), (needle)) != NULL)
#define ASSERT_NOT_NULL(x) assert((x) != NULL)
#define ASSERT_NULL(x) assert((x) == NULL)

/* MesiIncludeErrorMarker and the MesiCache* RAW_ARGS directives call
 * Apache's ap_getword_conf(), which needs a live server. This stand-in
 * implements the single-token subset those directives use; tokenization is
 * the only Apache-specific piece that cannot be shared, so their validation
 * still comes from the shared mesi_set_* implementations. */
static const char *parse_conf_word(apr_pool_t *p, const char **line) {
    const char *str = *line, *strend;
    char *res;
    char quote;

    while (mesi_ascii_space((unsigned char)*str)) {
        ++str;
    }
    if (!*str) {
        *line = str;
        return "";
    }

    if ((quote = *str) == '"' || quote == '\'') {
        strend = str + 1;
        while (*strend && *strend != quote) {
            if (*strend == '\\' && strend[1] &&
                (strend[1] == quote || strend[1] == '\\')) {
                strend += 2;
            } else {
                ++strend;
            }
        }
        res = apr_palloc(p, (apr_size_t)(strend - str));
        char *w = res;
        for (const char *s = str + 1; s < strend; ++s) {
            if (*s == '\\' && s + 1 < strend &&
                (s[1] == quote || s[1] == '\\')) {
                *w++ = *++s;
            } else {
                *w++ = *s;
            }
        }
        *w = '\0';
        if (*strend == quote) {
            ++strend;
        }
    } else {
        strend = str;
        while (*strend && !mesi_ascii_space((unsigned char)*strend)) {
            ++strend;
        }
        res = apr_pstrndup(p, str, (apr_size_t)(strend - str));
    }

    while (mesi_ascii_space((unsigned char)*strend)) {
        ++strend;
    }
    *line = strend;
    return res;
}

/* Test cases */

TEST(single_hostname) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allowed_hosts(&conf, pool, "trusted.example.com");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 1);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "trusted.example.com");
}

TEST(multiple_hostnames_space) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allowed_hosts(&conf, pool, "host1.com host2.com host3.com");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 3);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "host1.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[1], "host2.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[2], "host3.com");
}

TEST(multiple_hostnames_tab) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allowed_hosts(&conf, pool, "host1.com\thost2.com");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 2);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "host1.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[1], "host2.com");
}

TEST(mixed_whitespace) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allowed_hosts(&conf, pool, "host1.com  host2.com\t host3.com");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 3);
}

TEST(empty_string) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allowed_hosts(&conf, pool, "");

    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "at least one hostname");
    ASSERT_EQ(conf.allowed_hosts->nelts, 0);
}

TEST(whitespace_only) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allowed_hosts(&conf, pool, "   \t  ");

    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "at least one hostname");
    ASSERT_EQ(conf.allowed_hosts->nelts, 0);
}

/* #358: the full ASCII whitespace set of strings.Fields is rejected,
 * not only space and tab. */
TEST(whitespace_only_ascii_control) {
    const char *values[] = {"\r", "\n", "\v", "\f", " \r\n\v\f\t "};
    for (size_t i = 0; i < sizeof(values) / sizeof(values[0]); i++) {
        mesi_config conf;
        mesi_config_init(&conf, pool);

        const char *err = mesi_set_allowed_hosts(&conf, pool, values[i]);

        ASSERT_NOT_NULL(err);
        ASSERT_EQ(conf.allowed_hosts->nelts, 0);
    }
}

/* #358: Unicode-whitespace-only values tokenize to zero hosts in
 * libgomesi (strings.Fields), so they are rejected too. Covers every
 * non-ASCII rune of Go's unicode.IsSpace. */
TEST(whitespace_only_unicode) {
    const char *values[] = {
        "\xc2\x85",                 /* U+0085 */
        "\xc2\xa0",                 /* U+00A0 */
        "\xe1\x9a\x80",             /* U+1680 */
        "\xe2\x80\x80",             /* U+2000 */
        "\xe2\x80\x8a",             /* U+200A */
        "\xe2\x80\xa8",             /* U+2028 */
        "\xe2\x80\xa9",             /* U+2029 */
        "\xe2\x80\xaf",             /* U+202F */
        "\xe2\x81\x9f",             /* U+205F */
        "\xe3\x80\x80",             /* U+3000 */
        " \xc2\xa0\t\xe3\x80\x80 ", /* mixed ASCII and Unicode */
    };
    for (size_t i = 0; i < sizeof(values) / sizeof(values[0]); i++) {
        mesi_config conf;
        mesi_config_init(&conf, pool);

        const char *err = mesi_set_allowed_hosts(&conf, pool, values[i]);

        ASSERT_NOT_NULL(err);
        ASSERT_EQ(conf.allowed_hosts->nelts, 0);
    }
}

/* #532: the directive is RAW_ARGS, so quotes are not stripped by Apache;
 * a quoted value is rejected instead of storing hosts with quote chars. */
TEST(quoted_hostnames_rejected) {
    const char *values[] = {
        "\"backend\"",
        "\"backend other\"",
        "'backend'",
        "backend \"other\"",
        "backend 'other",
        "\"",
    };
    for (size_t i = 0; i < sizeof(values) / sizeof(values[0]); i++) {
        mesi_config conf;
        mesi_config_init(&conf, pool);

        const char *err = mesi_set_allowed_hosts(&conf, pool, values[i]);

        ASSERT_NOT_NULL(err);
        ASSERT_STR_CONTAINS(err, "quote characters");
        ASSERT_EQ(conf.allowed_hosts->nelts, 0);
    }
}

/* A rune next to the whitespace range (U+200B zero width space) is not
 * whitespace, and invalid or truncated UTF-8 is a token, exactly like
 * strings.Fields. */
TEST(non_whitespace_runes_are_tokens) {
    const char *values[] = {
        "\xe2\x80\x8b", /* U+200B zero width space: not unicode.IsSpace */
        "\xc2",         /* truncated UTF-8 */
        "\xe2\x80",     /* truncated 3-byte sequence */
        "\xff",         /* invalid byte */
    };
    for (size_t i = 0; i < sizeof(values) / sizeof(values[0]); i++) {
        mesi_config conf;
        mesi_config_init(&conf, pool);

        const char *err = mesi_set_allowed_hosts(&conf, pool, values[i]);

        ASSERT_NULL(err);
        ASSERT_EQ(conf.allowed_hosts->nelts, 1);
    }
}

/* A real hostname surrounded by Unicode whitespace is accepted. */
TEST(hostname_with_unicode_whitespace) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allowed_hosts(&conf, pool, "\xc2\xa0 backend \xe3\x80\x80");

    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 3);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[1], "backend");
}

TEST(block_private_on) {
    mesi_config conf;
    conf.block_private_ips = -1;

    const char *err = mesi_set_block_private_ips(&conf, 1);

    ASSERT_NULL(err);
    ASSERT_EQ(conf.block_private_ips, 1);
}

TEST(block_private_off) {
    mesi_config conf;
    conf.block_private_ips = -1;

    const char *err = mesi_set_block_private_ips(&conf, 0);

    ASSERT_NULL(err);
    ASSERT_EQ(conf.block_private_ips, 0);
}

TEST(merge_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    base.block_private_ips = 0;
    add.block_private_ips = 1;

    mesi_merge_configs(&base, &add, &merged);

    ASSERT_EQ(merged.block_private_ips, 1);
}

TEST(merge_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    base.block_private_ips = 1;
    add.block_private_ips = -1;

    mesi_merge_configs(&base, &add, &merged);

    ASSERT_EQ(merged.block_private_ips, 1);
}

TEST(merge_allowed_hosts_child_set) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    const char **h1 = apr_array_push(base.allowed_hosts);
    *h1 = apr_pstrdup(pool, "base.com");

    const char **h2 = apr_array_push(add.allowed_hosts);
    *h2 = apr_pstrdup(pool, "add.com");

    mesi_merge_configs(&base, &add, &merged);

    ASSERT_EQ(merged.allowed_hosts->nelts, 1);
    ASSERT_STR_EQ(((const char **)merged.allowed_hosts->elts)[0], "add.com");
}

/* --- MesiAllowPrivateIPsForAllowedHosts directive tests (#168) --- */

TEST(allow_private_for_allowed_default_unset) {
    /* A freshly-created config has the sentinel -1 (unset → off). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.allow_private_ips_for_allowed, -1);
}

TEST(allow_private_for_allowed_on) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allow_private_for_allowed(&conf, 1);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allow_private_ips_for_allowed, 1);
}

TEST(allow_private_for_allowed_off) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_allow_private_for_allowed(&conf, 0);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allow_private_ips_for_allowed, 0);
}

TEST(merge_allow_private_for_allowed_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.allow_private_ips_for_allowed = 0;
    add.allow_private_ips_for_allowed = 1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.allow_private_ips_for_allowed, 1);
}

TEST(merge_allow_private_for_allowed_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.allow_private_ips_for_allowed = 1;
    add.allow_private_ips_for_allowed = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.allow_private_ips_for_allowed, 1);
}

/* --- MesiSharedHTTPClient directive tests (#178) --- */

TEST(shared_http_client_default_unset) {
    /* A freshly-created config has the sentinel -1 (unset → off). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.shared_http_client, -1);
}

TEST(shared_http_client_on) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_shared_http_client(&conf, 1);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.shared_http_client, 1);
}

TEST(shared_http_client_off) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_shared_http_client(&conf, 0);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.shared_http_client, 0);
}

TEST(merge_shared_http_client_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.shared_http_client = 0;
    add.shared_http_client = 1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.shared_http_client, 1);
}

TEST(merge_shared_http_client_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.shared_http_client = 1;
    add.shared_http_client = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.shared_http_client, 1);
}

/* --- MesiDebug directive tests (#173) --- */

TEST(debug_default_unset) {
    /* A freshly-created config has the sentinel -1 (unset → off): the
     * parser stays silent, byte-identical to pre-#173 behaviour. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.debug, -1);
}

TEST(debug_on) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_debug(&conf, 1);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.debug, 1);
}

TEST(debug_off) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_debug(&conf, 0);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.debug, 0);
}

TEST(merge_debug_child_overrides) {
    /* Child On / parent Off → On (add overrides base). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.debug = 0;
    add.debug = 1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.debug, 1);
}

TEST(merge_debug_child_off_overrides) {
    /* Explicit Off must override an inherited On — 0 is configured,
     * not unset (the whole reason the sentinel is -1). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.debug = 1;
    add.debug = 0;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.debug, 0);
}

TEST(merge_debug_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.debug = 1;
    add.debug = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.debug, 1);
}

TEST(merge_debug_both_unset) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.debug, -1);
}

/* --- MesiParseOnHeader directive + Edge-control matcher tests (#172) --- */

TEST(parse_on_header_default_unset) {
    /* A freshly-created config has the sentinel -1 (unset → off): every
     * HTML response is parsed, byte-identical to pre-#172 behaviour. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.parse_on_header, -1);
}

TEST(parse_on_header_on) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_parse_on_header(&conf, 1);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.parse_on_header, 1);
}

TEST(parse_on_header_off) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_parse_on_header(&conf, 0);
    ASSERT_NULL(err);
    ASSERT_EQ(conf.parse_on_header, 0);
}

TEST(merge_parse_on_header_child_overrides) {
    /* Child On / parent Off → On (add overrides base). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.parse_on_header = 0;
    add.parse_on_header = 1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.parse_on_header, 1);
}

TEST(merge_parse_on_header_child_off_overrides) {
    /* Explicit Off must override an inherited On — 0 is configured,
     * not unset (the whole reason the sentinel is -1). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.parse_on_header = 1;
    add.parse_on_header = 0;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.parse_on_header, 0);
}

TEST(merge_parse_on_header_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.parse_on_header = 1;
    add.parse_on_header = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.parse_on_header, 1);
}

TEST(merge_parse_on_header_both_unset) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.parse_on_header, -1);
}

TEST(header_signals_esi_matches) {
    ASSERT_EQ(mesi_header_signals_esi("dca=esi"), 1);
}

TEST(header_signals_esi_case_insensitive) {
    ASSERT_EQ(mesi_header_signals_esi("DCA=ESI"), 1);
    ASSERT_EQ(mesi_header_signals_esi("Dca=Esi"), 1);
}

TEST(header_signals_esi_comma_list) {
    ASSERT_EQ(mesi_header_signals_esi("no-cache, dca=esi"), 1);
    ASSERT_EQ(mesi_header_signals_esi("no-cache,dca=esi,private"), 1);
    ASSERT_EQ(mesi_header_signals_esi("  dca=esi  "), 1);
    ASSERT_EQ(mesi_header_signals_esi("dca=esi,"), 1);
}

TEST(header_signals_esi_no_match) {
    ASSERT_EQ(mesi_header_signals_esi("max-age=60"), 0);
    ASSERT_EQ(mesi_header_signals_esi("esi"), 0);
    ASSERT_EQ(mesi_header_signals_esi("dca=esi-extra"), 0);
    ASSERT_EQ(mesi_header_signals_esi("xdca=esi"), 0);
    ASSERT_EQ(mesi_header_signals_esi(""), 0);
}

TEST(header_signals_esi_null) {
    ASSERT_EQ(mesi_header_signals_esi(NULL), 0);
}

/* --- Cache backend directive tests (#174) --- */

TEST(cache_backend_memory) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    /* camelCase constant "memory" */
    ASSERT_NULL(mesi_set_cache_backend(&conf, pool, "memory", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_backend, "memory");
}

TEST(cache_backend_empty_disables) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    conf.cache_backend = "memory";

    /* `""` is the explicit empty token (the raw form Apache passes for the
     * quoted directive value); it disables caching as a configured value. */
    ASSERT_NULL(mesi_set_cache_backend(&conf, pool, "\"\"", parse_conf_word));
    ASSERT_NOT_NULL(conf.cache_backend);
    ASSERT_STR_EQ(conf.cache_backend, "");
}

TEST(cache_backend_quoted_accepted) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_backend(&conf, pool, "\"memory\"", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_backend, "memory");
}

TEST(cache_backend_missing_argument_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_backend(&conf, pool, "", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "requires exactly one argument");
    ASSERT_NULL(conf.cache_backend);

    ASSERT_NOT_NULL(mesi_set_cache_backend(&conf, pool, "   ", parse_conf_word));
    ASSERT_NOT_NULL(mesi_set_cache_backend(&conf, pool, NULL, parse_conf_word));
    ASSERT_NULL(conf.cache_backend);
}

TEST(cache_backend_trailing_argument_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_backend(&conf, pool, "memory extra", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "takes exactly one argument");
    ASSERT_NULL(conf.cache_backend);

    /* `"" ""` has a second (empty) token, so it is still trailing input. */
    err = mesi_set_cache_backend(&conf, pool, "\"\" \"\"", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "takes exactly one argument");
}

TEST(cache_backend_unknown_rejected) {
    /* Unknown backend must NOT silently fall back to "no cache" — the
     * workflow prohibits silent-default in parsers. Reject loudly. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_backend(&conf, pool, "file", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "file");
    ASSERT_STR_CONTAINS(err, "memory");
    ASSERT_STR_CONTAINS(err, "redis");
    ASSERT_STR_CONTAINS(err, "memcached");
    /* config left at the default NULL (unset/inherit) rather than being
     * polluted with a bogus value */
    ASSERT_NULL(conf.cache_backend);
}

TEST(cache_backend_redis_accepted) {
    /* Redis backend is supported (#175). */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_backend(&conf, pool, "redis", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_backend, "redis");
}

TEST(cache_backend_memcached_accepted) {
    /* Memcached backend string is accepted; runtime InitCacheWithConfig
     * will surface the missing servers list (#176). */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_backend(&conf, pool, "memcached", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_backend, "memcached");
}

TEST(cache_backend_unknown_variant_rejected) {
    /* A near-miss like "rediscluster" must be rejected, not silently
     * aliased. Parity with #174 unknown-rejected test. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_backend(&conf, pool, "rediscluster", parse_conf_word);
    ASSERT_NOT_NULL(err);
    /* value was NOT stored */
    ASSERT_NULL(conf.cache_backend);
}

TEST(cache_size_default_unset) {
    /* 0 / -1 are sentinel "unset" — directive never invokes parser with those */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_EQ(conf.cache_size, 0);
    ASSERT_EQ(conf.cache_ttl, -1);
}

TEST(cache_size_valid) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_size(&conf, pool, "5000"));
    ASSERT_EQ(conf.cache_size, 5000);
}

TEST(cache_size_min_accepted) {
    /* Boundary: 1 is the smallest legal size */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_size(&conf, pool, "1"));
    ASSERT_EQ(conf.cache_size, 1);
}

TEST(cache_size_zero_rejected) {
    /* Boundary: 0 must be rejected (size must be positive) */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_size(&conf, pool, "0");
    ASSERT_NOT_NULL(err);
    /* config must NOT silently retain a non-zero value */
    ASSERT_EQ(conf.cache_size, 0);
}

TEST(cache_size_negative_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_size(&conf, pool, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "non-negative");
}

TEST(cache_size_max_accepted) {
    /* Boundary: MESI_MAX_CACHE_SIZE (1000000) is accepted */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    char buf[16];
    snprintf(buf, sizeof(buf), "%d", MESI_MAX_CACHE_SIZE);
    ASSERT_NULL(mesi_set_cache_size(&conf, pool, buf));
    ASSERT_EQ(conf.cache_size, MESI_MAX_CACHE_SIZE);
}

TEST(cache_size_max_plus_one_rejected) {
    /* Boundary: MESI_MAX_CACHE_SIZE+1 must be rejected */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    char buf[16];
    snprintf(buf, sizeof(buf), "%d", MESI_MAX_CACHE_SIZE + 1);
    const char *err = mesi_set_cache_size(&conf, pool, buf);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "out of range");
}

TEST(cache_size_decimal_rejected) {
    /* Decimals must fail-fast — no silent truncation */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_size(&conf, pool, "100.5");
    ASSERT_NOT_NULL(err);
}

TEST(cache_size_alpha_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_size(&conf, pool, "abc");
    ASSERT_NOT_NULL(err);
}

TEST(cache_size_oversized_rejected) {
    /* 12345678901 (11 digits) must be rejected for safety */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_size(&conf, pool, "12345678901");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "exceeds");
}

TEST(cache_size_with_leading_space) {
    /* Leading [ \t] is allowed by the parser */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_size(&conf, pool, "  42"));
    ASSERT_EQ(conf.cache_size, 42);
}

TEST(cache_size_with_leading_plus_rejected) {
    /* "+10" is rejected — only [ \t] can precede the integer */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_size(&conf, pool, "+10");
    ASSERT_NOT_NULL(err);
}

TEST(cache_size_empty_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_size(&conf, pool, "");
    ASSERT_NOT_NULL(err);
}

TEST(cache_ttl_zero_accepted) {
    /* Boundary: 0 is the smallest legal TTL (no expiry) */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_ttl(&conf, pool, "0"));
    ASSERT_EQ(conf.cache_ttl, 0);
}

TEST(cache_ttl_valid) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_ttl(&conf, pool, "60"));
    ASSERT_EQ(conf.cache_ttl, 60);
}

TEST(cache_ttl_max_accepted) {
    /* Boundary: 86400 (1 day) is the configurable max */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_ttl(&conf, pool, "86400"));
    ASSERT_EQ(conf.cache_ttl, 86400);
}

TEST(cache_ttl_max_plus_one_rejected) {
    /* Boundary: 86401 must be rejected */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_ttl(&conf, pool, "86401");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "out of range");
}

TEST(cache_ttl_negative_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_ttl(&conf, pool, "-5");
    ASSERT_NOT_NULL(err);
}

TEST(cache_ttl_decimal_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_ttl(&conf, pool, "30.5");
    ASSERT_NOT_NULL(err);
}

TEST(cache_ttl_alpha_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_ttl(&conf, pool, "60s");
    ASSERT_NOT_NULL(err);
}

TEST(merge_cache_backend_child_overrides) {
    /* Child directive wins even when it differs from parent.
     * (Both having the same value would not exercise the override
     * branch — child simply needs an explicit, non-empty backend.) */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_backend = "";      /* parent has cache disabled */
    add.cache_backend = "memory"; /* child opts in */

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_backend, "memory");
}

TEST(merge_cache_backend_child_inherits) {
    /* Child unset (NULL) inherits the parent's backend (#617). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_backend = "memory";
    add.cache_backend = NULL;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_backend, "memory");
}

TEST(merge_cache_backend_child_clears) {
    /* A child's explicit empty value disables an inherited backend (#617).
     * Before this, "" was treated like unset and the global cache leaked
     * into every vhost. */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_backend = "memory";
    add.cache_backend = "";

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_NOT_NULL(merged.cache_backend);
    ASSERT_STR_EQ(merged.cache_backend, "");
}

TEST(merge_cache_size_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_size = 100;
    add.cache_size = 5000;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_size, 5000);
}

TEST(merge_cache_size_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_size = 100;
    add.cache_size = 0; /* sentinel "unset" */

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_size, 100);
}

TEST(merge_cache_ttl_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_ttl = 30;
    add.cache_ttl = 0; /* explicit 0 = no expiry */

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_ttl, 0);
}

TEST(merge_cache_ttl_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_ttl = 30;
    add.cache_ttl = -1; /* sentinel "unset" */

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_ttl, 30);
}

/* --- Redis backend directive tests (#175) --- */

TEST(redis_addr_default_unset) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(conf.cache_redis_addr);
}

TEST(redis_addr_valid) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5:6379", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_addr, "10.0.0.5:6379");
}

TEST(redis_addr_localhost_default_port) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_addr(&conf, pool, "localhost:6379", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_addr, "localhost:6379");
}

TEST(redis_addr_hostname_with_port) {
    /* Issue example: "10.0.0.5:6379" — also confirms hostnames work. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_addr(&conf, pool, "redis.local:6380", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_addr, "redis.local:6380");
}

TEST(redis_addr_empty_clears) {
    /* `""` explicitly clears the address (operator action: use libgomesi
     * defaults localhost:6379). It must stay non-NULL so the merge treats
     * it as a configured value instead of inheriting a global address. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    conf.cache_redis_addr = "10.0.0.5:6379"; // existing value

    ASSERT_NULL(mesi_set_cache_redis_addr(&conf, pool, "\"\"", parse_conf_word));
    ASSERT_NOT_NULL(conf.cache_redis_addr);
    ASSERT_STR_EQ(conf.cache_redis_addr, "");
}

TEST(redis_addr_quoted_accepted) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_addr(&conf, pool, "\"10.0.0.5:6379\"", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_addr, "10.0.0.5:6379");
}

TEST(redis_addr_missing_argument_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "requires exactly one argument");
    ASSERT_NULL(conf.cache_redis_addr);

    ASSERT_NOT_NULL(mesi_set_cache_redis_addr(&conf, pool, "   ", parse_conf_word));
    ASSERT_NOT_NULL(mesi_set_cache_redis_addr(&conf, pool, NULL, parse_conf_word));
    ASSERT_NULL(conf.cache_redis_addr);
}

TEST(redis_addr_trailing_argument_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5:6379 extra", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "takes exactly one argument");
    ASSERT_NULL(conf.cache_redis_addr);
}

TEST(redis_addr_missing_port_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host:port");
}

TEST(redis_addr_missing_host_rejected) {
    /* ":6379" — colon at start, no host. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, ":6379", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host:port");
}

TEST(redis_addr_missing_port_value_rejected) {
    /* "host:" — colon at end. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5:", parse_conf_word);
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_port_zero_rejected) {
    /* Boundary: port must be >= 1. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5:0", parse_conf_word);
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_port_max_accepted) {
    /* Boundary: port 65535 is the largest legal port. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5:65535", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_addr, "10.0.0.5:65535");
}

TEST(redis_addr_port_max_plus_one_rejected) {
    /* Boundary: 65536 rejected. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5:65536", parse_conf_word);
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_port_negative_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5:-1", parse_conf_word);
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_with_whitespace_rejected) {
    /* JSON cannot be safely generated with embedded whitespace or
     * tabs. The value must be quoted to reach the setter as one token,
     * so the rejection is the character check, not the tokenizer. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "\"10.0.0.5 :6379\"", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(redis_addr_with_quote_rejected) {
    /* Embedded '"' would inject JSON keys. Reject. The raw form escapes
     * the inner quote so ap_getword_conf() yields it inside the token. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "\"10.0.0.5\\\":6379\"", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(redis_addr_with_backslash_rejected) {
    /* Embedded '\\' would inject JSON escape sequences. Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, "10.0.0.5\\:6379", parse_conf_word);
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_null_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_addr(&conf, pool, NULL, parse_conf_word);
    ASSERT_NOT_NULL(err);
}

TEST(redis_addr_ipv6_localhost_accepted) {
    /* [::1]:6379 — IPv6 literal. Note: we treat as a single colon in
     * strrchr so the *last* colon is the port separator; the host
     * itself may contain colons. Validate that the resulting port
     * is in 1..65535 and that the inner content is unfiltered only
     * for control chars (we don't validate IPv6 syntax beyond that). */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_addr(&conf, pool, "[::1]:6379", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_addr, "[::1]:6379");
}

TEST(redis_password_default_unset) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(conf.cache_redis_password);
}

TEST(redis_password_empty_accepted) {
    /* `""` is an explicit empty password = no auth, non-NULL so it
     * overrides an inherited password (#617). */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_password(&conf, pool, "\"\"", parse_conf_word));
    ASSERT_NOT_NULL(conf.cache_redis_password);
    ASSERT_STR_EQ(conf.cache_redis_password, "");
}

TEST(redis_password_quoted_with_space_accepted) {
    /* A password containing spaces must be quoted; the quotes are
     * stripped by ap_getword_conf(), the space survives. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_password(&conf, pool, "\"p@ss word\"", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_password, "p@ss word");
}

TEST(redis_password_missing_argument_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_password(&conf, pool, "", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "requires exactly one argument");
    ASSERT_NULL(conf.cache_redis_password);

    ASSERT_NOT_NULL(mesi_set_cache_redis_password(&conf, pool, "   ", parse_conf_word));
    ASSERT_NOT_NULL(mesi_set_cache_redis_password(&conf, pool, NULL, parse_conf_word));
    ASSERT_NULL(conf.cache_redis_password);
}

TEST(redis_password_trailing_argument_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_password(&conf, pool, "secret extra", parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "takes exactly one argument");
    ASSERT_NULL(conf.cache_redis_password);
}

TEST(redis_password_valid) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_password(&conf, pool, "supersecret123", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_password, "supersecret123");
}

TEST(redis_password_with_special_chars_accepted) {
    /* Quotes and backslashes inside the password are fine — they get
     * escaped by build_redis_config_json. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_password(&conf, pool, "p@ssw0rd\"with\\quotes", parse_conf_word));
    ASSERT_STR_EQ(conf.cache_redis_password, "p@ssw0rd\"with\\quotes");
}

TEST(redis_password_with_control_char_rejected) {
    /* Newlines, tabs, BEL, etc. would corrupt the rendered JSON. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_password(&conf, pool, "abc\x01", parse_conf_word);
    ASSERT_NOT_NULL(err);
    /* Must NOT leak the password into the error message. */
    ASSERT_NULL(strstr(err, "abc"));
    ASSERT_STR_CONTAINS(err, "control character");
}

TEST(redis_password_null_rejected) {
    /* RAW_ARGS passes the raw line; a NULL/empty line is a missing
     * argument, not an implicit clear (#617). */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_password(&conf, pool, NULL, parse_conf_word);
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "requires exactly one argument");
    ASSERT_NULL(conf.cache_redis_password);
}

TEST(redis_db_default_unset) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_EQ(conf.cache_redis_db, -1);
}

TEST(redis_db_zero_accepted) {
    /* Boundary: 0 is the smallest legal DB number. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_db(&conf, pool, "0"));
    ASSERT_EQ(conf.cache_redis_db, 0);
}

TEST(redis_db_valid) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NULL(mesi_set_cache_redis_db(&conf, pool, "2"));
    ASSERT_EQ(conf.cache_redis_db, 2);
}

TEST(redis_db_max_accepted) {
    /* Boundary: MESI_MAX_REDIS_DB (15) is the largest legal DB. */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    char buf[16];
    snprintf(buf, sizeof(buf), "%d", MESI_MAX_REDIS_DB);
    ASSERT_NULL(mesi_set_cache_redis_db(&conf, pool, buf));
    ASSERT_EQ(conf.cache_redis_db, MESI_MAX_REDIS_DB);
}

TEST(redis_db_max_plus_one_rejected) {
    /* Boundary: 16 is out of range (Redis default max is 16). */
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_db(&conf, pool, "16");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "out of range");
}

TEST(redis_db_negative_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_db(&conf, pool, "-1");
    ASSERT_NOT_NULL(err);
    /* config must NOT silently retain a non-negative value */
    ASSERT_EQ(conf.cache_redis_db, -1);
}

TEST(redis_db_decimal_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_db(&conf, pool, "2.5");
    ASSERT_NOT_NULL(err);
}

TEST(redis_db_empty_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_db(&conf, pool, "");
    ASSERT_NOT_NULL(err);
}

TEST(redis_db_oversized_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_cache_redis_db(&conf, pool, "12345678901");
    ASSERT_NOT_NULL(err);
}

TEST(merge_redis_addr_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_redis_addr = NULL;
    add.cache_redis_addr = "10.0.0.5:6379";

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_NOT_NULL(merged.cache_redis_addr);
    ASSERT_STR_EQ(merged.cache_redis_addr, "10.0.0.5:6379");
}

TEST(merge_redis_addr_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_redis_addr = "10.0.0.5:6379";
    add.cache_redis_addr = NULL;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_redis_addr, "10.0.0.5:6379");
}

TEST(merge_redis_addr_child_clears) {
    /* Explicit empty (non-NULL) clears an inherited address (#617). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_redis_addr = "10.0.0.5:6379";
    add.cache_redis_addr = "";

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_NOT_NULL(merged.cache_redis_addr);
    ASSERT_STR_EQ(merged.cache_redis_addr, "");
}

TEST(merge_redis_db_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_redis_db = -1;
    add.cache_redis_db = 2;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_redis_db, 2);
}

TEST(merge_redis_db_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_redis_db = 5;
    add.cache_redis_db = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_redis_db, 5);
}

TEST(merge_redis_password_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_redis_password = NULL;
    add.cache_redis_password = "secret";

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_redis_password, "secret");
}

TEST(merge_redis_password_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_redis_password = "secret";
    add.cache_redis_password = NULL;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_redis_password, "secret");
}

TEST(merge_redis_password_child_clears) {
    /* Explicit empty (non-NULL) clears an inherited password (#617). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_redis_password = "secret";
    add.cache_redis_password = "";

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_NOT_NULL(merged.cache_redis_password);
    ASSERT_STR_EQ(merged.cache_redis_password, "");
}

/* --- Memcached backend directive tests (#176) --- */

TEST(memcached_servers_default_unset) {
    /* A freshly-created config has an empty server array (nelts == 0),
     * not NULL — runtime cache init renders an empty JSON array so the
     * libgomesi parser produces a deterministic error. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NOT_NULL(conf.cache_memcached_servers);
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_single) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:11211"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 1);
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "10.0.0.1:11211");
}

TEST(memcached_servers_multiple) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool,
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
    mesi_config_init(&conf, pool);
    /* Tabs and spaces between entries; leading/trailing whitespace
     * silently trimmed (matches set_allowed_hosts behavior). */
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool,
                                                 "  host1:11211\thost2:11211  \thost3:11211"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 3);
}

TEST(memcached_servers_ascii_whitespace_set) {
    /* The whole ASCII whitespace set (space, tab, CR, LF, VT, FF) is a
     * separator, matching strings.Fields (#559). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool,
                                                 "\r\n\v\f\thost1:11211 host2:11211\r\n"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 2);
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "host1:11211");
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[1],
                  "host2:11211");
}

TEST(memcached_servers_unicode_whitespace_separators) {
    /* Every Unicode whitespace rune separates entries (#559), matching
     * Go's strings.Fields rule: NBSP, ogham space mark, EM SPACE,
     * LINE SEPARATOR and IDEOGRAPHIC SPACE. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_memcached_servers(
        &conf, pool,
        "host1:11211\xc2\xa0host2:11211\xe1\x9a\x80host3:11211\xe2\x80\x83"
        "host4:11211\xe2\x80\xa8host5:11211\xe3\x80\x80"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 5);
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "host1:11211");
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[2],
                  "host3:11211");
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[4],
                  "host5:11211");
}

TEST(memcached_servers_unicode_whitespace_only_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "\xc2\xa0\xe3\x80\x80");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "at least one");
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_nbsp_inside_entry_rejected) {
    /* NBSP inside an entry splits it under strings.Fields, so accepting
     * "mc<NBSP>:11211" would silently give libgomesi two invalid
     * servers. The module rejects the halves at config load instead
     * (#559). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "mc\xc2\xa0:11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiCacheMemcachedServers");
    ASSERT_STR_CONTAINS(err, "host:port");
}

TEST(memcached_servers_invalid_utf8_is_not_a_separator) {
    /* Invalid or truncated UTF-8 is part of a token, never whitespace
     * (strings.Fields decodes it as U+FFFD), so the lone 0xc2 is a
     * malformed entry instead of a separator between two valid ones. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool,
                                                       "host-a:11211 \xc2 host-b:11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiCacheMemcachedServers");
    ASSERT_STR_CONTAINS(err, "host:port");
}

TEST(memcached_servers_empty_list_rejected) {
    /* Boundary: an empty value list produces no entries — must be
     * rejected, never treated as a valid config. Operators who
     * accidentally leave the directive empty should see an error. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "at least one");
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_whitespace_only_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "   \t  ");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "at least one");
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_null_arg_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, NULL);
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_default_port_explicit_rejected) {
    /* Issue example: "10.0.0.1" (no port) — fail-fast instead of
     * accepting whatever default memcache.New might pick. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host:port");
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_only_colon_rejected) {
    /* ":11211" — colon at start, empty host. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, ":11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host:port");
}

TEST(memcached_servers_missing_port_value_rejected) {
    /* "host:" — colon at end, empty port. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:");
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_port_zero_rejected) {
    /* Boundary: port must be >= 1. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:0");
    ASSERT_NOT_NULL(err);
    /* Error must NOT leak the value verbatim into a control-char
     * path; use port-invalid phrasing instead. */
    ASSERT_STR_CONTAINS(err, "port invalid");
}

TEST(memcached_servers_port_max_accepted) {
    /* Boundary: port 65535 is the largest legal port. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:65535"));
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "10.0.0.1:65535");
}

TEST(memcached_servers_port_max_plus_one_rejected) {
    /* Boundary: port 65536 is out of range. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:65536");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "port invalid");
}

TEST(memcached_servers_negative_port_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:-1");
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_decimal_port_rejected) {
    /* parse_nonneg_int rejects decimal ports in [0,1), so 11211.5 fails
     * before the int parser trips. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:11211.5");
    ASSERT_NOT_NULL(err);
}

TEST(memcached_servers_alpha_port_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "host:http");
    ASSERT_NOT_NULL(err);
}

/* #542: the directive is RAW_ARGS, so Apache does not unquote the line
 * and never strips a quote. MesiAllowedHosts got the same treatment in
 * #532; without it 'mc':11211 stores the host 'mc', which can never
 * resolve, and the operator sees no error at config load. */
TEST(memcached_servers_single_quote_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "'mc':11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "quote characters are not allowed");
}

/* Renamed from memcached_servers_internal_whitespace_rejected: tokens are
 * extracted on whitespace boundaries, so internal whitespace is impossible
 * at this layer, and since #542 the double quote no longer reaches the
 * generic invalid-character check. */
TEST(memcached_servers_double_quote_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1\" :11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "quote characters are not allowed");
}

TEST(memcached_servers_quoted_entry_rejected) {
    /* The usual quoted forms. These failed before #542 too, but with a
     * misleading message: a double-quoted entry tripped the generic
     * invalid-character check, and a single-quoted one reached the port
     * parser and complained about the port. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_STR_CONTAINS(mesi_set_cache_memcached_servers(&conf, pool, "'10.0.0.1:11211'"),
                        "quote characters are not allowed");
    mesi_config_init(&conf, pool);
    ASSERT_STR_CONTAINS(mesi_set_cache_memcached_servers(&conf, pool, "\"10.0.0.1:11211\""),
                        "quote characters are not allowed");
}

TEST(memcached_servers_quote_in_later_entry_rejected) {
    /* The scan is per entry, so a quote in the second entry is caught
     * even though the first one is valid. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:11211 '10.0.0.2:11211'");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "quote characters are not allowed");
    ASSERT_STR_CONTAINS(err, "'10.0.0.2:11211'");
}

TEST(memcached_servers_unquoted_still_accepted) {
    /* The rejection must not narrow the accepted set. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1:11211 10.0.0.2:11211 [::1]:11211"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 3);
}

TEST(memcached_servers_backslash_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1\\:11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(memcached_servers_control_char_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "10.0.0.1\x01:11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "invalid character");
}

TEST(memcached_servers_del_rejected) {
    /* DEL (0x7f) is not `< 0x20`, so it slipped through the scan before
     * #559 and was stored inside the rendered JSON. Reject it, naming
     * the directive and the offending entry like every sibling
     * validator (set_cache_key_template, php-ext mesi_is_safe_string). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, "mc\x7f:11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiCacheMemcachedServers");
    ASSERT_STR_CONTAINS(err, "invalid character");
    ASSERT_STR_CONTAINS(err, "mc\x7f:11211");
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 0);
}

TEST(memcached_servers_del_in_later_entry_rejected) {
    /* The per-entry scan keeps the first valid entry but must reject
     * the malformed later one instead of storing it. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_memcached_servers(&conf, pool,
                                                       "host-a:11211 host-b\x7f:11211");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "host-b\x7f:11211");
}

TEST(memcached_servers_max_count_accepted) {
    /* Boundary: MESI_MAX_MEMCACHED_SERVERS entries are accepted in
     * one directive. We build "n.n.n.n:nnnn" × N in a fresh pool. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    char args[4096];
    int pos = 0;
    for (int i = 0; i < MESI_MAX_MEMCACHED_SERVERS && pos < (int)sizeof(args) - 32; i++) {
        int n = snprintf(args + pos, sizeof(args) - pos,
                         "%s10.0.0.%d:11211", (i == 0 ? "" : " "), i);
        if (n < 0 || n >= (int)sizeof(args) - pos) break;
        pos += n;
    }
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool, args));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, MESI_MAX_MEMCACHED_SERVERS);
}

TEST(memcached_servers_over_max_rejected) {
    /* Boundary: MESI_MAX_MEMCACHED_SERVERS + 1 must be rejected. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    char args[8192];
    int pos = 0;
    for (int i = 0; i < MESI_MAX_MEMCACHED_SERVERS + 1 && pos < (int)sizeof(args) - 32; i++) {
        int n = snprintf(args + pos, sizeof(args) - pos,
                         "%s10.0.0.%d:11211", (i == 0 ? "" : " "), i);
        if (n < 0 || n >= (int)sizeof(args) - pos) break;
        pos += n;
    }
    const char *err = mesi_set_cache_memcached_servers(&conf, pool, args);
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
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool, "[::1]:11211"));
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "[::1]:11211");
}

TEST(memcached_servers_append_to_existing_in_field) {
    /* Mock mirrors mod_mesi.c: each call *appends* to the array rather
     * than replacing it, matching set_allowed_hosts. Calling the
     * directive twice builds a longer list (operator can split a list
     * across multiple directive lines). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool, "host-a:11211"));
    ASSERT_NULL(mesi_set_cache_memcached_servers(&conf, pool, "host-b:11211"));
    ASSERT_EQ(conf.cache_memcached_servers->nelts, 2);
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[0],
                  "host-a:11211");
    ASSERT_STR_EQ(((const char **)conf.cache_memcached_servers->elts)[1],
                  "host-b:11211");
}

TEST(merge_memcached_servers_child_overrides) {
    /* Child directive with entries wins over parent. */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    const char **b1 = apr_array_push(base.cache_memcached_servers);
    *b1 = apr_pstrdup(pool, "10.0.0.1:11211");
    const char **b2 = apr_array_push(add.cache_memcached_servers);
    *b2 = apr_pstrdup(pool, "10.0.0.99:11211");

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_memcached_servers->nelts, 1);
    ASSERT_STR_EQ(((const char **)merged.cache_memcached_servers->elts)[0],
                  "10.0.0.99:11211");
}

TEST(merge_memcached_servers_child_inherits) {
    /* Child has empty list → inherits parent's list verbatim. */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    const char **b1 = apr_array_push(base.cache_memcached_servers);
    *b1 = apr_pstrdup(pool, "10.0.0.1:11211");

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.cache_memcached_servers->nelts, 1);
    ASSERT_STR_EQ(((const char **)merged.cache_memcached_servers->elts)[0],
                  "10.0.0.1:11211");
}

/* --- MesiCacheKeyTemplate directive tests (#177) --- */
TEST(cache_key_template_default_null) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(conf.cache_key_template);
}
TEST(cache_key_template_valid_simple) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_key_template(&conf, pool, "mesi:${url}"));
    ASSERT_NOT_NULL(conf.cache_key_template);
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}");
}
TEST(cache_key_template_with_header) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_key_template(&conf, pool, "mesi:${url}:${header:Accept-Language}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${header:Accept-Language}");
}
TEST(cache_key_template_with_cookie) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_key_template(&conf, pool, "mesi:${url}:${cookie:segment}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${cookie:segment}");
}
TEST(cache_key_template_with_both) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_key_template(&conf, pool, "mesi:${url}:${header:Accept-Language}:${cookie:segment}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${header:Accept-Language}:${cookie:segment}");
}
TEST(cache_key_template_unknown_placeholder_literal) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_key_template(&conf, pool, "mesi:${url}:${unknown:foo}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${unknown:foo}");
}
TEST(cache_key_template_empty_clears) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    conf.cache_key_template = "old";
    ASSERT_NULL(mesi_set_cache_key_template(&conf, pool, ""));
    ASSERT_NULL(conf.cache_key_template);
}
TEST(cache_key_template_null_arg_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_key_template(&conf, pool, NULL);
    ASSERT_NOT_NULL(err);
}
TEST(cache_key_template_control_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_key_template(&conf, pool, "mesi:\001bad");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "control");
}
TEST(cache_key_template_mangled_double_colon_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_key_template(&conf, pool, "mesi::${header:X}");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "AH00111");
}
TEST(cache_key_template_trailing_colon_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_cache_key_template(&conf, pool, "mesi:${header:X}:");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "AH00111");
}
TEST(cache_key_template_escaped_dollar_accepted) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_key_template(&conf, pool, "mesi:${url}:${header:Accept-Language}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${url}:${header:Accept-Language}");
}
TEST(cache_key_template_mid_position_accepted) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_cache_key_template(&conf, pool, "mesi:${header:X}:${url}:${cookie:C}"));
    ASSERT_STR_EQ(conf.cache_key_template, "mesi:${header:X}:${url}:${cookie:C}");
}
TEST(cache_key_template_too_long_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    char big[MESI_MAX_CACHE_KEY_TEMPLATE + 10];
    memset(big, 'a', sizeof(big) - 1);
    big[sizeof(big) - 1] = '\0';
    const char *err = mesi_set_cache_key_template(&conf, pool, big);
    ASSERT_NOT_NULL(err);
}
TEST(merge_cache_key_template_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_key_template = "mesi:${url}";
    add.cache_key_template = "mesi:${url}:${header:X}";
    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_key_template, "mesi:${url}:${header:X}");
}
TEST(merge_cache_key_template_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.cache_key_template = "mesi:${url}";
    add.cache_key_template = NULL;
    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.cache_key_template, "mesi:${url}");
}

/* --- MesiMaxDepth directive tests (#166) --- */

TEST(max_depth_default_unset) {
    /* A freshly-created config has the sentinel -1 (unset → 5 in filter). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_three_accepted) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_depth(&conf, pool, "3"));
    ASSERT_EQ(conf.max_depth, 3);
}

TEST(max_depth_zero_accepted) {
    /* Explicit 0 is valid passthrough (no ESI fetch). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_depth(&conf, pool, "0"));
    ASSERT_EQ(conf.max_depth, 0);
}

TEST(max_depth_hundred_accepted) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_depth(&conf, pool, "100"));
    ASSERT_EQ(conf.max_depth, 100);
}

TEST(max_depth_max_accepted) {
    /* Boundary: MESI_MAX_MAX_DEPTH (10000) is accepted. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_depth(&conf, pool, "10000"));
    ASSERT_EQ(conf.max_depth, MESI_MAX_MAX_DEPTH);
}

TEST(max_depth_negative_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_depth(&conf, pool, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    /* config must NOT silently retain a non-negative value */
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_alpha_rejected) {
    /* atoi("abc") → 0 would silently become passthrough. Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_depth(&conf, pool, "abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_trailing_garbage_rejected) {
    /* atoi("3foo") → 3 would silently accept. Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_depth(&conf, pool, "3foo");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_empty_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_depth(&conf, pool, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_decimal_rejected) {
    /* Decimals must fail-fast — no silent truncation. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_depth(&conf, pool, "3.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(max_depth_oversize_rejected) {
    /* Boundary: MESI_MAX_MAX_DEPTH+1 (10001) must be rejected. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_depth(&conf, pool, "10001");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxDepth");
    ASSERT_EQ(conf.max_depth, -1);
}

TEST(merge_max_depth_child_overrides) {
    /* Child 3 / parent unset → 3 */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_depth = -1;
    add.max_depth = 3;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_depth, 3);
}

TEST(merge_max_depth_child_inherits) {
    /* Child unset / parent 10 → 10 */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_depth = 10;
    add.max_depth = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_depth, 10);
}

TEST(merge_max_depth_child_zero_overrides) {
    /* Explicit 0 must win over parent — 0 is configured, not unset. */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_depth = 5;
    add.max_depth = 0;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_depth, 0);
}

/* --- MesiIncludeErrorMarker directive tests (#165) --- */

TEST(include_error_marker_default_unset) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(conf.include_error_marker);
}

TEST(include_error_marker_set_unquoted) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err = mesi_set_include_error_marker(&conf, pool, "<!--marker-->", parse_conf_word);
    ASSERT_NULL(err);
    ASSERT_STR_EQ(conf.include_error_marker, "<!--marker-->");
}

TEST(include_error_marker_set_quoted_spaces) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    const char *err =
        mesi_set_include_error_marker(&conf, pool, "\"<!-- ESI include failed -->\"", parse_conf_word);
    ASSERT_NULL(err);
    ASSERT_STR_EQ(conf.include_error_marker, "<!-- ESI include failed -->");
}

TEST(include_error_marker_empty_stored) {
    /* Explicit "" reaches the setter as a quoted-empty raw argument
     * (Apache's TAKE1 parser would reject it before the setter), and is
     * stored as a non-NULL pointer so it can clear an inherited marker;
     * it renders nothing (same as unset). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    conf.include_error_marker = "<!-- parent -->";

    const char *err = mesi_set_include_error_marker(&conf, pool, "\"\"", parse_conf_word);
    ASSERT_NULL(err);
    ASSERT_NOT_NULL(conf.include_error_marker);
    ASSERT_STR_EQ(conf.include_error_marker, "");
}

TEST(include_error_marker_empty_single_quotes) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    conf.include_error_marker = "<!-- parent -->";

    const char *err = mesi_set_include_error_marker(&conf, pool, "''", parse_conf_word);
    ASSERT_NULL(err);
    ASSERT_NOT_NULL(conf.include_error_marker);
    ASSERT_STR_EQ(conf.include_error_marker, "");
}

TEST(include_error_marker_missing_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    conf.include_error_marker = "<!-- keep -->";

    ASSERT_NOT_NULL(mesi_set_include_error_marker(&conf, pool, "", parse_conf_word));
    ASSERT_NOT_NULL(mesi_set_include_error_marker(&conf, pool, " \t ", parse_conf_word));
    ASSERT_NOT_NULL(mesi_set_include_error_marker(&conf, pool, NULL, parse_conf_word));
    /* A rejected directive must not overwrite the configured marker. */
    ASSERT_STR_EQ(conf.include_error_marker, "<!-- keep -->");
}

TEST(include_error_marker_trailing_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);

    ASSERT_NOT_NULL(
        mesi_set_include_error_marker(&conf, pool, "\"<!-- ESI include failed -->\" extra", parse_conf_word));
    ASSERT_NOT_NULL(mesi_set_include_error_marker(&conf, pool, "\"\" \"\"", parse_conf_word));
    ASSERT_NULL(conf.include_error_marker);
}

TEST(merge_include_error_marker_child_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.include_error_marker = NULL;
    add.include_error_marker = "<!-- add -->";

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.include_error_marker, "<!-- add -->");
}

TEST(merge_include_error_marker_child_inherits) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.include_error_marker = "<!-- base -->";
    add.include_error_marker = NULL;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_STR_EQ(merged.include_error_marker, "<!-- base -->");
}

TEST(merge_include_error_marker_child_empty_clears) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.include_error_marker = "<!-- base -->";
    add.include_error_marker = "";

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_NOT_NULL(merged.include_error_marker);
    ASSERT_STR_EQ(merged.include_error_marker, "");
}

TEST(include_error_marker_json_escapes) {
    /* Quotes and backslashes must survive the ParseJson blob. */
    ASSERT_STR_EQ(mesi_json_string(pool, "<!-- \"err\" \\ path -->"),
                  "\"<!-- \\\"err\\\" \\\\ path -->\"");
    /* Control characters are \u00XX-escaped. */
    ASSERT_STR_EQ(mesi_json_string(pool, "a\nb"), "\"a\\u000ab\"");
}

/* --- ParseJson blob assembly (regression tests, #165/#167–#173) --- */

/* every optional directive set at once must land in the blob. This is the
 * regression test for #165: the merge that added the marker kept the
 * seven-%s apr_psprintf format string while passing eight optional parts,
 * so the last one (requestCtx) was silently dropped and a vhost that
 * routed through ParseJson lost its cache-key template context — different
 * Accept-Language values shared one URL-only cache entry. The assertion is
 * on the exact blob so a dropped or reordered part fails loudly. */
TEST(parse_json_blob_all_optional_keys) {
    mesi_parse_json_config cfg = {
        .depth = 3,
        .base_url = "http://backend:8080/",
        .allowed_hosts = "backend other",
        .block_private_ips = 1,
        .allow_private_for_allowed = 0,
        .request_ctx_json = "{\"ctx\":{\"header:Accept-Language\":\"pl\"}}",
        .timeout_seconds = 7,
        .max_response_size = 1048576,
        .max_concurrent_requests = 4,
        .max_workers = 8,
        .debug = 1,
        .parse_on_header = 1,
        .include_error_marker = "<!-- err -->",
        .cache_key_template = "{header:Accept-Language}",
    };
    const char *blob = mesi_build_parse_json_config(pool, &cfg);
    ASSERT_STR_EQ(blob,
                  "{\"maxDepth\":3"
                  ",\"defaultUrl\":\"http://backend:8080/\""
                  ",\"allowedHosts\":\"backend other\""
                  ",\"blockPrivateIPs\":true"
                  ",\"allowPrivateIPsForAllowedHosts\":false"
                  ",\"timeoutSeconds\":7"
                  ",\"maxResponseSize\":1048576"
                  ",\"maxConcurrentRequests\":4"
                  ",\"maxWorkers\":8"
                  ",\"debug\":true"
                  ",\"parseOnHeader\":true"
                  ",\"includeErrorMarker\":\"<!-- err -->\""
                  ",\"cacheKeyTemplate\":\"{header:Accept-Language}\""
                  ",\"requestCtx\":{\"ctx\":{\"header:Accept-Language\":\"pl\"}}}");
    /* Explicitly pin the two parts that share the "last argument" slot the
     * 7-%s bug dropped: the request context must survive, and it must come
     * after the template that consumes it. */
    ASSERT_STR_CONTAINS(blob, ",\"requestCtx\":{");
    ASSERT_STR_CONTAINS(blob, "\"cacheKeyTemplate\":\"{header:Accept-Language}\"");
    assert(strstr(blob, "\"cacheKeyTemplate\"") < strstr(blob, "\"requestCtx\""));
}

/* A config with every optional directive unset keeps the legacy blob: no
 * optional key at all (the core then applies its positional-path
 * defaults). */
TEST(parse_json_blob_minimal) {
    mesi_parse_json_config cfg = {
        .depth = 5,
        .base_url = "http://127.0.0.1/",
        .allowed_hosts = "backend",
        .block_private_ips = 0,
        .allow_private_for_allowed = 1,
        .request_ctx_json = "",
        .timeout_seconds = -1,
        .max_response_size = -1,
        .max_concurrent_requests = -1,
        .max_workers = -1,
        .debug = -1,
        .parse_on_header = -1,
        .include_error_marker = NULL,
        .cache_key_template = NULL,
    };
    const char *blob = mesi_build_parse_json_config(pool, &cfg);
    ASSERT_STR_EQ(blob,
                  "{\"maxDepth\":5"
                  ",\"defaultUrl\":\"http://127.0.0.1/\""
                  ",\"allowedHosts\":\"backend\""
                  ",\"blockPrivateIPs\":false"
                  ",\"allowPrivateIPsForAllowedHosts\":true}");
    ASSERT_NULL(strstr(blob, "\"requestCtx\""));
    ASSERT_NULL(strstr(blob, "\"cacheKeyTemplate\""));
    ASSERT_NULL(strstr(blob, "\"includeErrorMarker\""));
}

/* requestCtx is only meaningful next to a cache key template: without a
 * configured template the core uses URL-only keys, so the context must not
 * be rendered (mirrors ParseWithConfigCtx's contract). */
TEST(parse_json_blob_request_ctx_needs_template) {
    mesi_parse_json_config cfg = {
        .depth = 5,
        .base_url = "http://127.0.0.1/",
        .allowed_hosts = "backend",
        .block_private_ips = 0,
        .allow_private_for_allowed = 0,
        .request_ctx_json = "{\"ctx\":{\"header:Accept-Language\":\"pl\"}}",
        .timeout_seconds = -1,
        .max_response_size = -1,
        .max_concurrent_requests = -1,
        .max_workers = -1,
        .debug = -1,
        .parse_on_header = -1,
        .include_error_marker = NULL,
        .cache_key_template = "",
    };
    const char *blob = mesi_build_parse_json_config(pool, &cfg);
    ASSERT_NULL(strstr(blob, "\"requestCtx\""));
    ASSERT_NULL(strstr(blob, "\"cacheKeyTemplate\""));
}

/* A template without a resolved context still renders the template (the
 * core then fails the request if a placeholder cannot be resolved) but
 * omits requestCtx. */
TEST(parse_json_blob_template_without_ctx) {
    mesi_parse_json_config cfg = {
        .depth = 5,
        .base_url = "http://127.0.0.1/",
        .allowed_hosts = "backend",
        .block_private_ips = 0,
        .allow_private_for_allowed = 0,
        .request_ctx_json = "",
        .timeout_seconds = -1,
        .max_response_size = -1,
        .max_concurrent_requests = -1,
        .max_workers = -1,
        .debug = -1,
        .parse_on_header = -1,
        .include_error_marker = NULL,
        .cache_key_template = "{header:Accept-Language}",
    };
    const char *blob = mesi_build_parse_json_config(pool, &cfg);
    ASSERT_STR_CONTAINS(blob, "\"cacheKeyTemplate\":\"{header:Accept-Language}\"");
    ASSERT_NULL(strstr(blob, "\"requestCtx\""));
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
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.timeout_seconds, -1);
    ASSERT_EQ(MESI_DEFAULT_TIMEOUT_SECONDS, 30);
}

TEST(timeout_ten_accepted) {
    /* Issue example: MesiTimeout 10. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_timeout(&conf, pool, "10"));
    ASSERT_EQ(conf.timeout_seconds, 10);
}

TEST(timeout_min_accepted) {
    /* Boundary: 1 is the smallest legal timeout. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_timeout(&conf, pool, "1"));
    ASSERT_EQ(conf.timeout_seconds, 1);
}

TEST(timeout_max_accepted) {
    /* Boundary: 86400 (24h) is the configured max. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_timeout(&conf, pool, "86400"));
    ASSERT_EQ(conf.timeout_seconds, MESI_MAX_TIMEOUT_SECONDS);
}

TEST(timeout_max_plus_one_rejected) {
    /* Boundary: 86401 must be rejected. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_timeout(&conf, pool, "86401");
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
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_timeout(&conf, pool, "0");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_negative_rejected) {
    /* AC: MesiTimeout -1 is an error (atoi would silently coerce it). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_timeout(&conf, pool, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_alpha_rejected) {
    /* atoi("abc") → 0 would silently mean... a broken config. Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_timeout(&conf, pool, "abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_trailing_garbage_rejected) {
    /* atoi("3foo") → 3 would silently accept. Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_timeout(&conf, pool, "3foo");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_decimal_rejected) {
    /* Decimals must fail-fast — atoi("2.5") would truncate to 2. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_timeout(&conf, pool, "2.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_empty_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_timeout(&conf, pool, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiTimeout");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(timeout_oversize_rejected) {
    /* 12345678901 (11 digits) — overflow guard fires before the range
     * check; 999999999 (9 digits) passes the digit-count guard but is
     * caught by the [1, 86400] range. Both must fail. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_timeout(&conf, pool, "12345678901");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "exceeds");
    ASSERT_EQ(conf.timeout_seconds, -1);

    err = mesi_set_timeout(&conf, pool, "999999999");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "out of range");
    ASSERT_EQ(conf.timeout_seconds, -1);
}

TEST(merge_timeout_child_overrides) {
    /* Child 30 / parent 10 → 30 (add overrides base). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.timeout_seconds = 10;
    add.timeout_seconds = 30;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.timeout_seconds, 30);
}

TEST(merge_timeout_child_inherits) {
    /* Base 10 + unset add → 10 (unset child inherits the parent). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.timeout_seconds = 10;
    add.timeout_seconds = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.timeout_seconds, 10);
}

TEST(merge_timeout_both_unset) {
    /* Both unset → -1 sentinel → legacy path; LIBGOMESI applies the
     * 30s default Go-side. */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.timeout_seconds, -1);
}

/* --- MesiMaxBodySize / MesiOnOversize --- */

TEST(mbs_default_unset) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.max_body_size, (apr_off_t)-1);
    ASSERT_EQ(conf.on_oversize, -1);
}

TEST(mbs_valid_and_zero) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_body_size(&conf, pool, "1048576"));
    ASSERT_EQ(conf.max_body_size, (apr_off_t)1048576);
    ASSERT_NULL(mesi_set_max_body_size(&conf, pool, "0"));
    ASSERT_EQ(conf.max_body_size, (apr_off_t)0);
}

TEST(mbs_invalid_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_body_size(&conf, pool, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxBodySize");
    ASSERT_NOT_NULL(mesi_set_max_body_size(&conf, pool, "10abc"));
    ASSERT_NOT_NULL(mesi_set_max_body_size(&conf, pool, ""));
    ASSERT_EQ(conf.max_body_size, (apr_off_t)-1);
}

TEST(oversize_modes) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_on_oversize(&conf, pool, "error"));
    ASSERT_EQ(conf.on_oversize, 1);
    ASSERT_NULL(mesi_set_on_oversize(&conf, pool, "PASS"));
    ASSERT_EQ(conf.on_oversize, 0);
    const char *err = mesi_set_on_oversize(&conf, pool, "drop");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiOnOversize");
    ASSERT_EQ(conf.on_oversize, 0);
}

TEST(merge_mbs_child_zero_overrides) {
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_body_size = 1000;
    base.on_oversize = 1;
    add.max_body_size = 0;
    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_body_size, (apr_off_t)0);
    ASSERT_EQ(merged.on_oversize, 1);
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
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_zero_accepted) {
    /* AC: MesiMaxResponseSize 0 = unlimited. Unlike MesiTimeout, 0 is
     * a legitimate configured value (mesi/fetch.go only limits when
     * MaxResponseSize > 0), so it must parse AND be storable — the
     * -1 sentinel is what keeps it distinct from "unset". */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_response_size(&conf, pool, "0"));
    ASSERT_EQ(conf.max_response_size, (apr_off_t)0);
}

TEST(mrs_small_accepted) {
    /* AC: MesiMaxResponseSize 100 — a 200-byte include must be
     * rejected at fetch time (functional test on vhost 8089). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_response_size(&conf, pool, "100"));
    ASSERT_EQ(conf.max_response_size, (apr_off_t)100);
}

TEST(mrs_one_mb_accepted) {
    /* AC: MesiMaxResponseSize 1048576 — a 500 KB include must
     * succeed (functional test on vhost 8090). Issue example. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_response_size(&conf, pool, "1048576"));
    ASSERT_EQ(conf.max_response_size, (apr_off_t)1048576);
}

TEST(mrs_max_accepted) {
    /* Boundary: 9223372036854775806 (math.MaxInt64 - 1) is the
     * configured max — the largest value for which the core's
     * MaxResponseSize+1 LimitReader bound (mesi/fetch.go) stays
     * positive. Needs the full 19-digit range parse_nonneg_int's
     * 9-digit guard cannot express. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_response_size(&conf, pool, "9223372036854775806"));
    ASSERT_EQ(conf.max_response_size, MESI_MAX_MAX_RESPONSE_SIZE);
    ASSERT_EQ(MESI_MAX_MAX_RESPONSE_SIZE, (apr_off_t)9223372036854775806LL);
}

TEST(mrs_max_plus_one_rejected) {
    /* Boundary: 9223372036854775807 (math.MaxInt64) — at this value
     * the core's MaxResponseSize+1 wraps negative and the include
     * used to silently render an EMPTY body (the core rejects it
     * itself since #448). Rejected at config load. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_response_size(&conf, pool, "9223372036854775807");
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
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_response_size(&conf, pool, "-1");
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
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_response_size(&conf, pool, "+100");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_alpha_rejected) {
    /* atoi("abc") → 0 would silently mean "unlimited". Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_response_size(&conf, pool, "abc");
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
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_response_size(&conf, pool, "100abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_decimal_rejected) {
    /* Decimals must fail-fast — a truncating parse would silently
     * accept "1024.5" as 1024. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_response_size(&conf, pool, "2.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(mrs_empty_rejected) {
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_response_size(&conf, pool, "");
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
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_response_size(&conf, pool, "9999999999999999999");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxResponseSize");
    ASSERT_STR_CONTAINS(err, "exceeds maximum");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);

    err = mesi_set_max_response_size(&conf, pool, "18446744073709551616");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "exceeds maximum");
    ASSERT_EQ(conf.max_response_size, (apr_off_t)-1);
}

TEST(merge_mrs_child_overrides) {
    /* Child 1 MB / parent 100 → 1 MB (add overrides base). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_response_size = 100;
    add.max_response_size = 1048576;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_response_size, (apr_off_t)1048576);
}

TEST(merge_mrs_child_inherits) {
    /* Base 1 MB + unset add → 1 MB (unset child inherits the parent). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_response_size = 1048576;
    add.max_response_size = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_response_size, (apr_off_t)1048576);
}

TEST(merge_mrs_child_zero_overrides) {
    /* Explicit 0 ("unlimited") must win over a parent limit — 0 is
     * configured, not unset. This is the whole reason the sentinel
     * is -1 and not 0 (as a `>= 0` merge would silently inherit the
     * parent's limit and the operator's "unlimited" would be lost). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_response_size = 1048576;
    add.max_response_size = 0;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_response_size, (apr_off_t)0);
}

TEST(merge_mrs_both_unset) {
    /* Both unset → -1 sentinel → legacy path; LIBGOMESI leaves 0
     * (unlimited) — byte-identical to pre-#169 behaviour. */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    mesi_merge_configs(&base, &add, &merged);
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
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_zero_accepted) {
    /* AC: MesiMaxConcurrentRequests 0 = unlimited. Unlike
     * MesiTimeout, 0 is a legitimate configured value (mesi/parser.go
     * only installs the semaphore when > 0), so it must parse AND be
     * storable — the -1 sentinel is what keeps it distinct from
     * "unset" so it can override an inherited global cap. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_concurrent_requests(&conf, pool, "0"));
    ASSERT_EQ(conf.max_concurrent_requests, 0);
}

TEST(mcr_typical_accepted) {
    /* AC: MesiMaxConcurrentRequests 5 (the issue's example) — and the
     * functional cap-3 variant runs on vhost 8092. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_concurrent_requests(&conf, pool, "5"));
    ASSERT_EQ(conf.max_concurrent_requests, 5);
}

TEST(mcr_cap_accepted) {
    /* Boundary: 999999999 (MESI_MAX_MAX_CONCURRENT_REQUESTS) is the
     * configured max — the 9-digit int32-safe bound shared with
     * libgomesi config.MaxMaxConcurrentRequests (the core does not cap
     * the value, but Caddy validates the same bound, #452; this is the
     * transport's portable range). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_concurrent_requests(&conf, pool, "999999999"));
    ASSERT_EQ(conf.max_concurrent_requests, MESI_MAX_MAX_CONCURRENT_REQUESTS);
    ASSERT_EQ(MESI_MAX_MAX_CONCURRENT_REQUESTS, 999999999);
}

TEST(mcr_cap_plus_one_rejected) {
    /* Boundary: 1000000000 (10 digits) trips parse_nonneg_int's
     * 9-digit overflow guard before any intermediate wraps. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_concurrent_requests(&conf, pool, "1000000000");
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
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_concurrent_requests(&conf, pool, "-1");
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
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_concurrent_requests(&conf, pool, "+3");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_alpha_rejected) {
    /* atoi("abc") → 0 would silently mean "unlimited". Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_concurrent_requests(&conf, pool, "abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_trailing_garbage_rejected) {
    /* atoi("3foo") → 3 would silently accept. Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_concurrent_requests(&conf, pool, "3foo");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_decimal_rejected) {
    /* Decimals must fail-fast — atoi("2.5") would truncate to 2. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_concurrent_requests(&conf, pool, "2.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_empty_rejected) {
    /* atoi("") → 0 would silently mean "unlimited". Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_concurrent_requests(&conf, pool, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(mcr_oversize_rejected) {
    /* 12345678901 (11 digits) — the digit-count guard fires before
     * the range check. Must fail with an error naming the directive. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_concurrent_requests(&conf, pool, "12345678901");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxConcurrentRequests");
    ASSERT_STR_CONTAINS(err, "exceeds");
    ASSERT_EQ(conf.max_concurrent_requests, -1);
}

TEST(merge_mcr_child_overrides) {
    /* Child 5 / parent 3 → 5 (add overrides base). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_concurrent_requests = 3;
    add.max_concurrent_requests = 5;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_concurrent_requests, 5);
}

TEST(merge_mcr_child_inherits) {
    /* Base 3 + unset add → 3 (unset child inherits the parent). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_concurrent_requests = 3;
    add.max_concurrent_requests = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_concurrent_requests, 3);
}

TEST(merge_mcr_child_zero_overrides) {
    /* Explicit 0 ("unlimited") must win over a parent cap — 0 is
     * configured, not unset. This is the whole reason the sentinel
     * is -1 and not 0 (a `>= 0` merge would silently inherit the
     * parent's cap and the operator's "unlimited" would be lost). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_concurrent_requests = 3;
    add.max_concurrent_requests = 0;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_concurrent_requests, 0);
}

TEST(merge_mcr_both_unset) {
    /* Both unset → -1 sentinel → legacy path; LIBGOMESI leaves 0
     * (unlimited) — byte-identical to pre-#170 behaviour. */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_concurrent_requests, -1);
}

/* --- MesiMaxWorkers directive tests (#171) --- */

TEST(mw_default_unset) {
    /* Fresh config: sentinel -1 (unset). The filter then stays on the
     * legacy parse path and LIBGOMESI leaves MaxWorkers at 0 — the
     * library default runtime.NumCPU()*4, the exact pre-#171 Apache
     * behaviour (mesi/parser.go substitutes it for any value <= 0). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(mw_zero_accepted) {
    /* AC: MesiMaxWorkers 0 = library default (NumCPU*4). Unlike
     * MesiTimeout, 0 is a legitimate configured value (mesi/parser.go
     * substitutes NumCPU*4 for <= 0), so it must parse AND be storable
     * — the -1 sentinel is what keeps it distinct from "unset" so it
     * can override an inherited global cap. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_workers(&conf, pool, "0"));
    ASSERT_EQ(conf.max_workers, 0);
}

TEST(mw_typical_accepted) {
    /* AC: MesiMaxWorkers 4 (the issue's unit-test value) — and the
     * functional pool-2 variant runs on vhost 8094. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_workers(&conf, pool, "4"));
    ASSERT_EQ(conf.max_workers, 4);
}

TEST(mw_cap_accepted) {
    /* Boundary: 999999999 (MESI_MAX_MAX_WORKERS) is the configured
     * max — the 9-digit int32-safe bound shared with libgomesi
     * config.MaxMaxWorkers (neither core nor Caddy caps the value;
     * this is the transport's portable range). */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    ASSERT_NULL(mesi_set_max_workers(&conf, pool, "999999999"));
    ASSERT_EQ(conf.max_workers, MESI_MAX_MAX_WORKERS);
    ASSERT_EQ(MESI_MAX_MAX_WORKERS, 999999999);
}

TEST(mw_cap_plus_one_rejected) {
    /* Boundary: 1000000000 (10 digits) trips parse_nonneg_int's
     * 9-digit overflow guard before any intermediate wraps. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_workers(&conf, pool, "1000000000");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxWorkers");
    ASSERT_STR_CONTAINS(err, "exceeds");
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(mw_negative_rejected) {
    /* AC: -1 is an error. The core would silently substitute
     * runtime.NumCPU()*4 for any value <= 0 with NO warning
     * (mesi/parser.go — there is no #329-style warn+normalize for
     * MaxWorkers) — a malformed explicit value must never pass as the
     * documented "library default". */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_workers(&conf, pool, "-1");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxWorkers");
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(mw_plus_sign_rejected) {
    /* "+4" is rejected — the strict digits-only parser (which skips
     * leading spaces/tabs, like parse_nonneg_int everywhere) requires
     * a plain decimal form; the issue's atoi sketch would accept the
     * sign. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_workers(&conf, pool, "+4");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxWorkers");
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(mw_alpha_rejected) {
    /* atoi("abc") → 0 would silently mean "library default". Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_workers(&conf, pool, "abc");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxWorkers");
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(mw_trailing_garbage_rejected) {
    /* atoi("4foo") → 4 would silently accept. Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_workers(&conf, pool, "4foo");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxWorkers");
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(mw_decimal_rejected) {
    /* Decimals must fail-fast — atoi("2.5") would truncate to 2. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_workers(&conf, pool, "2.5");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxWorkers");
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(mw_empty_rejected) {
    /* atoi("") → 0 would silently mean "library default". Reject. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_workers(&conf, pool, "");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxWorkers");
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(mw_oversize_rejected) {
    /* 12345678901 (11 digits) — the digit-count guard fires before
     * the range check. Must fail with an error naming the directive. */
    mesi_config conf;
    mesi_config_init(&conf, pool);
    const char *err = mesi_set_max_workers(&conf, pool, "12345678901");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "MesiMaxWorkers");
    ASSERT_STR_CONTAINS(err, "exceeds");
    ASSERT_EQ(conf.max_workers, -1);
}

TEST(merge_mw_child_overrides) {
    /* Child 8 / parent 4 → 8 (add overrides base). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_workers = 4;
    add.max_workers = 8;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_workers, 8);
}

TEST(merge_mw_child_inherits) {
    /* Base 4 + unset add → 4 (unset child inherits the parent). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_workers = 4;
    add.max_workers = -1;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_workers, 4);
}

TEST(merge_mw_child_zero_overrides) {
    /* Explicit 0 ("library default") must win over a parent cap — 0
     * is configured, not unset. This is the whole reason the sentinel
     * is -1 and not 0 (a `>= 0` merge would silently inherit the
     * parent's cap and the operator's "library default" would be
     * lost). */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);
    base.max_workers = 4;
    add.max_workers = 0;

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_workers, 0);
}

TEST(merge_mw_both_unset) {
    /* Both unset → -1 sentinel → legacy path; LIBGOMESI leaves 0
     * (library default NumCPU*4) — byte-identical to pre-#171
     * behaviour. */
    mesi_config base, add, merged;
    mesi_config_init(&base, pool);
    mesi_config_init(&add, pool);
    mesi_config_init(&merged, pool);

    mesi_merge_configs(&base, &add, &merged);
    ASSERT_EQ(merged.max_workers, -1);
}

TEST(filter_html_content_type) {
    ASSERT_EQ(is_html_content("text/html"), 1);
    ASSERT_EQ(is_html_content("text/html; charset=utf-8"), 1);
    ASSERT_EQ(is_html_content("TEXT/HTML"), 1);
    ASSERT_EQ(is_html_content("  text/html"), 1);
    ASSERT_EQ(is_html_content("text/plain"), 0);
    ASSERT_EQ(is_html_content("text/htmlx"), 0);
    ASSERT_EQ(is_html_content("application/xhtml+xml"), 0);
    ASSERT_EQ(is_html_content(NULL), 0);
}

TEST(filter_status_eligibility) {
    ASSERT_EQ(mesi_status_allows_esi(200), 1);
    ASSERT_EQ(mesi_status_allows_esi(206), 1);
    ASSERT_EQ(mesi_status_allows_esi(204), 0);
    ASSERT_EQ(mesi_status_allows_esi(301), 0);
    ASSERT_EQ(mesi_status_allows_esi(304), 0);
    ASSERT_EQ(mesi_status_allows_esi(400), 0);
    ASSERT_EQ(mesi_status_allows_esi(401), 0);
    ASSERT_EQ(mesi_status_allows_esi(500), 0);
}

TEST(filter_response_eligibility) {
    ASSERT_EQ(mesi_response_is_eligible("text/html", 200), 1);
    ASSERT_EQ(mesi_response_is_eligible("text/html; charset=utf-8", 200), 1);
    ASSERT_EQ(mesi_response_is_eligible("text/plain", 200), 0);
    ASSERT_EQ(mesi_response_is_eligible(NULL, 200), 0);
    ASSERT_EQ(mesi_response_is_eligible("text/html", 302), 0);
    ASSERT_EQ(mesi_response_is_eligible("text/html", 304), 0);
    ASSERT_EQ(mesi_response_is_eligible("text/html", 404), 0);
}

TEST(filter_insert_wanted) {
    /* Static HTML: the content type is known before the handler runs. */
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, "text/html", 0, 0, 0), 1);
    /* Static non-HTML skips the filter allocation entirely. */
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, "text/plain", 0, 0, 0), 0);
    /* Proxied request: an extension-derived type is only a guess. */
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, "text/plain", 0, 0, 1), 1);
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, "text/html", 0, 0, 1), 1);
    /* Dynamic handler: same reasoning. */
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, "text/plain", 0, 0, 1), 1);
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, NULL, 0, 0, 1), 1);
    /* Unknown type from an ordinary request also defers to the response. */
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, NULL, 0, 0, 0), 1);
    /* Errors, redirects and bodiless responses never get the filter. */
    ASSERT_EQ(mesi_insert_filter_wanted(1, 302, "text/html", 0, 0, 0), 0);
    ASSERT_EQ(mesi_insert_filter_wanted(1, 304, "text/html", 0, 0, 0), 0);
    ASSERT_EQ(mesi_insert_filter_wanted(1, 404, "text/html", 0, 0, 0), 0);
    /* HEAD has no body to process. */
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, "text/html", 1, 0, 0), 0);
    /* Subrequests keep the pre-#97 behaviour (post_read_request did not run
     * for them); a page is only processed once, by its main request. */
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, "text/html", 0, 1, 0), 0);
    ASSERT_EQ(mesi_insert_filter_wanted(1, 200, "text/html", 0, 1, 1), 0);
    /* EnableMesi Off. */
    ASSERT_EQ(mesi_insert_filter_wanted(0, 200, "text/html", 0, 0, 0), 0);
}

TEST(filter_surrogate_capability_absent) {
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, NULL), "mesi=\"ESI/1.0\"");
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, ""), "mesi=\"ESI/1.0\"");
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "   "), "mesi=\"ESI/1.0\"");
}

TEST(filter_surrogate_capability_append) {
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "upstream=\"Surrogate/1.0\""),
                  "upstream=\"Surrogate/1.0\", mesi=\"ESI/1.0\"");
    /* A trailing comma must not produce an empty capability set. */
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "upstream=\"Surrogate/1.0\","),
                  "upstream=\"Surrogate/1.0\", mesi=\"ESI/1.0\"");
    /* Other devices' capability sets are preserved verbatim. */
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "a=\"X/1.0\", b=\"Y/1.0\""),
                  "a=\"X/1.0\", b=\"Y/1.0\", mesi=\"ESI/1.0\"");
}

TEST(filter_surrogate_capability_no_duplicate) {
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "mesi=\"ESI/1.0\""),
                  "mesi=\"ESI/1.0\"");
    ASSERT_STR_EQ(
        mesi_surrogate_capability(pool, "a=\"X/1.0\", mesi=\"ESI/1.0\""),
        "a=\"X/1.0\", mesi=\"ESI/1.0\"");
    /* A different device token that starts with "mesi" is not ours. */
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "mesi2=\"X/1.0\""),
                  "mesi2=\"X/1.0\", mesi=\"ESI/1.0\"");
}

TEST(filter_surrogate_capability_quoted_comma) {
    /* A comma inside another token's quoted capability value must not split
     * the element, so this is not mistaken for our "mesi" set. */
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "other=\"a,mesi=x\""),
                  "other=\"a,mesi=x\", mesi=\"ESI/1.0\"");
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "other=\"a,b\""),
                  "other=\"a,b\", mesi=\"ESI/1.0\"");
    /* A real "mesi" set after a quoted comma is still detected. */
    ASSERT_STR_EQ(
        mesi_surrogate_capability(pool, "other=\"a,b\", mesi=\"ESI/1.0\""),
        "other=\"a,b\", mesi=\"ESI/1.0\"");
    /* Backslash-escaped quotes inside a quoted value are handled. */
    ASSERT_STR_EQ(mesi_surrogate_capability(pool, "other=\"a\\\",b\""),
                  "other=\"a\\\",b\", mesi=\"ESI/1.0\"");
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
    RUN_TEST(whitespace_only_ascii_control);
    RUN_TEST(whitespace_only_unicode);
    RUN_TEST(non_whitespace_runes_are_tokens);
    RUN_TEST(hostname_with_unicode_whitespace);
    RUN_TEST(quoted_hostnames_rejected);

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

    printf("\nTesting set_debug() (#173):\n");
    RUN_TEST(debug_default_unset);
    RUN_TEST(debug_on);
    RUN_TEST(debug_off);
    RUN_TEST(merge_debug_child_overrides);
    RUN_TEST(merge_debug_child_off_overrides);
    RUN_TEST(merge_debug_child_inherits);
    RUN_TEST(merge_debug_both_unset);

    printf("\nTesting MesiParseOnHeader + Edge-control matcher (#172):\n");
    RUN_TEST(parse_on_header_default_unset);
    RUN_TEST(parse_on_header_on);
    RUN_TEST(parse_on_header_off);
    RUN_TEST(merge_parse_on_header_child_overrides);
    RUN_TEST(merge_parse_on_header_child_off_overrides);
    RUN_TEST(merge_parse_on_header_child_inherits);
    RUN_TEST(merge_parse_on_header_both_unset);
    RUN_TEST(header_signals_esi_matches);
    RUN_TEST(header_signals_esi_case_insensitive);
    RUN_TEST(header_signals_esi_comma_list);
    RUN_TEST(header_signals_esi_no_match);
    RUN_TEST(header_signals_esi_null);

    printf("\nTesting set_cache_backend() (#174):\n");
    RUN_TEST(cache_backend_memory);
    RUN_TEST(cache_backend_empty_disables);
    RUN_TEST(cache_backend_quoted_accepted);
    RUN_TEST(cache_backend_missing_argument_rejected);
    RUN_TEST(cache_backend_trailing_argument_rejected);
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
    RUN_TEST(merge_cache_backend_child_clears);
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
    RUN_TEST(redis_addr_quoted_accepted);
    RUN_TEST(redis_addr_missing_argument_rejected);
    RUN_TEST(redis_addr_trailing_argument_rejected);
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
    RUN_TEST(redis_password_quoted_with_space_accepted);
    RUN_TEST(redis_password_missing_argument_rejected);
    RUN_TEST(redis_password_trailing_argument_rejected);
    RUN_TEST(redis_password_valid);
    RUN_TEST(redis_password_with_special_chars_accepted);
    RUN_TEST(redis_password_with_control_char_rejected);
    RUN_TEST(redis_password_null_rejected);

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
    RUN_TEST(merge_redis_addr_child_clears);
    RUN_TEST(merge_redis_db_child_overrides);
    RUN_TEST(merge_redis_db_child_inherits);
    RUN_TEST(merge_redis_password_child_overrides);
    RUN_TEST(merge_redis_password_child_inherits);
    RUN_TEST(merge_redis_password_child_clears);

    printf("\nTesting set_cache_memcached_servers() (#176):\n");
    RUN_TEST(memcached_servers_default_unset);
    RUN_TEST(memcached_servers_single);
    RUN_TEST(memcached_servers_multiple);
    RUN_TEST(memcached_servers_mixed_whitespace);
    RUN_TEST(memcached_servers_ascii_whitespace_set);
    RUN_TEST(memcached_servers_unicode_whitespace_separators);
    RUN_TEST(memcached_servers_unicode_whitespace_only_rejected);
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
    RUN_TEST(memcached_servers_single_quote_rejected);
    RUN_TEST(memcached_servers_double_quote_rejected);
    RUN_TEST(memcached_servers_quoted_entry_rejected);
    RUN_TEST(memcached_servers_quote_in_later_entry_rejected);
    RUN_TEST(memcached_servers_unquoted_still_accepted);
    RUN_TEST(memcached_servers_backslash_rejected);
    RUN_TEST(memcached_servers_control_char_rejected);
    RUN_TEST(memcached_servers_nbsp_inside_entry_rejected);
    RUN_TEST(memcached_servers_invalid_utf8_is_not_a_separator);
    RUN_TEST(memcached_servers_del_rejected);
    RUN_TEST(memcached_servers_del_in_later_entry_rejected);
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

    printf("\nTesting MesiIncludeErrorMarker (#165):\n");
    RUN_TEST(include_error_marker_default_unset);
    RUN_TEST(include_error_marker_set_unquoted);
    RUN_TEST(include_error_marker_set_quoted_spaces);
    RUN_TEST(include_error_marker_empty_stored);
    RUN_TEST(include_error_marker_empty_single_quotes);
    RUN_TEST(include_error_marker_missing_rejected);
    RUN_TEST(include_error_marker_trailing_rejected);
    RUN_TEST(merge_include_error_marker_child_overrides);
    RUN_TEST(merge_include_error_marker_child_inherits);
    RUN_TEST(merge_include_error_marker_child_empty_clears);
    RUN_TEST(include_error_marker_json_escapes);

    printf("\nTesting ParseJson blob assembly (#165/#167-#173):\n");
    RUN_TEST(parse_json_blob_all_optional_keys);
    RUN_TEST(parse_json_blob_minimal);
    RUN_TEST(parse_json_blob_request_ctx_needs_template);
    RUN_TEST(parse_json_blob_template_without_ctx);

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

    printf("\nTesting set_max_workers() (#171):\n");
    RUN_TEST(mw_default_unset);
    RUN_TEST(mw_zero_accepted);
    RUN_TEST(mw_typical_accepted);
    RUN_TEST(mw_cap_accepted);
    RUN_TEST(mw_cap_plus_one_rejected);
    RUN_TEST(mw_negative_rejected);
    RUN_TEST(mw_plus_sign_rejected);
    RUN_TEST(mw_alpha_rejected);
    RUN_TEST(mw_trailing_garbage_rejected);
    RUN_TEST(mw_decimal_rejected);
    RUN_TEST(mw_empty_rejected);
    RUN_TEST(mw_oversize_rejected);

    printf("\nTesting merge_server_config() max_workers fields (#171):\n");
    RUN_TEST(merge_mw_child_overrides);
    RUN_TEST(merge_mw_child_inherits);
    RUN_TEST(merge_mw_child_zero_overrides);
    RUN_TEST(merge_mw_both_unset);

    printf("\nTesting MesiMaxBodySize / MesiOnOversize:\n");
    RUN_TEST(mbs_default_unset);
    RUN_TEST(mbs_valid_and_zero);
    RUN_TEST(mbs_invalid_rejected);
    RUN_TEST(oversize_modes);
    RUN_TEST(merge_mbs_child_zero_overrides);

    printf("\nTesting response filter lifecycle (#97):\n");
    RUN_TEST(filter_html_content_type);
    RUN_TEST(filter_status_eligibility);
    RUN_TEST(filter_response_eligibility);
    RUN_TEST(filter_insert_wanted);
    RUN_TEST(filter_surrogate_capability_absent);
    RUN_TEST(filter_surrogate_capability_append);
    RUN_TEST(filter_surrogate_capability_no_duplicate);
    RUN_TEST(filter_surrogate_capability_quoted_comma);

    apr_pool_destroy(pool);

    apr_terminate();

    printf("\n=== Results: %d passed, %d failed ===\n", tests_passed, tests_failed);

    return tests_failed > 0 ? 1 : 0;
}
