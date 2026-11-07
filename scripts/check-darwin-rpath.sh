#!/usr/bin/env bash
# check-darwin-rpath.sh
#
# Verify the macOS install-name wiring for #443: libgomesi.so must carry
# "@rpath/libgomesi.so" as its LC_ID_DYLIB, and a consumer built against it
# (the PHP extension or libgomesi's C test binary) must record an @rpath
# dependency plus an LC_RPATH that actually resolves to the directory holding
# the library. dyld only consults LC_RPATH for @rpath/... references, so a bare
# "libgomesi.so" id makes php-ext/config.m4's LC_RPATH a no-op and the extension
# only loads with DYLD_LIBRARY_PATH set.
#
# Usage: scripts/check-darwin-rpath.sh [LIB] [CONSUMER]
#   LIB       default: libgomesi/libgomesi.so (run from the repo root)
#   CONSUMER  optional Mach-O binary/dylib that links against LIB, for example
#             php-ext/modules/mesi.so or libgomesi/test-libgomesi
#
# Skipped (exit 0) on non-Darwin hosts: otool and Mach-O libraries only exist
# on macOS. Run it in a macOS checkout after `make -C libgomesi build`.
# Used by: make check-darwin (root and libgomesi Makefiles).
#
# Covered by scripts/test-check-darwin-rpath.sh, which fakes uname/otool so the
# PASS/FAIL branches (including an unrelated LC_RPATH) run on Linux CI too.
set -euo pipefail

LIB=${1:-libgomesi/libgomesi.so}
CONSUMER=${2:-}
EXPECTED_ID='@rpath/libgomesi.so'

if [ "$(uname -s)" != "Darwin" ]; then
    echo "SKIP: not macOS (uname -s is $(uname -s)); Mach-O install-name checks need otool"
    exit 0
fi

if ! command -v otool >/dev/null 2>&1; then
    echo "FAIL: otool not found; install the Xcode command line tools" >&2
    exit 1
fi

if [ ! -f "$LIB" ]; then
    echo "FAIL: $LIB not found; run 'make -C libgomesi build' first" >&2
    exit 1
fi

# otool -D prints a "path:" header on the first line and LC_ID_DYLIB after it;
# strip indentation and take the last non-empty line so both forms work.
ACTUAL_ID=$(otool -D "$LIB" | sed 's/^[[:space:]]*//' | grep -v '^$' | tail -n 1)
if [ "$ACTUAL_ID" != "$EXPECTED_ID" ]; then
    echo "FAIL: $LIB install id is '$ACTUAL_ID', expected '$EXPECTED_ID'" >&2
    echo "      Build it with 'make -C libgomesi build' (macOS branch sets -install_name)." >&2
    exit 1
fi
echo "PASS: $LIB install id is $EXPECTED_ID"

# Print the physical path of an existing directory; fall back to the input
# without a trailing slash so a non-existent rpath can still be reported.
normalize_dir() {
    if [ -d "$1" ]; then
        (cd "$1" && pwd -P)
    else
        printf '%s\n' "${1%/}"
    fi
}

# One LC_RPATH command per entry, as written in the Mach-O:
#           cmd LC_RPATH
#       cmdsize 32
#          path /some/dir (offset 12)
extract_rpaths() {
    otool -l "$1" | awk '
        /^[[:space:]]*cmd / { want = ($0 ~ /LC_RPATH/); next }
        want && /^[[:space:]]*path / {
            sub(/^[[:space:]]*path /, "")
            sub(/[[:space:]]+\(offset [0-9]+\)[[:space:]]*$/, "")
            print
            want = 0
        }
    '
}

if [ -n "$CONSUMER" ]; then
    if [ ! -f "$CONSUMER" ]; then
        echo "FAIL: consumer $CONSUMER not found" >&2
        exit 1
    fi
    if ! otool -L "$CONSUMER" | grep -Fq "$EXPECTED_ID"; then
        echo "FAIL: $CONSUMER does not depend on $EXPECTED_ID (its link-time install id was not @rpath)" >&2
        echo "      Rebuild $CONSUMER against the fixed $LIB." >&2
        otool -L "$CONSUMER" >&2
        exit 1
    fi

    # LC_RPATH must resolve to the directory that holds the library. dyld
    # expands "@loader_path" relative to the directory of the Mach-O that
    # contains the LC_RPATH, so that form is accepted when it lands on $LIB's
    # directory (libgomesi's test binary uses it). Any other directory would
    # leave @rpath/libgomesi.so unresolvable.
    LIB_DIR=$(normalize_dir "$(dirname "$LIB")")
    CONSUMER_DIR=$(normalize_dir "$(dirname "$CONSUMER")")
    RPATHS=$(extract_rpaths "$CONSUMER")
    if [ -z "$RPATHS" ]; then
        echo "FAIL: $CONSUMER has no LC_RPATH entry, so $EXPECTED_ID cannot be resolved" >&2
        exit 1
    fi

    RESOLVED_RPATH=""
    while IFS= read -r RPATH; do
        [ -n "$RPATH" ] || continue
        case "$RPATH" in
            @loader_path) CANDIDATE="$CONSUMER_DIR" ;;
            @loader_path/*) CANDIDATE="$CONSUMER_DIR/${RPATH#@loader_path/}" ;;
            @executable_path | @executable_path/*) CANDIDATE="" ;;
            *) CANDIDATE="$RPATH" ;;
        esac
        [ -n "$CANDIDATE" ] || continue
        if [ "$(normalize_dir "$CANDIDATE")" = "$LIB_DIR" ]; then
            RESOLVED_RPATH="$RPATH"
            break
        fi
    done <<<"$RPATHS"

    if [ -z "$RESOLVED_RPATH" ]; then
        echo "FAIL: $CONSUMER has no LC_RPATH that resolves to $LIB_DIR (where $LIB lives);" >&2
        echo "      found LC_RPATH entries (none usable):" >&2
        printf '        %s\n' "$RPATHS" >&2
        exit 1
    fi
    echo "PASS: $CONSUMER depends on $EXPECTED_ID; LC_RPATH '$RESOLVED_RPATH' resolves to $LIB_DIR"
fi
