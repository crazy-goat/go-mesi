#!/usr/bin/env bash
# Stop and remove the Docker test stacks of this worktree.
# Called by bin/worktree-done.sh before the worktree is removed.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

for dir in php-ext servers/*/; do
  dir="${dir%/}"
  if compgen -G "$dir/docker-compose*.y*ml" >/dev/null || compgen -G "$dir/compose*.y*ml" >/dev/null; then
    echo "docker compose down: $dir"
    (cd "$dir" && docker compose down -v --remove-orphans) || true
  fi
done
