#!/usr/bin/env bash
# Self-test for scripts/check-darwin-rpath.sh.
#
# The check itself is macOS-only, but its parsing/validation logic is plain
# shell. This script fakes `uname -s` and `otool` so every PASS/FAIL branch —
# including an LC_RPATH that points at an unrelated directory or an
# @loader_path that lands outside the library's directory — runs on Linux CI
# without a macOS runner.
#
# Used by: bin/lint.sh (shellcheck also covers both scripts).
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
CHECK="$SCRIPT_DIR/check-darwin-rpath.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

FAKEBIN="$TMP/bin"
NOOTOOL="$TMP/nootool"
LIBDIR="$TMP/lib dir"
OTHERDIR="$TMP/other dir"
mkdir -p "$FAKEBIN" "$NOOTOOL" "$LIBDIR" "$OTHERDIR"

LIB="$LIBDIR/libgomesi.so"
CONSUMER_SAME_DIR="$LIBDIR/test-libgomesi"
CONSUMER_OTHER_DIR="$OTHERDIR/mesi.so"
touch "$LIB" "$CONSUMER_SAME_DIR" "$CONSUMER_OTHER_DIR"

cat >"$FAKEBIN/uname" <<'EOF'
#!/usr/bin/env bash
echo "${FAKE_UNAME:-Darwin}"
EOF

cat >"$FAKEBIN/otool" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
  -D)
    printf '%s:\n%s\n' "${2:-}" "${MODE_ID-@rpath/libgomesi.so}"
    ;;
  -L)
    printf '%s:\n\t%s (compatibility version 0.0.0, current version 0.0.0)\n\t/usr/lib/libSystem.B.dylib\n' \
      "${2:-}" "${MODE_DEP-@rpath/libgomesi.so}"
    ;;
  -l)
    if [ -n "${MODE_RPATH-}" ]; then
      printf '          cmd LC_RPATH\n      cmdsize 40\n         path %s (offset 12)\n' "$MODE_RPATH"
    fi
    ;;
esac
EOF
chmod +x "$FAKEBIN/uname" "$FAKEBIN/otool"

# A PATH with a Darwin `uname` but no otool at all, so `command -v otool` fails
# on macOS hosts too (where /usr/bin/otool would otherwise be found).
cp "$FAKEBIN/uname" "$NOOTOOL/uname"
for tool in awk bash dirname grep sed tail; do
    tool_path=$(command -v "$tool") || {
        echo "FAIL: self-test needs '$tool' on PATH" >&2
        exit 1
    }
    ln -s "$tool_path" "$NOOTOOL/$tool"
done
chmod +x "$NOOTOOL/uname"

checks=0
failures=0

# run_case NAME EXPECTED_RC OUTPUT_PATTERN LIB CONSUMER [ENV=VAL ...]
run_case() {
    local name=$1 want=$2 pattern=$3 lib=$4 consumer=$5
    shift 5
    local out rc
    checks=$((checks + 1))
    set +e
    if [ -n "$consumer" ]; then
        out=$(env PATH="$FAKEBIN:$PATH" "$@" "$CHECK" "$lib" "$consumer" 2>&1)
    else
        out=$(env PATH="$FAKEBIN:$PATH" "$@" "$CHECK" "$lib" 2>&1)
    fi
    rc=$?
    set -e
    if [ "$rc" -ne "$want" ]; then
        echo "FAIL: $name (exit $rc, expected $want)"
        printf '%s\n' "$out"
        failures=$((failures + 1))
        return
    fi
    if [ -n "$pattern" ] && ! printf '%s\n' "$out" | grep -Fq -- "$pattern"; then
        echo "FAIL: $name (output does not contain: $pattern)"
        printf '%s\n' "$out"
        failures=$((failures + 1))
        return
    fi
    echo "PASS: $name"
}

run_case "absolute LC_RPATH in the library directory" 0 \
    "resolves to $LIBDIR" "$LIB" "$CONSUMER_OTHER_DIR" "MODE_RPATH=$LIBDIR"

run_case "@loader_path LC_RPATH in the library directory" 0 \
    "LC_RPATH '@loader_path' resolves to $LIBDIR" "$LIB" "$CONSUMER_SAME_DIR" "MODE_RPATH=@loader_path"

run_case "unrelated LC_RPATH is rejected" 1 \
    "no LC_RPATH that resolves to $LIBDIR" "$LIB" "$CONSUMER_OTHER_DIR" "MODE_RPATH=$OTHERDIR"

run_case "@loader_path outside the library directory is rejected" 1 \
    "no LC_RPATH that resolves to $LIBDIR" "$LIB" "$CONSUMER_OTHER_DIR" "MODE_RPATH=@loader_path"

run_case "@executable_path is rejected" 1 \
    "no LC_RPATH that resolves to $LIBDIR" "$LIB" "$CONSUMER_OTHER_DIR" "MODE_RPATH=@executable_path"

run_case "missing LC_RPATH is rejected" 1 \
    "has no LC_RPATH entry" "$LIB" "$CONSUMER_OTHER_DIR"

run_case "bare install id is rejected" 1 \
    "install id is 'libgomesi.so'" "$LIB" "$CONSUMER_OTHER_DIR" \
    "MODE_RPATH=$LIBDIR" "MODE_ID=libgomesi.so"

run_case "bare consumer dependency is rejected" 1 \
    "does not depend on @rpath/libgomesi.so" "$LIB" "$CONSUMER_OTHER_DIR" \
    "MODE_RPATH=$LIBDIR" "MODE_DEP=libgomesi.so"

run_case "missing library is rejected" 1 \
    "not found" "$TMP/nope/libgomesi.so" "$CONSUMER_OTHER_DIR" "MODE_RPATH=$LIBDIR"

run_case "missing consumer is rejected" 1 \
    "consumer $TMP/nope/mesi.so not found" "$LIB" "$TMP/nope/mesi.so" "MODE_RPATH=$LIBDIR"

run_case "non-Darwin host skips" 0 \
    "SKIP: not macOS" "$LIB" "$CONSUMER_OTHER_DIR" "FAKE_UNAME=Linux"

run_case "missing otool fails" 1 \
    "otool not found" "$LIB" "$CONSUMER_OTHER_DIR" "PATH=$NOOTOOL"

if [ "$failures" -ne 0 ]; then
    echo "FAIL: $failures of $checks check-darwin-rpath cases failed" >&2
    exit 1
fi
echo "PASS: all $checks check-darwin-rpath cases passed"
