/*
 * Pure, Apache-glue-free helpers shared by mod_mesi.c and test_pure.c.
 *
 * These functions only allocate from the caller's APR pool, so the C unit
 * tests can exercise the real implementation instead of a copy. Keeping
 * them here adds no linker symbols: every function is file-local (static)
 * and mod_mesi.c includes this header exactly once.
 *
 * test_directives.c predates this header and still carries copies of the
 * directive parsers, whose Apache signatures require a cmd_parms.
 */
#ifndef MESI_PURE_H
#define MESI_PURE_H

#include <string.h>
#include <strings.h>

#include <apr_network_io.h>
#include <apr_pools.h>
#include <apr_strings.h>

/*
 * is_html_content reports whether a Content-Type value denotes text/html.
 *
 * Comparison is case-insensitive, leading OWS is ignored, and the media type
 * must be followed by end-of-string, a parameter separator (';') or OWS, so
 * "text/html; charset=utf-8" and "TEXT/HTML" match while "text/htmlabc" does
 * not. NULL and values that do not start with the media type return 0.
 */
static int is_html_content(const char *ct) {
    if (!ct) return 0;
    // Skip leading whitespace (OWS per RFC 7230 §3.2.6)
    while (*ct == ' ' || *ct == '\t') ct++;
    // Case-insensitive media-type comparison (RFC 9110 §8.3.1)
    if (strncasecmp(ct, "text/html", 9) != 0) return 0;
    char delim = ct[9];
    // Must be followed by delimiter, parameter separator, or end-of-string
    return delim == '\0' || delim == ';' || delim == ' ' || delim == '\t' ||
           delim == '\r' || delim == '\n';
}

/*
 * mesi_base_url_host chooses the host for the default ESI base URL.
 *
 * server_hostname is the configured canonical name (ServerName); server_name
 * is Apache's request-derived fallback (ap_get_server_name, which honours the
 * client-supplied Host header depending on UseCanonicalName). A non-NULL
 * server_hostname always wins; NULL falls back to server_name; an empty
 * result becomes "localhost". This preserves the pre-extraction semantics.
 */
static const char *mesi_base_url_host(const char *server_hostname,
                                      const char *server_name) {
    const char *host = server_hostname ? server_hostname : server_name;
    return (host && *host) ? host : "localhost";
}

/*
 * mesi_build_base_url renders "<scheme>://<host>[:port]/", omitting the port
 * when it is the scheme's default (443 for https, 80 otherwise). The host is
 * used verbatim, so an IPv6 literal must already be bracketed ("[::1]").
 */
static char *mesi_build_base_url(const char *scheme, const char *host,
                                 apr_port_t port, apr_pool_t *pool) {
    int default_port = (strcmp(scheme, "https") == 0) ? 443 : 80;

    if (port != default_port) {
        return apr_psprintf(pool, "%s://%s:%d/", scheme, host, port);
    }
    return apr_psprintf(pool, "%s://%s/", scheme, host);
}

#endif /* MESI_PURE_H */
