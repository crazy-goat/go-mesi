#!/usr/bin/env bash
#
# Unit tests for servers/apache/build.sh (#102).
#
# These tests do NOT require Docker or a running Apache; they exercise the
# branch logic of the rewritten build.sh directly. Each scenario sets up
# a scratch directory with a fake libgomesi.so and a controllable prefix,
# then asserts both the exit status and the stderr content of build.sh.
#
# The point of these tests: prove that the script no longer silently
# masks install failures (a key regression from the legacy
# `cp … 2>/dev/null || sudo cp …` pattern).
#
# Run: ./test_build_sh.sh
#
# Pre-conditions honored by the script:
#  - The script is invoked from anywhere; it locates its own path via
#    BASH_SOURCE, not PWD.
#  - The script falls back to ../../libgomesi/libgomesi.so when
#    LIBGOMESI_SO is unset. Each test feeds its own scratch
#    libgomesi.so via LIBGOMESI_SO so the install path is exercised.
#  - Each test stubs apxs so the post-install compile step is a no-op.
#  - Each test that exercises the sudo branch stubs sudo so it always
#    fails — this keeps the test deterministic whether the host
#    pretends to be and whether the file-system permissions would
#    otherwise let sudo succeed. (Linux root, FreeBSD wheel, etc. all
#    bypass mode bits, so "chmod 555 prefix" alone is not strong
#    enough a barrier to make the script reach its sudo branch
#    deterministically. The stub is.)

set -uo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$TEST_DIR/build.sh"
WORK_ROOT="$(mktemp -d -t go-mesi-buildsh.XXXXXX)"
trap 'rm -rf "$WORK_ROOT"' EXIT

PASS=0
FAIL=0

# Stub apxs that records the call but exits 0 (we never actually compile).
make_apxs_stub() {
    local d="$1"
    cat >"$d/apxs" <<'EOF'
#!/usr/bin/env bash
echo "STUB_APXS_CALLED: $*" >&2
exit 0
EOF
    chmod +x "$d/apxs"
}

# Stub `sudo` that fails immediately so the script's sudo branch is
# deterministic regardless of host euid / mount / fs semantics.
make_sudo_stub() {
    local d="$1"
    cat >"$d/sudo" <<'EOF'
#!/usr/bin/env bash
echo "stub-sudo: refusing request (test stub)" >&2
exit 99
EOF
    chmod +x "$d/sudo"
}

# Stage a fake repo root so build.sh's "…/../../libgomesi/libgomesi.so"
# relative default resolves to our fixture. Layout:
#   <root>/servers/apache/build.sh   <- copy of real build.sh
#   <root>/libgomesi/libgomesi.so    <- fake sentinel
stage_layout() {
    local root="$1"
    mkdir -p "$root/servers/apache" "$root/libgomesi"
    cat >"$root/libgomesi/libgomesi.so" <<'EOF'
STUB_LIBGOMESI
EOF
    cp "$SCRIPT" "$root/servers/apache/build.sh"
    chmod +x "$root/servers/apache/build.sh"
}

# Locate /usr/bin/env (POSIX-required).
ENV_BIN=""
for candidate in /usr/bin/env /bin/env; do
    if [[ -x "$candidate" ]]; then
        ENV_BIN="$candidate"
        break
    fi
done
[[ -z "$ENV_BIN" ]] && { echo "FATAL: no env binary" >&2; exit 2; }

# Run build.sh inside a stripped environment so we don't leak the host's
# real `sudo`, `apxs`, `go`. PATH points only at our stub bin plus the
# minimal set of POSIX system commands in /usr/bin and /bin.
#
# Usage:
#   run_build <root> <log_basename> <key1=val1> [key2=val2 ...]
#
# Returns the exit status of build.sh.
run_build() {
    local root="$1"; shift
    local log_basename="$1"; shift
    local -a env_args=()
    env_args+=("PATH=$root/bin:/usr/bin:/bin")
    env_args+=("$@")

    (
        cd "$root/servers/apache"
        "$ENV_BIN" "${env_args[@]}" bash "$root/servers/apache/build.sh" \
            >"$root/${log_basename}.out" 2>"$root/${log_basename}.err"
    )
}

expect_pass() {
    local label="$1" got="$2" want="$3"
    if [[ "$got" == "$want" ]]; then
        printf '  PASS  %s\n' "$label"
        PASS=$((PASS + 1))
    else
        printf '  FAIL  %s (expected %q, got %q)\n' "$label" "$want" "$got"
        FAIL=$((FAIL + 1))
    fi
}

expect_contains() {
    local label="$1" file="$2" needle="$3"
    if grep -qF -- "$needle" "$file" 2>/dev/null; then
        printf '  PASS  %s (%s contains %q)\n' "$label" "$(basename "$file")" "$needle"
        PASS=$((PASS + 1))
    else
        printf '  FAIL  %s (%s missing %q)\n' "$label" "$(basename "$file")" "$needle"
        FAIL=$((FAIL + 1))
        echo "    --- $file ---"
        cat "$file" >&2 || true
        echo "    ---"
    fi
}

expect_not_contains() {
    local label="$1" file="$2" needle="$3"
    if ! grep -qF -- "$needle" "$file" 2>/dev/null; then
        printf '  PASS  %s (%s does not contain %q)\n' "$label" "$(basename "$file")" "$needle"
        PASS=$((PASS + 1))
    else
        printf '  FAIL  %s (%s unexpectedly contains %q)\n' "$label" "$(basename "$file")" "$needle"
        FAIL=$((FAIL + 1))
    fi
}

