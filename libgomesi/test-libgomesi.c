#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <pthread.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include "libgomesi.h"

/* ---- minimal loopback fragment server (threaded) ---- */

typedef struct {
    int sock;
    int port;
    int remaining;
    pthread_t tid;
} frag_server_t;

static void *frag_server_loop(void *arg) {
    frag_server_t *s = (frag_server_t *)arg;
    struct timeval tv;
    tv.tv_sec = 8; /* no request in 8s (blocked include) -> give up */
    tv.tv_usec = 0;
    setsockopt(s->sock, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
    while (s->remaining > 0) {
        int c = accept(s->sock, NULL, NULL);
        if (c < 0) break; /* timeout: the include never dialed */
        char buf[2048];
        (void)recv(c, buf, sizeof(buf), 0); /* consume the request */
        const char *resp =
            "HTTP/1.0 200 OK\r\n"
            "Content-Type: text/plain\r\n"
            "Content-Length: 11\r\n"
            "Connection: close\r\n"
            "\r\n"
            "FRAGMENT-OK";
        size_t total = strlen(resp), sent = 0;
        while (sent < total) {
            ssize_t n = send(c, resp + sent, total - sent, 0);
            if (n <= 0) break;
            sent += (size_t)n;
        }
        close(c);
        s->remaining--;
    }
    return NULL;
}

static int frag_server_start(frag_server_t *s, int max_requests) {
    s->sock = socket(AF_INET, SOCK_STREAM, 0);
    if (s->sock < 0) return -1;
    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    addr.sin_port = 0; /* ephemeral */
    if (bind(s->sock, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
        close(s->sock);
        return -1;
    }
    if (listen(s->sock, 4) != 0) {
        close(s->sock);
        return -1;
    }
    socklen_t len = sizeof(addr);
    if (getsockname(s->sock, (struct sockaddr *)&addr, &len) != 0) {
        close(s->sock);
        return -1;
    }
    s->port = ntohs(addr.sin_port);
    s->remaining = max_requests;
    if (pthread_create(&s->tid, NULL, frag_server_loop, s) != 0) {
        close(s->sock);
        return -1;
    }
    return 0;
}

static void frag_server_stop(frag_server_t *s) {
    /* NOTE: close() alone does NOT reliably wake a blocked accept() on all
     * platforms (Linux > 5.14 and macOS accept() also honor SO_RCVTIMEO,
     * set in frag_server_start — that bounded timeout is the real guarantee
     * that the join cannot hang when the include was blocked and no request
     * ever arrived). */
    close(s->sock);
    pthread_join(s->tid, NULL);
}

int main(void) {
    int failed = 0;

    printf("Test 1: InitHTTPClient creates shared client\n");
    InitHTTPClient(1);
    printf("  PASS: InitHTTPClient returned without error\n");

    printf("Test 2: Parse with shared client\n");
    {
        char *result = ParseDefault("hello world");
        if (result == NULL) {
            printf("  FAIL: ParseDefault returned NULL\n");
            failed++;
        } else if (strcmp(result, "hello world") != 0) {
            printf("  FAIL: expected 'hello world', got '%s'\n", result);
            failed++;
        } else {
            printf("  PASS: ParseDefault returned correct result\n");
        }
        FreeString(result);
    }

    printf("Test 3: Parse with config uses shared client\n");
    {
        char *result = ParseWithConfig("test input", 5, "http://localhost/", "", 0);
        if (result == NULL) {
            printf("  FAIL: ParseWithConfig returned NULL\n");
            failed++;
        } else {
            printf("  PASS: ParseWithConfig returned result\n");
        }
        FreeString(result);
    }

    printf("Test 4: FreeHTTPClient is idempotent\n");
    FreeHTTPClient();
    FreeHTTPClient();
    printf("  PASS: Double FreeHTTPClient did not crash\n");

    printf("Test 5: Parse after FreeHTTPClient still works (no shared client)\n");
    {
        char *result = ParseDefault("after free");
        if (result == NULL) {
            printf("  FAIL: ParseDefault returned NULL\n");
            failed++;
        } else if (strcmp(result, "after free") != 0) {
            printf("  FAIL: expected 'after free', got '%s'\n", result);
            failed++;
        } else {
            printf("  PASS: ParseDefault works without shared client\n");
        }
        FreeString(result);
    }

    printf("Test 6: Re-init after free works\n");
    {
        InitHTTPClient(0);
        char *result = ParseDefault("reinit test");
        if (result == NULL) {
            printf("  FAIL: ParseDefault returned NULL after reinit\n");
            failed++;
        } else if (strcmp(result, "reinit test") != 0) {
            printf("  FAIL: expected 'reinit test', got '%s'\n", result);
            failed++;
        } else {
            printf("  PASS: Reinit after free works\n");
        }
        FreeString(result);
        FreeHTTPClient();
    }

    printf("Test 7: InitCache with unsupported backend returns -1\n");
    {
        int ret = InitCache("invalid", 100, 30);
        if (ret == -1) {
            printf("  PASS: InitCache returned -1 for unknown backend\n");
        } else {
            printf("  FAIL: expected -1, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 8: InitCache with memory backend returns 0\n");
    {
        int ret = InitCache("memory", 5000, 30);
        if (ret == 0) {
            printf("  PASS: InitCache returned 0 for memory backend\n");
        } else {
            printf("  FAIL: expected 0, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 9: Parse after InitCache still works\n");
    {
        char *result = ParseDefault("with cache");
        if (result == NULL) {
            printf("  FAIL: ParseDefault returned NULL\n");
            failed++;
        } else if (strcmp(result, "with cache") != 0) {
            printf("  FAIL: expected 'with cache', got '%s'\n", result);
            failed++;
        } else {
            printf("  PASS: ParseDefault works with cache initialized\n");
        }
        FreeString(result);
    }

    printf("Test 10: FreeCache is idempotent\n");
    FreeCache();
    FreeCache();
    printf("  PASS: Double FreeCache did not crash\n");

    printf("Test 11: InitCache with empty backend disables cache\n");
    {
        int ret = InitCache("", 100, 30);
        if (ret == 0) {
            printf("  PASS: InitCache returned 0 for empty backend\n");
        } else {
            printf("  FAIL: expected 0, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 12: InitCacheWithConfig memory backend returns 0\n");
    {
        // Memory backend with empty config must still succeed (config
        // is optional for memory). Verifies the new entry point
        // added in #175 doesn't break the legacy codepath.
        int ret = InitCacheWithConfig("memory", 5000, 30, "{}");
        if (ret == 0) {
            printf("  PASS: InitCacheWithConfig(memory, ..., {}) returned 0\n");
        } else {
            printf("  FAIL: expected 0, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 13: InitCacheWithConfig empty backend disables cache\n");
    {
        int ret = InitCacheWithConfig("", 100, 30, "");
        if (ret == 0) {
            printf("  PASS: InitCacheWithConfig('') returned 0\n");
        } else {
            printf("  FAIL: expected 0, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 14: InitCacheWithConfig redis with malformed JSON returns -1\n");
    {
        // Workflow rule: silent substitution is forbidden — malformed
        // config must surface as -1 so the caller logs the error.
        int ret = InitCacheWithConfig("redis", 100, 30, "not json");
        if (ret == -1) {
            printf("  PASS: malformed JSON returned -1\n");
        } else {
            printf("  FAIL: expected -1, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 15: InitCacheWithConfig memcached without servers returns -1\n");
    {
        int ret = InitCacheWithConfig("memcached", 100, 30, "{}");
        if (ret == -1) {
            printf("  PASS: memcached + no servers returned -1\n");
        } else {
            printf("  FAIL: expected -1, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 16: InitCacheWithConfig redis with valid config returns 0\n");
    {
        // Use an address unlikely to actually connect (port 1). Init
        // succeeds because the Redis client is lazy; no DIAL happens
        // at this point. Subsequent operations that fail to connect
        // are handled by the library, but Init itself must succeed.
        int ret = InitCacheWithConfig("redis", 100, 30,
            "{\"redisAddr\":\"127.0.0.1:1\",\"redisDB\":2}");
        if (ret == 0) {
            printf("  PASS: InitCacheWithConfig(redis, valid) returned 0\n");
        } else {
            printf("  FAIL: expected 0, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 17: InitCacheWithConfig memcached with valid servers returns 0\n");
    {
        // Memcached client is also lazy — no DIAL at init.
        int ret = InitCacheWithConfig("memcached", 100, 30,
            "{\"servers\":[\"127.0.0.1:11211\"]}");
        if (ret == 0) {
            printf("  PASS: InitCacheWithConfig(memcached, valid) returned 0\n");
        } else {
            printf("  FAIL: expected 0, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 18: InitCacheWithConfig unknown backend returns -1\n");
    {
        int ret = InitCacheWithConfig("file", 100, 30, "");
        if (ret == -1) {
            printf("  PASS: unknown backend returned -1\n");
        } else {
            printf("  FAIL: expected -1, got %d\n", ret);
            failed++;
        }
    }

    printf("Test 19: Parse after InitCacheWithConfig still works\n");
    {
        // Switch to memory backend so the global cache ptr is a working
        // memory cache. Then Parse must still work.
        InitCacheWithConfig("memory", 5000, 30, "{}");
        char *result = ParseDefault("post cache config");
        if (result == NULL) {
            printf("  FAIL: ParseDefault returned NULL\n");
            failed++;
        } else if (strcmp(result, "post cache config") != 0) {
            printf("  FAIL: expected 'post cache config', got '%s'\n", result);
            failed++;
        } else {
            printf("  PASS: Parse works after InitCacheWithConfig\n");
        }
        if (result) FreeString(result);
    }

    printf("Test 20: FreeCache after InitCacheWithConfig is idempotent\n");
    FreeCache();
    FreeCache();
    printf("  PASS: FreeCache did not crash\n");

    /* ---- #196: shared-client yield for allowPrivateIPsForAllowedHosts ----
     *
     * The PHP extension (and any C consumer) runs with the shared HTTP
     * client created by InitHTTPClient. Its transport bakes blockPrivateIPs
     * in at startup, and the core's fetchClientForURL serves shared-client
     * requests before ever reaching the per-host bypass branch. ParseWithConfigEx
     * with allowPrivateIPsForAllowedHosts=1 must therefore detach from the
     * shared client; otherwise the bypass is a silent no-op. Proved with a
     * loopback fragment server: loopback is a private/reserved address, so
     * with blockPrivateIPs=1 the include can only succeed through the bypass.
     */
    {
        printf("Test 21: bypass allowlist + loopback fragment (shared client active)\n");
        InitHTTPClient(1);
        printf("  PASS: InitHTTPClient(1) set up the blocking shared client\n");

        frag_server_t srv;
        if (frag_server_start(&srv, 5) != 0) {
            printf("  FAIL: could not start fragment server\n");
            failed++;
        } else {
            char src[256];
            snprintf(src, sizeof(src),
                     "<esi:include src=\"http://127.0.0.1:%d/frag\" />", srv.port);

            /* 21a: bypass=1 -> whitelisted private host dial through. */
            char *r = ParseWithConfigEx(src, 5, "http://127.0.0.1/",
                                        "127.0.0.1", 1, 1);
            if (r == NULL) {
                printf("  FAIL: ParseWithConfigEx returned NULL\n");
                failed++;
            } else if (strstr(r, "FRAGMENT-OK") == NULL) {
                printf("  FAIL: bypass=1 should fetch the loopback fragment, got: %s\n", r);
                failed++;
            } else {
                printf("  PASS: bypass=1 fetched the loopback fragment\n");
            }
            FreeString(r);

            /* 21b: bypass=0 -> private-IP dial stays blocked. */
            r = ParseWithConfigEx(src, 5, "http://127.0.0.1/",
                                  "127.0.0.1", 1, 0);
            if (r == NULL) {
                printf("  FAIL: ParseWithConfigEx returned NULL\n");
                failed++;
            } else if (strstr(r, "FRAGMENT-OK") != NULL) {
                printf("  FAIL: bypass=0 must block the private-IP dial, got: %s\n", r);
                failed++;
            } else {
                printf("  PASS: bypass=0 kept the private-IP dial blocked\n");
            }
            FreeString(r);

            /* 21c: bypass=1 but host NOT in the allowlist -> blocked pre-dial. */
            r = ParseWithConfigEx(src, 5, "http://127.0.0.1/",
                                  "example.com", 1, 1);
            if (r == NULL) {
                printf("  FAIL: ParseWithConfigEx returned NULL\n");
                failed++;
            } else if (strstr(r, "FRAGMENT-OK") != NULL) {
                printf("  FAIL: unlisted host must stay blocked, got: %s\n", r);
                failed++;
            } else {
                printf("  PASS: unlisted host blocked despite bypass=1\n");
            }
            FreeString(r);

            /* 21d: bypass=1 but empty allowlist -> no entry matches, so no
             * bypass is granted and the private dial stays blocked. */
            r = ParseWithConfigEx(src, 5, "http://127.0.0.1/",
                                  "", 1, 1);
            if (r == NULL) {
                printf("  FAIL: ParseWithConfigEx returned NULL\n");
                failed++;
            } else if (strstr(r, "FRAGMENT-OK") != NULL) {
                printf("  FAIL: empty allowlist must not grant the bypass, got: %s\n", r);
                failed++;
            } else {
                printf("  PASS: empty allowlist grants no bypass (blocked)\n");
            }
            FreeString(r);

            /* 21e: ordinary (non-bypass) parses still work after bypass
             * parses detached from the shared client (no-degradation smoke;
             * this does NOT prove connection-pooling reuse, which cannot be
             * observed from the C ABI). */
            r = ParseWithConfig("shared client intact", 5, "http://127.0.0.1/", "", 1);
            if (r == NULL) {
                printf("  FAIL: ParseWithConfig returned NULL\n");
                failed++;
            } else if (strcmp(r, "shared client intact") != 0) {
                printf("  FAIL: shared client path degraded, got: %s\n", r);
                failed++;
            } else {
                printf("  PASS: shared client intact for ordinary parses\n");
            }
            FreeString(r);

            frag_server_stop(&srv);
        }
        FreeHTTPClient();
    }

    printf("\nResults: %d failed\n", failed);
    return failed > 0 ? 1 : 0;
}
