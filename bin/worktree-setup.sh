#!/usr/bin/env bash
# Download Go module dependencies for every module in the repository.
# Called by bin/worktree.sh after a new worktree is created.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

git ls-files 'go.mod' '*/go.mod' | while read -r mod; do
  dir="$(dirname "$mod")"
  echo "go mod download: $dir"
  (cd "$dir" && go mod download)
done
