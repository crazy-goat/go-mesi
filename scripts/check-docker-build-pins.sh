#!/usr/bin/env bash
# check-docker-build-pins.sh
#
# CI guard: a pinned Docker build dependency records the minimum Go it declares
# in its own go.mod as an `ARG <NAME>_GO_MIN` next to its `ARG <NAME>_VERSION`.
# This script checks that the build stage's Go is at least that minimum.
#
# Why: an unpinned build dependency breaks the image build whenever upstream
# raises its Go requirement. That happened on 2026-10-04 when frankenphp v1.13.0
# started requiring go >= 1.27.0 while servers/frankenphp/Dockerfile builds with
# Go 1.26, and it made `ci-ok` red on `main` with no commit to this repository.
# Pinning the dependency fixes it, but a pin ages silently, so the *minimum Go*
# is recorded as data and checked here. Bumping a pin without deciding what Go it
# needs then fails the lint of the pull request that does the bumping, instead of
# failing `ci-ok` for whoever pushes next.
#
# A Dockerfile opts in by declaring a *_GO_MIN; nothing else in the repository is
# affected, so pinning a build dependency is a one-line change that also gets this
# guard for free.
#
# This script is offline and deterministic: it reads only tracked Dockerfiles.
# Building the image is what actually verifies a pin; this only keeps the
# recorded numbers and the build stage consistent with each other.
#
# Used by: bin/lint.sh
# See also: servers/frankenphp/Dockerfile

set -euo pipefail
cd "$(dirname "$0")/.."

failed=0

# Normalize a Go version to major.minor.patch, so that a version without a patch
# (1.26) compares equal to an explicit .0 (1.26.0), as the Go toolchain does.
normalize_go() {
    local v="${1#v}" major rest minor patch
    case "$v" in
        '' | *[!0-9.]*) return 1 ;;
    esac
    major="${v%%.*}"
    if [ "$v" = "$major" ]; then
        printf '%s.0.0' "$major"
        return 0
    fi
    rest="${v#*.}"
    minor="${rest%%.*}"
    if [ "$rest" = "$minor" ]; then
        patch=0
    else
        patch="${rest#*.}"
    fi
    case "$patch" in
        '' | *[!0-9]*) patch=0 ;;
    esac
    printf '%s.%s.%s' "$major" "$minor" "$patch"
}

# True when Go version $1 is at least $2.
go_at_least() {
    local want have
    want="$(normalize_go "$2")"
    have="$(normalize_go "$1")"
    [ "$(printf '%s\n%s\n' "$have" "$want" | sort -V | head -n1)" = "$want" ]
}

while IFS= read -r dockerfile; do
    go_mins="$(grep -oE '^ARG [A-Za-z0-9_]+_GO_MIN' "$dockerfile" | awk '{print $2}' | sort -u || true)"
    if [ -z "$go_mins" ]; then
        continue
    fi

    echo "--- $dockerfile"

    # The newest Go the Dockerfile builds with. When there are several golang
    # stages, the newest one wins: this guard is meant to fail loudly, not to
    # fail spuriously, and a miss only defers to the image build, which is the
    # real authority and fails on the pull request either way.
    build_go=""
    while IFS= read -r line; do
        line="${line#FROM golang:}"
        if [ -z "$build_go" ] || go_at_least "$line" "$build_go"; then
            build_go="$line"
        fi
    done < <(grep -oE '^FROM golang:[0-9]+(\.[0-9]+)*' "$dockerfile" | cut -d: -f2 || true)
    if [ -z "$build_go" ]; then
        echo "ERROR: $dockerfile declares a *_GO_MIN but has no 'FROM golang:<version>'" >&2
        echo "       build stage to check it against." >&2
        failed=1
        continue
    fi
    echo "    build stage Go: $build_go"

    for name in $go_mins; do
        pin_name="${name%_GO_MIN}_VERSION"
        # The pin must carry a default value, because that is what the FROM and
        # RUN lines interpolate. A bare `ARG NAME` (re-declaring a global ARG
        # inside a stage) is not a pin.
        version="$(sed -n "s/^ARG ${pin_name}=//p" "$dockerfile" | head -n1)"
        if [ -z "$version" ]; then
            echo "ERROR: $dockerfile declares $name but no ARG $pin_name=<version>," >&2
            echo "       so the build dependency is not pinned. Pin it, or drop $name." >&2
            failed=1
            continue
        fi

        go_min="$(sed -n "s/^ARG ${name}=//p" "$dockerfile" | head -n1)"

        # Anything normalize_go cannot parse must be reported here, not turned
        # into a silent non-zero exit further down.
        if ! printf '%s' "$go_min" | grep -qE '^[0-9]+(\.[0-9]+)*$'; then
            echo "ERROR: $dockerfile: ARG $name=$go_min is not a Go version." >&2
            echo "       Use the version the dependency declares in its go.mod, e.g. 1.26.0." >&2
            failed=1
            continue
        fi

        echo "    $pin_name=$version requires Go >= $go_min"

        if ! go_at_least "$build_go" "$go_min"; then
            echo "ERROR: $dockerfile: the build stage Go $build_go is older than the Go $go_min" >&2
            echo "       that $pin_name=$version requires. The image build would fail with" >&2
            echo "       'requires go >= $go_min', and pinning $version cannot fix that." >&2
            echo "       Either lower $pin_name to a release that still builds with Go $build_go," >&2
            echo "       or raise the build stage to a newer golang image." >&2
            failed=1
        fi
    done
done < <(git ls-files '*Dockerfile' '*Dockerfile.*')

if [ "$failed" -ne 0 ]; then
    echo "" >&2
    echo "Docker build pins are inconsistent with their build stages." >&2
    exit 1
fi
echo "OK: every pinned Docker build dependency is compatible with its build stage Go."
