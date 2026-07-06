#!/usr/bin/env bash
#
# Build the Apache mod_mesi shared object against an installed libgomesi.so.
#
# Defaults install the module's prerequisite into /usr/lib:
#   - libgomesi.so is searched first in $LIBGOMESI_SO, then in ../../libgomesi/.
#     Override the destination with INSTALL_PREFIX (e.g. /usr/local/lib on
#     FreeBSD-style layouts).
#
# The script refuses to silently swallow permission errors (#102). When the
# install prefix isn't writable by the current user, sudo is invoked with a
# clear message on stderr; a real failure surfaces the underlying tool's
# stderr verbatim rather than being masked behind a benign-looking `||`.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"

LIBGOMESI_SO="${LIBGOMESI_SO:-$ROOT_DIR/libgomesi/libgomesi.so}"
INSTALL_PREFIX="${INSTALL_PREFIX:-/usr/lib}"

if [[ ! -f "$LIBGOMESI_SO" ]]; then
    echo "build.sh: libgomesi.so not found at $LIBGOMESI_SO; building from source" >&2
    if ! command -v go >/dev/null 2>&1; then
        echo "build.sh: 'go' toolchain not found on PATH; install Go or set LIBGOMESI_SO to an existing shared object" >&2
        exit 1
    fi
    (
        set -eux
        cd "$ROOT_DIR/libgomesi"
        go build -trimpath -ldflags="-s -w" \
            -buildmode=c-shared -o libgomesi.so \
            libgomesi.go cache_config.go
    )
fi

if [[ ! -f "$LIBGOMESI_SO" ]]; then
    echo "build.sh: libgomesi.so is still missing at $LIBGOMESI_SO after build attempt" >&2
    exit 1
fi

target="$INSTALL_PREFIX/libgomesi.so"
if [[ ! -d "$INSTALL_PREFIX" ]]; then
    echo "build.sh: install prefix $INSTALL_PREFIX does not exist" >&2
    exit 1
fi

# `-w` is the cheapest portable check across POSIX systems: it returns
# true when the directory accepts new files for the current effective
# user. We avoid the legacy `cp ... 2>/dev/null || sudo cp ...` pattern
# because it discards every error class (missing source, read-only fs,
# ENOSPC) and lets a half-broken install slip through unnoticed.
if [[ -w "$INSTALL_PREFIX" ]]; then
    install -m 0644 "$LIBGOMESI_SO" "$target"
else
    echo "build.sh: $INSTALL_PREFIX is not writable as $(id -un); requesting sudo to install $target" >&2
    # `sudo -n` would skip the prompt; we deliberately allow interactive
    # sudo here so a developer gets the prompt instead of a silent hang
    # when invoking the script from a fresh shell.
    sudo install -m 0644 "$LIBGOMESI_SO" "$target"
fi
echo "build.sh: installed $target" >&2

cd "$SCRIPT_DIR"

APXS=""
if command -v apxs2 >/dev/null 2>&1; then
    APXS="$(command -v apxs2)"
elif command -v apxs >/dev/null 2>&1; then
    APXS="$(command -v apxs)"
else
    echo "build.sh: apxs / apxs2 not found on PATH; install apache2-dev (Debian/Ubuntu) or httpd-devel (Red Hat)" >&2
    exit 1
fi

"$APXS" -c mod_mesi.c

echo "build.sh: build complete (mod_mesi.so)"