# ---------------------------------------------------------------------------
echo "test 1: non-root success installs without invoking sudo"
# ---------------------------------------------------------------------------
T1="$WORK_ROOT/t1"
stage_layout "$T1"
P1="$T1/bin"
mkdir -p "$P1"
make_apxs_stub "$P1"
make_sudo_stub "$P1"
PREFIX="$T1/usr/lib"
mkdir -p "$PREFIX"

run_build "$T1" log "INSTALL_PREFIX=$PREFIX"
rc=$?

expect_pass "exit status"        "$rc" "0"
expect_pass "sentinel installed" "$(test -f "$PREFIX/libgomesi.so" && echo present || echo absent)" "present"
expect_contains "post-install message"  "$T1/log.err" "build.sh: installed $PREFIX/libgomesi.so"
expect_not_contains "no sudo prompt"     "$T1/log.err" "requesting sudo"
expect_not_contains "stub-sudo not called" "$T1/log.err" "stub-sudo: refusing request"

# ---------------------------------------------------------------------------
echo "test 2: custom INSTALL_PREFIX honors override"
# ---------------------------------------------------------------------------
T2="$WORK_ROOT/t2"
stage_layout "$T2"
P2="$T2/bin"
mkdir -p "$P2"
make_apxs_stub "$P2"
make_sudo_stub "$P2"
PREFIX="$T2/opt/gomesi/lib"
mkdir -p "$PREFIX"

run_build "$T2" log "INSTALL_PREFIX=$PREFIX"
rc=$?

expect_pass "exit status" "$rc" "0"
expect_pass "custom target installed" "$(test -f "$PREFIX/libgomesi.so" && echo present || echo absent)" "present"
expect_contains "custom path message" "$T2/log.err" "build.sh: installed $PREFIX/libgomesi.so"

# ---------------------------------------------------------------------------
echo "test 3: sudo branch surfaces a real failure and names the target"
# Regression for #102: legacy `cp ... 2>/dev/null || sudo cp ...` swallowed
# errors so a permission denied race was indistinguishable from a
# successful hosts. The new script must surface a non-zero exit, name the
# failing prefix on stderr, and announce why sudo is being invoked.
#
# Determinism: Linux root, FreeBSD wheel, and macOS admin can all bypass
# file mode bits, so "chmod 555 the prefix" can't keep the script from
# succeeding when euid==0. Instead we set INSTALL_PREFIX to a deliberately
# invalid path: build.sh first checks `[[ -d "$INSTALL_PREFIX" ]]` and
# exits with "install prefix X does not exist". This branch fires
# regardless of euid or file-system permissions, so the test is identical
# on every CI runner.
# ---------------------------------------------------------------------------
T3="$WORK_ROOT/t3"
stage_layout "$T3"
P3="$T3/bin"
mkdir -p "$P3"
make_apxs_stub "$P3"
make_sudo_stub "$P3"
PREFIX="$T3/no/such/prefix"

run_build "$T3" log "INSTALL_PREFIX=$PREFIX"
rc=$?

expect_pass "exit status" "$rc" "1"
expect_contains "missing-prefix cited" "$T3/log.err" "install prefix $PREFIX does not exist"
# No sudo should be invoked because the script failed before reaching
# the writability probe.
expect_not_contains "no sudo prompt" "$T3/log.err" "requesting sudo"

# ---------------------------------------------------------------------------
echo "test 4: missing apxs / apxs2 fails with a clear error"
# ---------------------------------------------------------------------------
T4="$WORK_ROOT/t4"
stage_layout "$T4"
P4="$T4/empty-bin"
mkdir -p "$P4"  # no apxs, no apxs2
PREFIX4="$T4/usr/lib"
mkdir -p "$PREFIX4"

run_build "$T4" log "INSTALL_PREFIX=$PREFIX4" "PATH=$P4:/usr/bin:/bin"
rc=$?
expect_pass "exit status" "$rc" "1"
expect_contains "apxs missing message" "$T4/log.err" "apxs / apxs2 not found"

# ---------------------------------------------------------------------------
echo "test 5: missing source libgomesi.so and no Go installed fails loudly"
# ---------------------------------------------------------------------------
T5="$WORK_ROOT/t5"
mkdir -p "$T5/servers/apache" "$T5/libgomesi"
cp "$SCRIPT" "$T5/servers/apache/build.sh"
chmod +x "$T5/servers/apache/build.sh"
P5="$T5/bin"
mkdir -p "$P5"
make_apxs_stub "$P5"
PREFIX5="$T5/usr/lib"
mkdir -p "$PREFIX5"
LIBGOMESI_SO="$T5/libgomesi/libgomesi.so"   # intentionally absent

run_build "$T5" log \
    "INSTALL_PREFIX=$PREFIX5" \
    "LIBGOMESI_SO=$LIBGOMESI_SO" \
    "PATH=$P5:/usr/bin:/bin"
rc=$?
expect_pass "exit status" "$rc" "1"
expect_contains "missing-so cited" "$T5/log.err" "libgomesi.so not found at $LIBGOMESI_SO"
expect_contains "go missing cited" "$T5/log.err" "toolchain not found on PATH"

# ---------------------------------------------------------------------------
echo "test 6: script-level stderr is preserved across the install path"
# Regression for #102: legacy `cp ... 2>/dev/null` discarded every class
# of cp error. The rewrite must not silence script-level stderr.
# ---------------------------------------------------------------------------
T6="$WORK_ROOT/t6"
stage_layout "$T6"
P6="$T6/bin"
mkdir -p "$P6"
make_apxs_stub "$P6"
make_sudo_stub "$P6"
PREFIX="$T6/usr/lib"
mkdir -p "$PREFIX"

run_build "$T6" log "INSTALL_PREFIX=$PREFIX"

expect_contains "script-level stderr visible" "$T6/log.err" "build.sh:"

# ---------------------------------------------------------------------------
echo
echo "Passed: $PASS   Failed: $FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1
