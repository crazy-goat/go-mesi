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

# Give every suite image a per-worktree tag. A compose file with a fixed
# `image: name` makes two worktrees overwrite each other's image; with
# `image: ${NAME_IMAGE:-name}` the default (used by CI and the main checkout) is
# unchanged and .env.worktree overrides it. Existing entries are kept.
if [[ -f .env.worktree && -n "${COMPOSE_PROJECT_NAME:-}" ]]; then
  { grep -rhoE '\$\{[A-Z0-9_]*_IMAGE:-[a-z0-9._-]+\}' --include='*compose*.y*ml' . 2>/dev/null || true; } \
    | sed -E 's/^\$\{([A-Z0-9_]+):-(.*)\}$/\1 \2/' | sort -u | while read -r name default; do
      if ! grep -q "^$name=" .env.worktree; then
        echo "$name=$default-$COMPOSE_PROJECT_NAME" >>.env.worktree
      fi
    done
fi
