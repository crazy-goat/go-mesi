#!/usr/bin/env bash
#
# Unit tests for APR include directory discovery in Makefile.

set -uo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK_ROOT="$(mktemp -d -t go-mesi-apr.XXXXXX)"
trap 'rm -rf "$WORK_ROOT"' EXIT

mkdir -p "$WORK_ROOT/valid"
touch "$WORK_ROOT/valid/apr_general.h"

compile_command="$(
    make --no-print-directory -C "$TEST_DIR" \
        -n test-unit \
        APXS=missing-apxs \
        APR_CANDIDATES="$WORK_ROOT/missing $WORK_ROOT/valid"
)"

if ! grep -q -- "-I$WORK_ROOT/valid " <<<"$compile_command"; then
    printf 'FAIL: expected compile command to use -I%s\n%s\n' \
        "$WORK_ROOT/valid" "${compile_command:-<empty>}" >&2
    exit 1
fi

override_command="$(
    make --no-print-directory -C "$TEST_DIR" \
        -n test-unit \
        APR_INCLUDEDIR="$WORK_ROOT/override"
)"

if ! grep -q -- "-I$WORK_ROOT/override " <<<"$override_command"; then
    printf 'FAIL: explicit APR_INCLUDEDIR override was ignored\n%s\n' \
        "${override_command:-<empty>}" >&2
    exit 1
fi

printf 'PASS: APR_INCLUDEDIR probes candidates for apr_general.h\n'
