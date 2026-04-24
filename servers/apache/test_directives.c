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
#define ASSERT_NOT_NULL(x) assert((x) != NULL)
#define ASSERT_NULL(x) assert((x) == NULL)

/* Mock types from mod_mesi.c */
typedef struct {
    int enable_mesi;
    apr_array_header_t *allowed_hosts;
    int block_private_ips;
} mesi_config;

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

static void merge_configs(mesi_config *base, mesi_config *add, mesi_config *merged) {
    merged->enable_mesi = (add->enable_mesi != 0) ? add->enable_mesi : base->enable_mesi;
    merged->allowed_hosts = (add->allowed_hosts->nelts > 0) ? add->allowed_hosts : base->allowed_hosts;
    merged->block_private_ips = (add->block_private_ips != -1) ? add->block_private_ips : base->block_private_ips;
}

/* Test cases */

TEST(single_hostname) {
    mesi_config conf;
    memset(&conf, 0, sizeof(conf));
    conf.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
    const char *err = parse_allowed_hosts(&conf, "trusted.example.com");
    
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 1);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "trusted.example.com");
}

TEST(multiple_hostnames_space) {
    mesi_config conf;
    memset(&conf, 0, sizeof(conf));
    conf.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
    const char *err = parse_allowed_hosts(&conf, "host1.com host2.com host3.com");
    
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 3);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "host1.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[1], "host2.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[2], "host3.com");
}

TEST(multiple_hostnames_tab) {
    mesi_config conf;
    memset(&conf, 0, sizeof(conf));
    conf.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
    const char *err = parse_allowed_hosts(&conf, "host1.com\thost2.com");
    
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 2);
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[0], "host1.com");
    ASSERT_STR_EQ(((const char **)conf.allowed_hosts->elts)[1], "host2.com");
}

TEST(mixed_whitespace) {
    mesi_config conf;
    memset(&conf, 0, sizeof(conf));
    conf.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
    const char *err = parse_allowed_hosts(&conf, "host1.com  host2.com\t host3.com");
    
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 3);
}

TEST(empty_string) {
    mesi_config conf;
    memset(&conf, 0, sizeof(conf));
    conf.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
    const char *err = parse_allowed_hosts(&conf, "");
    
    ASSERT_NULL(err);
    ASSERT_EQ(conf.allowed_hosts->nelts, 0);
}

TEST(whitespace_only) {
    mesi_config conf;
    memset(&conf, 0, sizeof(conf));
    conf.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
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
    memset(&base, 0, sizeof(base));
    memset(&add, 0, sizeof(add));
    memset(&merged, 0, sizeof(merged));
    
    base.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    add.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    merged.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
    base.block_private_ips = 0;
    add.block_private_ips = 1;
    
    merge_configs(&base, &add, &merged);
    
    ASSERT_EQ(merged.block_private_ips, 1);
}

TEST(merge_child_inherits) {
    mesi_config base, add, merged;
    memset(&base, 0, sizeof(base));
    memset(&add, 0, sizeof(add));
    memset(&merged, 0, sizeof(merged));
    
    base.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    add.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    merged.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
    base.block_private_ips = 1;
    add.block_private_ips = -1;
    
    merge_configs(&base, &add, &merged);
    
    ASSERT_EQ(merged.block_private_ips, 1);
}

TEST(merge_allowed_hosts_child_set) {
    mesi_config base, add, merged;
    memset(&base, 0, sizeof(base));
    memset(&add, 0, sizeof(add));
    memset(&merged, 0, sizeof(merged));
    
    base.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    add.allowed_hosts = apr_array_make(pool, 4, sizeof(const char *));
    
    const char **h1 = apr_array_push(base.allowed_hosts);
    *h1 = apr_pstrdup(pool, "base.com");
    
    const char **h2 = apr_array_push(add.allowed_hosts);
    *h2 = apr_pstrdup(pool, "add.com");
    
    merge_configs(&base, &add, &merged);
    
    ASSERT_EQ(merged.allowed_hosts->nelts, 1);
    ASSERT_STR_EQ(((const char **)merged.allowed_hosts->elts)[0], "add.com");
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
    
    apr_pool_destroy(pool);
    apr_terminate();
    
    printf("\n=== Results: %d passed, %d failed ===\n", tests_passed, tests_failed);
    
    return tests_failed > 0 ? 1 : 0;
}
