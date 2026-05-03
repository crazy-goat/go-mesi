#!/usr/bin/env bash
# check-no-generated-artifacts.sh
#
# CI guard: fail if generated/build artifacts are tracked by git.
# These files should never be committed — they are machine-generated,
# change on every run, and pollute diffs.
#
# Used by: .github/workflows/tests.yaml
# See also: .gitignore (coverage, debug, and IDE entries)

set -euo pipefail

pattern='coverage\.out$|coverage\.html$|\.coverprofile$|\.test$|__debug_bin|\.DS_Store$'

if git ls-files | grep -qE "$pattern"; then
    echo "ERROR: Generated artifacts are committed to the repository:" >&2
    git ls-files | grep -E "$pattern" >&2
    echo "" >&2
    echo "These files are machine-generated and must not be tracked." >&2
    echo "Remove them with: git rm --cached <file>" >&2
    echo "Ensure patterns are listed in .gitignore." >&2
    exit 1
fi

echo "OK: No generated artifacts found in tracked files."
