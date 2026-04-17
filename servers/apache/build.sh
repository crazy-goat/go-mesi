#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"

LIBGOMESI_SO="${LIBGOMESI_SO:-$ROOT_DIR/libgomesi/libgomesi.so}"

if [ ! -f "$LIBGOMESI_SO" ]; then
    echo "Building libgomesi.so..."
    cd "$ROOT_DIR/libgomesi"
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go
fi

cp "$LIBGOMESI_SO" /usr/lib/libgomesi.so 2>/dev/null || sudo cp "$LIBGOMESI_SO" /usr/lib/libgomesi.so

cd "$SCRIPT_DIR"

if command -v apxs2 &> /dev/null; then
    APXS=apxs2
else
    APXS=apxs
fi

$APXS -c mod_mesi.c

echo "Build complete: mod_mesi.so"
