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

    printf("\nResults: %d failed\n", failed);
    return failed > 0 ? 1 : 0;
}
