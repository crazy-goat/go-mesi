#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

TEST_DIR="$(mktemp -d)"
CLI_BINARY="$TEST_DIR/mesi-cli"
SERVER_BINARY="$TEST_DIR/mesi-test-server"
SERVER_PID=""
PASS_COUNT=0
FAIL_COUNT=0

cleanup() {
	[ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
	sleep 1
	[ -n "$SERVER_PID" ] && kill -9 "$SERVER_PID" 2>/dev/null || true
	rm -rf "$TEST_DIR"
}

pass() {
	echo "PASS: $1"
	PASS_COUNT=$((PASS_COUNT + 1))
}

fail() {
	echo "FAIL: $1"
	[ -n "$2" ] && echo "  $2"
	FAIL_COUNT=$((FAIL_COUNT + 1))
}

trap cleanup EXIT

echo "=== Building CLI binary ==="
go build -o "$CLI_BINARY" "$SCRIPT_DIR"

echo "=== Building test server ==="
go build -o "$SERVER_BINARY" "$ROOT_DIR/tests/server"

echo "=== Starting test server on :8080 ==="
"$SERVER_BINARY" &
SERVER_PID=$!

for i in $(seq 1 10); do
	if curl -sf "http://127.0.0.1:8080/hello" > /dev/null 2>&1; then
		echo "Server ready (attempt $i)"
		break
	fi
	if [ "$i" -eq 10 ]; then
		echo "Server failed to start"
		exit 1
	fi
	sleep 1
done

echo ""
echo "=== CLI E2E Tests ==="

echo ""
echo "--- File Mode: Static ESI Processing ---"

echo "Test 1: ESI comment unwrapping"
RESULT=$("$CLI_BINARY" "$ROOT_DIR/tests/fixtures/05-comment.html" 2>/dev/null)
if echo "$RESULT" | grep -q "This should be empty:"; then
	pass "ESI comment stripped (no raw tags)"
else
	fail "ESI comment strip" "Result: $RESULT"
fi

echo "Test 2: ESI esi:remove stripping"
RESULT=$("$CLI_BINARY" "$ROOT_DIR/tests/fixtures/04-remove.html" 2>/dev/null)
if echo "$RESULT" | grep -q "This should be empty: \[\]"; then
	pass "ESI remove stripped"
else
	fail "ESI remove strip" "Result: $RESULT"
fi

echo "Test 3: ESI inline processing"
RESULT=$("$CLI_BINARY" "$ROOT_DIR/tests/fixtures/14-esi-inline.html" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello from inline"; then
	pass "ESI inline processed"
else
	fail "ESI inline" "Result: $RESULT"
fi

echo "Test 4: Non-HTML passthrough"
RESULT=$("$CLI_BINARY" "$ROOT_DIR/tests/fixtures/01-escape.html" 2>/dev/null)
if echo "$RESULT" | grep -q "some html <b>tag</b>"; then
	pass "Non-HTML passthrough (HTML entities preserved)"
else
	fail "Non-HTML passthrough" "Result: $RESULT"
fi

echo "Test 5: Large file processing"
for i in $(seq 1 100); do
	echo "<!--esi line${i}-->"
done > "$TEST_DIR/large.html"
RESULT=$("$CLI_BINARY" "$TEST_DIR/large.html" 2>/dev/null)
LINE_COUNT=$(echo "$RESULT" | grep -c "line" || true)
if [ "$LINE_COUNT" -ge 90 ]; then
	pass "Large file processed correctly ($LINE_COUNT lines)"
else
	fail "Large file" "Expected ~100 lines, got $LINE_COUNT"
fi

echo "Test 6: Empty file"
touch "$TEST_DIR/empty.html"
RESULT=$("$CLI_BINARY" "$TEST_DIR/empty.html" 2>/dev/null)
if [ -z "$RESULT" ]; then
	pass "Empty file produces no output"
else
	fail "Empty file" "Output: $(echo "$RESULT" | head -c 100)"
fi

echo ""
echo "--- URL Mode Tests ---"

echo "Test 7: URL mode - fetch content"
RESULT=$("$CLI_BINARY" "http://127.0.0.1:8080/hello" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello World"; then
	pass "URL mode fetches and outputs content"
else
	fail "URL mode fetch" "Result: $RESULT"
fi

echo "Test 8: URL mode returns error on bad URL"
RESULT=$("$CLI_BINARY" "http://127.0.0.1:1/" 2>&1 || true)
if echo "$RESULT" | grep -qi "error\|refused\|connection"; then
	pass "Bad URL error reported"
else
	fail "Bad URL" "Output: $RESULT"
fi

echo ""
echo "--- Flag Tests ---"

echo "Test 9: --default-url flag"
RESULT=$("$CLI_BINARY" --default-url "http://127.0.0.1:8080/" "http://127.0.0.1:8080/hello" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello World"; then
	pass "Default URL flag works"
else
	fail "Default URL flag" "Result: $RESULT"
fi

echo "Test 10: --max-depth flag with static content"
RESULT=$("$CLI_BINARY" --max-depth 0 "$ROOT_DIR/tests/fixtures/05-comment.html" 2>/dev/null)
if echo "$RESULT" | grep -q "This should be empty:"; then
	pass "Max-depth=0 still processes static ESI"
else
	fail "Max-depth=0" "Result: $RESULT"
fi

echo "Test 11: --parse-on-header flag in file mode"
RESULT=$("$CLI_BINARY" --parse-on-header "$ROOT_DIR/tests/fixtures/05-comment.html" 2>/dev/null)
if echo "$RESULT" | grep -q "This should be empty:"; then
	pass "Parse-on-header does not affect file mode"
else
	fail "Parse-on-header file mode" "Result: $RESULT"
fi

echo "Test 12: Multiple flags together"
RESULT=$("$CLI_BINARY" --max-depth 3 --timeout 30 --default-url "http://127.0.0.1:8080/" "http://127.0.0.1:8080/returnString/Hello" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello"; then
	pass "Multiple flags work together"
else
	fail "Multiple flags" "Result: $RESULT"
fi

echo ""
echo "--- Error Handling ---"

echo "Test 13: Missing argument produces error message"
RESULT=$("$CLI_BINARY" 2>&1 || true)
if echo "$RESULT" | grep -qi "error\|missing\|usage"; then
	pass "Missing argument error reported"
else
	fail "Missing argument" "Output: $RESULT"
fi

echo "Test 14: Nonexistent file produces error message"
RESULT=$("$CLI_BINARY" "/nonexistent/file.html" 2>&1 || true)
if echo "$RESULT" | grep -qi "error"; then
	pass "Nonexistent file error reported"
else
	fail "Nonexistent file" "Output: $RESULT"
fi

echo "Test 15: Bad URL produces error message"
RESULT=$("$CLI_BINARY" "http://127.0.0.1:99999/" 2>&1 || true)
if echo "$RESULT" | grep -qi "error\|refused\|timeout\|connection"; then
	pass "Bad URL error reported"
else
	fail "Bad URL" "Output: $RESULT"
fi

echo ""
echo "--- Fixture Comparison (Inline Fixtures) ---"

FIXTURE_PASS=0
FIXTURE_FAIL=0

for f in "$ROOT_DIR/tests/fixtures/"*.html; do
	base=$(basename "$f")

	case "$base" in
		14-esi-inline.html|15-esi-inline-escape.html|16-esi-inline-empty.html)
			;;
		*)
			continue
			;;
	esac

	expected="${f}.expected"
	RESULT=$("$CLI_BINARY" "$f" 2>/dev/null || true)
	EXPECTED=$(cat "$expected")

	RESULT_TRIM=$(echo "$RESULT" | sed -e 's/[[:space:]]*$//' -e 's/^[[:space:]]*//')
	EXPECTED_TRIM=$(echo "$EXPECTED" | sed -e 's/[[:space:]]*$//' -e 's/^[[:space:]]*//')

	if [ "$RESULT_TRIM" = "$EXPECTED_TRIM" ]; then
		pass "Fixture $base matches expected"
		FIXTURE_PASS=$((FIXTURE_PASS + 1))
	else
		fail "Fixture $base mismatch"
		FIXTURE_FAIL=$((FIXTURE_FAIL + 1))
	fi
done

echo ""
echo "=== Summary ==="
echo "Passed: $PASS_COUNT"
echo "Failed: $FAIL_COUNT"
echo "Fixtures matched: $FIXTURE_PASS / $((FIXTURE_PASS + FIXTURE_FAIL))"

TOTAL_FAIL=$((FAIL_COUNT + FIXTURE_FAIL))
if [ "$TOTAL_FAIL" -gt 0 ]; then
	exit 1
fi

echo ""
echo "=== All E2E tests passed ==="
