#!/usr/bin/env bash
# Run all static analysis, linters and formatter checks. --fix applies fixes first.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

FIX=0
[ "${1:-}" = "--fix" ] && FIX=1
failed=()

step() {
    local name="$1"; shift
    echo "==> $name"
    "$@" || failed+=("$name")
}

# Run a command in every Go module (one shared root .golangci.yml).
# golangci-lint run also checks formatting (gofmt/goimports in .golangci.yml).
go_modules() {
    local rc=0 mod
    while IFS= read -r mod; do
        echo "--- $mod"
        (cd "$(dirname "$mod")" && "$@") || rc=1
    done < <(git ls-files '*go.mod')
    return $rc
}

lint_go() { go_modules golangci-lint run --config "$PWD/.golangci.yml" ./...; }
vet_go() { go_modules go vet ./...; }
fmt_go() { go_modules golangci-lint fmt --config "$PWD/.golangci.yml" ./...; }

c_files() { git ls-files -z -- '*.c' '*.h'; }

if [ "$FIX" = 1 ]; then
    fmt_go
    c_files | xargs -0 -r clang-format -i
fi

step "golangci-lint" lint_go
step "go vet" vet_go
step "clang-format" bash -c 'git ls-files -z -- "*.c" "*.h" | xargs -0 -r clang-format --dry-run --Werror'
step "php -l" bash -c 'git ls-files -z -- "*.php" | xargs -0 -r -n1 php -l >/dev/null'
step "shellcheck" bash -c 'git ls-files -z "*.sh" | xargs -0 -r shellcheck'
step "hadolint" bash -c 'git ls-files -z "*Dockerfile" "*Dockerfile.*" | xargs -0 -r hadolint'
step "generated artifacts" scripts/check-no-generated-artifacts.sh

if [ "${#failed[@]}" -gt 0 ]; then
    echo "Failed: ${failed[*]}" >&2
    exit 1
fi
echo "All checks passed."
