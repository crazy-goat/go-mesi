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
    /* Cache backend (parity with mod_mesi.c mesi_config) */
    const char *cache_backend;
    int cache_size;
    int cache_ttl;
} mesi_config;

/* Sentinel/constant values copied from mod_mesi.c */
#define MESI_DEFAULT_CACHE_SIZE 10000
#define MESI_MAX_CACHE_SIZE 1000000
#define MESI_MAX_CACHE_TTL_SECONDS (24 * 60 * 60)

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

/* Parse helper copied verbatim from mod_mesi.c — verifies the
 * validator rejects malformed integers silently rather than falling
 * back to a default. */
static const char *parse_nonneg_int(apr_pool_t *pool_arg, const char *arg,
                                    int min, int max, int *out) {
    const char *p = arg ? arg : "";
    while (*p == ' ' || *p == '\t') p++;
    if (*p == '\0') {
        return apr_psprintf(pool_arg,
            "MesiCache* requires a non-negative integer argument");
    }
    const char *digits = p;
    while (*p >= '0' && *p <= '9') p++;
    if (*p != '\0') {
        return apr_psprintf(pool_arg,
            "MesiCache* must be a non-negative integer (got: %s)", arg);
    }
    if (digits == p) {
        return apr_psprintf(pool_arg,
            "MesiCache* must contain at least one digit (got: %s)", arg);
    }
    size_t n = (size_t)(p - digits);
    if (n > 9) {
        return apr_psprintf(pool_arg,
            "MesiCache* value %s exceeds maximum allowed (%d)", arg, max);
    }
    long val = 0;
    for (size_t i = 0; i < n; i++) {
        val = val * 10 + (digits[i] - '0');
    }
    if (val < min || val > max) {
        return apr_psprintf(pool_arg,
            "MesiCache* value %s out of range [%d, %d]", arg, min, max);
    }
    *out = (int)val;
    return NULL;
}

/* Set functions copied from mod_mesi.c — verify they wire directives
 * into mesi_config correctly and reject invalid values without
 * silent-default substitution. */
static const char *set_cache_backend(mesi_config *conf, const char *arg) {
    if (!arg) {
        return "MesiCacheBackend requires an argument (use empty string to disable)";
    }
    if (strcmp(arg, "memory") == 0) {
        conf->cache_backend = "memory";
        return NULL;
    }
    if (arg[0] == '\0') {
        conf->cache_backend = "";
        return NULL;
    }
    return apr_psprintf(pool,
        "MesiCacheBackend: unknown backend %s (only \"memory\" is supported)",
        arg);
}

static const char *set_cache_size(mesi_config *conf, const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, 1, MESI_MAX_CACHE_SIZE, &v);
    if (err) {
        return err;
    }
    conf->cache_size = v;
    return NULL;
}

static const char *set_cache_ttl(mesi_config *conf, const char *arg) {
    int v = 0;
    const char *err = parse_nonneg_int(pool, arg, 0, MESI_MAX_CACHE_TTL_SECONDS, &v);
    if (err) {
        return err;
    }
    conf->cache_ttl = v;
    return NULL;
}

static void init_config(mesi_config *conf) {
    memset(conf, 0, sizeof(*conf));
    conf->allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    conf->block_private_ips = -1;
    conf->cache_backend = "";
    conf->cache_size = 0;
    conf->cache_ttl = -1;
}

static void merge_configs(mesi_config *base, mesi_config *add, mesi_config *merged) {
    merged->enable_mesi = (add->enable_mesi != 0) ? add->enable_mesi : base->enable_mesi;
    merged->allowed_hosts = (add->allowed_hosts->nelts > 0) ? add->allowed_hosts : base->allowed_hosts;
    merged->block_private_ips = (add->block_private_ips != -1) ? add->block_private_ips : base->block_private_ips;
    merged->cache_backend = (add->cache_backend && add->cache_backend[0] != '\0')
                           ? add->cache_backend
                           : base->cache_backend;
    merged->cache_size = (add->cache_size > 0) ? add->cache_size : base->cache_size;
    merged->cache_ttl = (add->cache_ttl >= 0) ? add->cache_ttl : base->cache_ttl;
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

    const char *err = set_cache_backend(&conf, "redis");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "redis");
    ASSERT_STR_CONTAINS(err, "memory");
    /* config left at default "" instead of being polluted with bogus value */
    ASSERT_STR_EQ(conf.cache_backend, "");
}

TEST(cache_backend_redis_rejected) {
    /* redis/memcached are unsupported on Apache in this milestone. */
    mesi_config conf;
    init_config(&conf);

    const char *err = set_cache_backend(&conf, "memcached");
    ASSERT_NOT_NULL(err);
    ASSERT_STR_CONTAINS(err, "memcached");
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

    printf("\nTesting set_cache_backend() (#174):\n");
    RUN_TEST(cache_backend_memory);
    RUN_TEST(cache_backend_empty_disables);
    RUN_TEST(cache_backend_unknown_rejected);
    RUN_TEST(cache_backend_redis_rejected);

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

    apr_pool_destroy(pool);
    apr_terminate();

    printf("\n=== Results: %d passed, %d failed ===\n", tests_passed, tests_failed);

    return tests_failed > 0 ? 1 : 0;
}
