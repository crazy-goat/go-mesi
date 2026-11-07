/*
 * Unit tests for the pure helpers in mesi_pure.h.
 *
 * These are the helpers that mod_mesi.c uses verbatim (see the include in
 * mod_mesi.c), not copies, so a regression in the filter's media-type or
 * base-URL logic fails here without Docker or a running Apache.
 *
 * Compile: gcc -o test_pure test_pure.c -I/usr/include/apr-1.0 -lapr-1
 * Run: ./test_pure
 */

#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <apr_general.h>
#include <apr_pools.h>

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

/* --- is_html_content ---------------------------------------------------- */

TEST(html_exact) {
    ASSERT_EQ(is_html_content("text/html"), 1);
}

TEST(html_case_insensitive) {
    ASSERT_EQ(is_html_content("TEXT/HTML"), 1);
    ASSERT_EQ(is_html_content("Text/Html"), 1);
    ASSERT_EQ(is_html_content("tExT/hTmL"), 1);
}

TEST(html_with_charset) {
    ASSERT_EQ(is_html_content("text/html; charset=utf-8"), 1);
    ASSERT_EQ(is_html_content("text/html;charset=UTF-8"), 1);
    ASSERT_EQ(is_html_content("TEXT/HTML; CHARSET=ISO-8859-1"), 1);
    ASSERT_EQ(is_html_content("text/html;"), 1);
}

TEST(html_leading_ows) {
    ASSERT_EQ(is_html_content(" text/html"), 1);
    ASSERT_EQ(is_html_content("\ttext/html"), 1);
    ASSERT_EQ(is_html_content(" \t text/html; charset=utf-8"), 1);
}

TEST(html_trailing_ows) {
    ASSERT_EQ(is_html_content("text/html "), 1);
    ASSERT_EQ(is_html_content("text/html\t"), 1);
    ASSERT_EQ(is_html_content("text/html\r\n"), 1);
}

TEST(html_suffix_is_not_html) {
    /* The exact bug this matcher exists to prevent: a prefix match would
     * treat "text/htmlabc" as HTML and run the ESI parser on it. */
    ASSERT_EQ(is_html_content("text/htmlabc"), 0);
    ASSERT_EQ(is_html_content("text/htmlx"), 0);
    ASSERT_EQ(is_html_content("text/html5"), 0);
}

TEST(html_prefix_is_not_html) {
    ASSERT_EQ(is_html_content("text/htm"), 0);
    ASSERT_EQ(is_html_content("text/"), 0);
    ASSERT_EQ(is_html_content("text"), 0);
}

TEST(html_other_media_types) {
    ASSERT_EQ(is_html_content("text/plain"), 0);
    ASSERT_EQ(is_html_content("application/xhtml+xml"), 0);
    ASSERT_EQ(is_html_content("application/json"), 0);
    ASSERT_EQ(is_html_content("image/png"), 0);
    ASSERT_EQ(is_html_content(""), 0);
}

TEST(html_null) {
    ASSERT_EQ(is_html_content(NULL), 0);
}

TEST(html_crlf_injection_after_media_type) {
    /* Header splitting is Apache's job; the matcher only needs to reject a
     * media type that continues past the delimiter. */
    ASSERT_EQ(is_html_content("text/html\r\nX-Other: 1"), 1);
    ASSERT_EQ(is_html_content("text/html, text/plain"), 0);
}

/* --- mesi_base_url_host ------------------------------------------------- */

TEST(base_host_canonical_wins_over_host_header) {
    /* server_hostname (ServerName) must win over the request-derived value
     * (ap_get_server_name, which may echo the client Host header). */
    ASSERT_STR_EQ(mesi_base_url_host("canonical.example", "attacker.example"),
                  "canonical.example");
}

TEST(base_host_falls_back_when_server_hostname_null) {
    ASSERT_STR_EQ(mesi_base_url_host(NULL, "request.example"),
                  "request.example");
}

TEST(base_host_empty_server_hostname_becomes_localhost) {
    /* Preserves the pre-extraction behaviour: an empty server_hostname does
     * not fall through to the request-derived name. */
    ASSERT_STR_EQ(mesi_base_url_host("", "request.example"), "localhost");
}

TEST(base_host_empty_server_name_becomes_localhost) {
    ASSERT_STR_EQ(mesi_base_url_host(NULL, ""), "localhost");
}

