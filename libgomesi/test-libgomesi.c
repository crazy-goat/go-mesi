#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "libgomesi.h"

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

    printf("\nResults: %d failed\n", failed);
    return failed > 0 ? 1 : 0;
}
