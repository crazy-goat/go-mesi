/*
 * Response filter lifecycle predicates for mod_mesi.
 *
 * Lives in its own header (and not in mod_mesi.c) so the C unit tests in
 * test_directives.c exercise the real decisions instead of a copy: these
 * helpers are pure string/status logic and need no Apache request_rec.
 *
 * The lifecycle has two gates:
 *
 *   1. mesi_insert_filter_wanted() runs from ap_hook_insert_filter (inside
 *      ap_invoke_handler, after authentication and the fixups hook). It
 *      avoids attaching the output filter to requests that will never be
 *      processed and to static non-HTML responses.
 *   2. mesi_response_is_eligible() runs in the output filter on the first
 *      brigade, when the final Content-Type and status are known - which
 *      is the only point a proxied/dynamic response can be judged, because
 *      mod_proxy only learns the origin's headers inside its handler,
 *      after ap_hook_insert_filter has already run (see issue #97).
 */
#ifndef MOD_MESI_FILTER_LIFECYCLE_H
#define MOD_MESI_FILTER_LIFECYCLE_H

#include <string.h>
#include <strings.h>

#include <apr_pools.h>
#include <apr_strings.h>

/* The capability set this module appends to the request header. The Edge
 * Architecture Specification (W3C NOTE 2001-08-04, section 2.1) defines the
 * value as `device-token "=" <"> capability <">`, for example
 * `abc="ESI/1.0"`, not as a bare capability token. */
#define MESI_SURROGATE_CAPABILITY "mesi=\"ESI/1.0\""

/* mesi_is_html_content reports whether ct is text/html, ignoring case and
 * leading optional whitespace. A parameter ("; charset=...") is allowed;
 * another media type that merely starts with "text/html" is not. */
static int mesi_is_html_content(const char *ct) {
    if (!ct) {
        return 0;
    }
    /* Skip leading whitespace (OWS per RFC 7230 section 3.2.6). */
    while (*ct == ' ' || *ct == '\t') {
        ct++;
    }
    /* Case-insensitive media-type comparison (RFC 9110 section 8.3.1). */
    if (strncasecmp(ct, "text/html", 9) != 0) {
        return 0;
    }
    char delim = ct[9];
    /* Must be followed by a delimiter, parameter separator, or end. */
    return delim == '\0' || delim == ';' || delim == ' ' || delim == '\t' ||
           delim == '\r' || delim == '\n';
}

/* mesi_status_allows_esi: only a successful response with a body can be an
 * ESI document. Redirects (3xx), errors (4xx/5xx) and 204 No Content are
 * never parsed, and 304 Not Modified has no body. */
static int mesi_status_allows_esi(int status) {
    return status >= 200 && status < 300 && status != 204;
}

/* mesi_response_is_eligible is the final response-filter gate: the body is
 * an HTML 2xx document with content to process. */
static int mesi_response_is_eligible(const char *content_type, int status) {
    return mesi_status_allows_esi(status) &&
           mesi_is_html_content(content_type);
}

/* mesi_insert_filter_wanted decides whether to attach the output filter at
 * ap_hook_insert_filter time. A NULL content_type means the handler has not
 * produced it yet (mod_proxy, CGI, ...). More importantly, even a non-NULL
 * content_type can be only an extension-derived guess: mod_mime sets it from
 * r->filename before the handler runs, and for mod_proxy that filename is
 * "proxy:<origin-url>" (so /backend/report.txt is guessed text/plain even
 * when the origin serves text/html). A proxy or another non-default handler
 * therefore always gets the filter and the response filter applies the final
 * gate; only a known non-HTML static response is skipped. Subrequests keep
 * the pre-#97 behaviour (no filter) and HEAD has no body to process. */
static int mesi_insert_filter_wanted(int enable_mesi, int status,
                                     const char *content_type,
                                     int header_only, int is_subrequest,
                                     int dynamic_handler) {
    if (!enable_mesi || header_only || is_subrequest) {
        return 0;
    }
    if (!mesi_status_allows_esi(status)) {
        return 0;
    }
    if (dynamic_handler) {
        return 1;
    }
    return content_type == NULL || mesi_is_html_content(content_type);
}

/* mesi_surrogate_capability returns the request header value this module
 * sends to the origin. Existing capability sets are preserved and ours is
 * appended (the spec requires surrogates to append, not replace); a set for
 * the "mesi" device token is never duplicated. Elements are split on commas
 * outside quoted strings (with backslash escapes), so a quoted comma in
 * another token - for example other="a,mesi=x" - is not mistaken for our
 * capability set. Leading/trailing whitespace and trailing commas in the
 * existing value are not repeated. */
static const char *mesi_surrogate_capability(apr_pool_t *pool,
                                             const char *existing) {
    if (existing == NULL) {
        return MESI_SURROGATE_CAPABILITY;
    }

    apr_size_t len = strlen(existing);
    while (len > 0 && (existing[len - 1] == ' ' || existing[len - 1] == '\t' ||
                       existing[len - 1] == ',')) {
        len--;
    }
    if (len == 0) {
        return MESI_SURROGATE_CAPABILITY;
    }

    const char *end = existing + len;
    const char *p = existing;
    while (p < end) {
        while (p < end && (*p == ' ' || *p == '\t')) {
            p++;
        }
        const char *elem = p;
        int quoted = 0;
        int escaped = 0;
        while (p < end) {
            if (escaped) {
                escaped = 0;
            } else if (quoted) {
                if (*p == '\\') {
                    escaped = 1;
                } else if (*p == '"') {
                    quoted = 0;
                }
            } else if (*p == '"') {
                quoted = 1;
            } else if (*p == ',') {
                break;
            }
            p++;
        }
        const char *elem_end = p;
        while (elem_end > elem &&
               (elem_end[-1] == ' ' || elem_end[-1] == '\t')) {
            elem_end--;
        }
        if ((size_t)(elem_end - elem) >= sizeof("mesi") - 1 &&
            strncmp(elem, "mesi", sizeof("mesi") - 1) == 0) {
            const char *q = elem + sizeof("mesi") - 1;
            while (q < elem_end && (*q == ' ' || *q == '\t')) {
                q++;
            }
            if (q < elem_end && *q == '=') {
                return existing;
            }
        }
        if (p < end) {
            p++; /* Skip the separating comma. */
        }
    }

    return apr_pstrcat(pool, apr_pstrmemdup(pool, existing, len), ", ",
                       MESI_SURROGATE_CAPABILITY, NULL);
}

#endif /* MOD_MESI_FILTER_LIFECYCLE_H */