TEST(base_host_both_null_becomes_localhost) {
    ASSERT_STR_EQ(mesi_base_url_host(NULL, NULL), "localhost");
}

/* --- mesi_build_base_url ------------------------------------------------ */

TEST(base_url_http_default_port) {
    ASSERT_STR_EQ(mesi_build_base_url("http", "example.com", 80, pool),
                  "http://example.com/");
}

TEST(base_url_http_custom_port) {
    ASSERT_STR_EQ(mesi_build_base_url("http", "example.com", 8080, pool),
                  "http://example.com:8080/");
}

TEST(base_url_https_default_port) {
    ASSERT_STR_EQ(mesi_build_base_url("https", "example.com", 443, pool),
                  "https://example.com/");
}

TEST(base_url_https_custom_port) {
    ASSERT_STR_EQ(mesi_build_base_url("https", "example.com", 8443, pool),
                  "https://example.com:8443/");
}

TEST(base_url_port_default_is_scheme_specific) {
    /* Port 443 is non-default for http, and 80 is non-default for https. */
    ASSERT_STR_EQ(mesi_build_base_url("http", "example.com", 443, pool),
                  "http://example.com:443/");
    ASSERT_STR_EQ(mesi_build_base_url("https", "example.com", 80, pool),
                  "https://example.com:80/");
}

TEST(base_url_unknown_scheme_uses_http_default) {
    /* Only an exact "https" changes the default port; this is the existing
     * behaviour and is kept by the extraction. */
    ASSERT_STR_EQ(mesi_build_base_url("ftp", "example.com", 80, pool),
                  "ftp://example.com/");
    ASSERT_STR_EQ(mesi_build_base_url("HTTPS", "example.com", 443, pool),
                  "HTTPS://example.com:443/");
}

TEST(base_url_ipv6_literal) {
    ASSERT_STR_EQ(mesi_build_base_url("http", "[::1]", 80, pool),
                  "http://[::1]/");
    ASSERT_STR_EQ(mesi_build_base_url("https", "[2001:db8::1]", 8443, pool),
                  "https://[2001:db8::1]:8443/");
}

TEST(base_url_unicode_host_verbatim) {
    ASSERT_STR_EQ(mesi_build_base_url("http", "ex\xc3\xa4mple.com", 80, pool),
                  "http://ex\xc3\xa4mple.com/");
}

TEST(base_url_localhost) {
    ASSERT_STR_EQ(mesi_build_base_url("http", "localhost", 80, pool),
                  "http://localhost/");
}

int main(void) {
    apr_initialize();
    apr_pool_create(&pool, NULL);

    printf("Testing is_html_content():\n");
    RUN_TEST(html_exact);
    RUN_TEST(html_case_insensitive);
    RUN_TEST(html_with_charset);
    RUN_TEST(html_leading_ows);
    RUN_TEST(html_trailing_ows);
    RUN_TEST(html_suffix_is_not_html);
    RUN_TEST(html_prefix_is_not_html);
    RUN_TEST(html_other_media_types);
    RUN_TEST(html_null);
    RUN_TEST(html_crlf_injection_after_media_type);

    printf("\nTesting mesi_base_url_host():\n");
    RUN_TEST(base_host_canonical_wins_over_host_header);
    RUN_TEST(base_host_falls_back_when_server_hostname_null);
    RUN_TEST(base_host_empty_server_hostname_becomes_localhost);
    RUN_TEST(base_host_empty_server_name_becomes_localhost);
    RUN_TEST(base_host_both_null_becomes_localhost);

    printf("\nTesting mesi_build_base_url():\n");
    RUN_TEST(base_url_http_default_port);
    RUN_TEST(base_url_http_custom_port);
    RUN_TEST(base_url_https_default_port);
    RUN_TEST(base_url_https_custom_port);
    RUN_TEST(base_url_port_default_is_scheme_specific);
    RUN_TEST(base_url_unknown_scheme_uses_http_default);
    RUN_TEST(base_url_ipv6_literal);
    RUN_TEST(base_url_unicode_host_verbatim);
    RUN_TEST(base_url_localhost);

    apr_pool_destroy(pool);
    apr_terminate();

    printf("\n=== Results: %d passed, %d failed ===\n", tests_passed,
           tests_failed);

    return tests_failed > 0 ? 1 : 0;
}
