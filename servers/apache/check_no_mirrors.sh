#!/usr/bin/env bash
#
# check_no_mirrors.sh — regression guard for #558.
#
# The directive validators, setters, merge rules and lifecycle helpers live in
# shared headers included by both mod_mesi.c and the C unit tests. The unit
# files are only allowed to define their test_* cases plus the deliberate
# stand-ins for Apache-specific pieces (the ap_getword_conf tokenizer). Any
# other helper means production logic was copied back into a test, which is
# exactly the drift this guard prevents.
#
# Used by: servers/apache/Makefile (test-no-mirrors, a test-unit prerequisite)

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

extract_defs() {
    # Print the name of every function defined at column 0 by a `static`
    # (optionally `static inline`) declaration. Shared and production helpers
    # keep the name on the declaration's first line; multi-line argument lists
    # are fine.
    sed -nE 's/^static( inline)? [^;]*[ *]([A-Za-z_][A-Za-z0-9_]*)\(.*/\2/p' "$1"
}

check_file() {
    local file="$1"
    local allow="$2"
    local unexpected

    unexpected=$(extract_defs "$file" | grep -Ev "$allow" || true)
    if [[ -n "$unexpected" ]]; then
        echo "ERROR: $file defines helpers that are not test cases:" >&2
        while IFS= read -r name; do
            echo "  $name" >&2
        done <<<"$unexpected"
        echo "Share production logic through the shared headers instead of copying it (#558)." >&2
        echo "If this is a new Apache tokenizer stand-in, add it to this allowlist." >&2
        return 1
    fi
}

check_file test_directives.c '^(test_|parse_conf_word$)'
check_file test_pure.c '^test_'

for include in mesi_config.h cache_config.h filter_lifecycle.h mesi_pure.h parse_json_config.h; do
    if ! grep -q "#include \"$include\"" test_directives.c; then
        echo "ERROR: test_directives.c must include $include to test the real code (#558)." >&2
        exit 1
    fi
done

if ! grep -q '#include "mesi_pure.h"' test_pure.c; then
    echo "ERROR: test_pure.c must include mesi_pure.h to test the real code (#558)." >&2
    exit 1
fi

echo "OK: unit tests have no mirrored production helpers."
