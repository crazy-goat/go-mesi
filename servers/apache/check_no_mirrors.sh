#!/usr/bin/env bash
#
# check_no_mirrors.sh — regression guard for #558.
#
# The directive validators, setters and merge helpers live in mesi_config.h and
# are included by both mod_mesi.c and test_directives.c. The test file is only
# allowed to define its test_* cases plus two deliberate stand-ins for the one
# Apache-specific piece (the ap_getword_conf tokenizer). Any other helper means
# production logic was copied back into the test, which is exactly the drift
# this guard exists to prevent.
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

unexpected=$(
    extract_defs test_directives.c |
        grep -Ev '^(test_|conf_space$|parse_conf_word$)' || true
)

if [[ -n "$unexpected" ]]; then
    echo "ERROR: test_directives.c defines helpers that are not test cases:" >&2
    while IFS= read -r name; do
        echo "  $name" >&2
    done <<<"$unexpected"
    echo "Share production logic through mesi_config.h instead of copying it (#558)." >&2
    echo "If this is a new Apache tokenizer stand-in, add it to this allowlist." >&2
    exit 1
fi

if ! grep -q '#include "mesi_config.h"' test_directives.c; then
    echo "ERROR: test_directives.c must include mesi_config.h to test the real code (#558)." >&2
    exit 1
fi

echo "OK: test_directives.c has no mirrored production helpers."
