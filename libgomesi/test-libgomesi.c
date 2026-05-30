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

    printf("\nResults: %d failed\n", failed);
    return failed > 0 ? 1 : 0;
}
