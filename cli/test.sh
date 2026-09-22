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

echo "=== Starting test server on :18080 ==="
"$SERVER_BINARY" &
SERVER_PID=$!

for i in $(seq 1 10); do
	if curl -sf "http://127.0.0.1:18080/hello" > /dev/null 2>&1; then
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
RESULT=$("$CLI_BINARY" "http://127.0.0.1:18080/hello" 2>/dev/null)
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
RESULT=$("$CLI_BINARY" --default-url "http://127.0.0.1:18080/" "http://127.0.0.1:18080/hello" 2>/dev/null)
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

echo "Test 10b: --max-depth above MaxMaxDepth is rejected"
set +e
OVER_ERR=$("$CLI_BINARY" --max-depth 10001 "$ROOT_DIR/tests/fixtures/05-comment.html" 2>&1)
OVER_CODE=$?
set -e
if [ "$OVER_CODE" -ne 0 ] && echo "$OVER_ERR" | grep -q "max-depth"; then
	pass "Max-depth=10001 rejected"
else
	fail "Max-depth=10001 reject" "exit=$OVER_CODE err=$OVER_ERR"
fi

echo "Test 11: --parse-on-header flag in file mode"
RESULT=$("$CLI_BINARY" --parse-on-header "$ROOT_DIR/tests/fixtures/05-comment.html" 2>/dev/null)
if echo "$RESULT" | grep -q "This should be empty:"; then
	pass "Parse-on-header does not affect file mode"
else
	fail "Parse-on-header file mode" "Result: $RESULT"
fi

echo "Test 12: Multiple flags together"
RESULT=$("$CLI_BINARY" --max-depth 3 --timeout 30 --default-url "http://127.0.0.1:18080/" "http://127.0.0.1:18080/returnString/Hello" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello"; then
	pass "Multiple flags work together"
else
	fail "Multiple flags" "Result: $RESULT"
fi

echo ""
echo "--- Cache Backend Tests ---"

echo "Test 13: -cache-backend=memory deduplicates duplicate includes"
cat > "$TEST_DIR/dup-includes-cached.html" <<'EOF'
<esi:include src="count/cache-with-backend"/>
<esi:include src="count/cache-with-backend"/>
<esi:include src="count/cache-with-backend"/>
EOF
RESULT=$("$CLI_BINARY" -cache-backend=memory -max-workers=1 -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/dup-includes-cached.html" 2>/dev/null)
RESULT_TRIM=$(echo "$RESULT" | tr -d '\n')
if [ "$RESULT_TRIM" = "111" ]; then
	pass "Cache dedup: 3 duplicate includes resolved with single origin hit"
else
	fail "Cache dedup" "Expected '111', got: $RESULT_TRIM"
fi

echo "Test 14: no -cache-backend hits origin for every include"
cat > "$TEST_DIR/dup-includes-uncached.html" <<'EOF'
<esi:include src="count/cache-no-backend"/>
<esi:include src="count/cache-no-backend"/>
<esi:include src="count/cache-no-backend"/>
EOF
RESULT=$("$CLI_BINARY" -max-workers=1 -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/dup-includes-uncached.html" 2>/dev/null)
RESULT_TRIM=$(echo "$RESULT" | tr -d '\n')
if [ "$RESULT_TRIM" = "123" ]; then
	pass "No cache: 3 duplicate includes each hit origin"
else
	fail "No cache" "Expected '123', got: $RESULT_TRIM"
fi

echo "Test 15: unknown -cache-backend value exits with error"
RESULT=$("$CLI_BINARY" -cache-backend=unknown -allow-private-ips "$TEST_DIR/dup-includes-cached.html" 2>&1 || true)
if echo "$RESULT" | grep -qi "unknown cache backend"; then
	pass "Unknown backend rejected"
else
	fail "Unknown backend" "Output: $RESULT"
fi

echo ""
echo "--- Error Handling ---"

echo "Test 16: Missing argument produces error message"
RESULT=$("$CLI_BINARY" 2>&1 || true)
if echo "$RESULT" | grep -qi "error\|missing\|usage"; then
	pass "Missing argument error reported"
else
	fail "Missing argument" "Output: $RESULT"
fi

echo "Test 17: Nonexistent file produces error message"
RESULT=$("$CLI_BINARY" "/nonexistent/file.html" 2>&1 || true)
if echo "$RESULT" | grep -qi "error"; then
	pass "Nonexistent file error reported"
else
	fail "Nonexistent file" "Output: $RESULT"
fi

echo "Test 18: Bad URL produces error message"
RESULT=$("$CLI_BINARY" "http://127.0.0.1:99999/" 2>&1 || true)
if echo "$RESULT" | grep -qi "error\|refused\|timeout\|connection"; then
	pass "Bad URL error reported"
else
	fail "Bad URL" "Output: $RESULT"
fi

echo ""
echo "--- Allowed Hosts Tests ---"

echo "Test 19: -allowed-hosts allows include from listed host"
cat > "$TEST_DIR/allowed-host.html" <<'EOF'
<esi:include src="hello"/>
EOF
RESULT=$("$CLI_BINARY" -allowed-hosts=127.0.0.1 -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/allowed-host.html" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello World"; then
	pass "Allowed host include resolved"
else
	fail "Allowed host include" "Result: $RESULT"
fi

echo "Test 20: -allowed-hosts blocks include from unlisted host"
RESULT=$("$CLI_BINARY" -allowed-hosts=other.example.com -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/allowed-host.html" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello World"; then
	fail "Unlisted host include" "Expected blocked include, got: $RESULT"
else
	pass "Unlisted host include blocked"
fi

echo ""
echo "--- AllowPrivateIPsForAllowedHosts Tests ---"

echo "Test 21: -allowPrivateIPsForAllowedHosts allows listed host on private IP (private block stays on)"
RESULT=$("$CLI_BINARY" -allowed-hosts=127.0.0.1 -allowPrivateIPsForAllowedHosts -default-url "http://127.0.0.1:18080/" "$TEST_DIR/allowed-host.html" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello World"; then
	pass "Bypassed private-IP block for allowed host"
else
	fail "Bypass for allowed host" "Expected include resolved, got: $RESULT"
fi

echo "Test 22: without -allowPrivateIPsForAllowedHosts the private host stays blocked"
RESULT=$("$CLI_BINARY" -allowed-hosts=127.0.0.1 -default-url "http://127.0.0.1:18080/" "$TEST_DIR/allowed-host.html" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello World"; then
	fail "Control (no bypass flag)" "Expected blocked include, got: $RESULT"
else
	pass "Private host still blocked without the bypass flag"
fi

echo "Test 23: -allowPrivateIPsForAllowedHosts does not bypass for hosts outside allowedHosts"
RESULT=$("$CLI_BINARY" -allowed-hosts=other.example.com -allowPrivateIPsForAllowedHosts -default-url "http://127.0.0.1:18080/" "$TEST_DIR/allowed-host.html" 2>/dev/null)
if echo "$RESULT" | grep -q "Hello World"; then
	fail "Unlisted host with bypass flag" "Expected blocked include, got: $RESULT"
else
	pass "Unlisted host stays blocked with the bypass flag"
fi

echo ""
echo "--- Max Response Size Tests ---"

cat > "$TEST_DIR/maxrs-include.html" <<'EOF'
<html><body><esi:include src="bytes/200"/></body></html>
EOF

cat > "$TEST_DIR/maxrs-default-cap.html" <<'EOF'
<html><body><esi:include src="bytes/10485761"/></body></html>
EOF

echo "Test 24: -max-response-size 100 rejects a 200-byte include"
RESULT=$("$CLI_BINARY" -max-response-size=100 -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/maxrs-include.html" 2>/dev/null)
if echo "$RESULT" | grep -q "MesiBytesPayload"; then
	fail "max-response-size 100 rejects 200-byte include" "Expected over-limit include to render empty, got: $RESULT"
else
	pass "200-byte include rejected under -max-response-size 100"
fi

echo "Test 25: -max-response-size 1024 accepts a 200-byte include"
RESULT=$("$CLI_BINARY" -max-response-size=1024 -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/maxrs-include.html" 2>/dev/null)
if echo "$RESULT" | grep -q "MesiBytesPayload"; then
	pass "200-byte include accepted under -max-response-size 1024"
else
	fail "max-response-size 1024 accepts 200-byte include" "Result: $RESULT"
fi

echo "Test 26: absent -max-response-size keeps the 10 MB default"
RESULT=$("$CLI_BINARY" -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/maxrs-include.html" 2>/dev/null)
if echo "$RESULT" | grep -q "MesiBytesPayload"; then
	pass "Absent flag keeps the 10 MB CreateDefaultConfig default (200-byte include accepted)"
else
	fail "Absent -max-response-size default" "Result: $RESULT"
fi
# Discriminating half: a 10 MB + 1 body must be REJECTED with the flag
# absent — this is what actually pins the 10 MB default end-to-end (a
# regression to 0 = unlimited would deliver it; a larger cap would too).
CAP_RESULT=$("$CLI_BINARY" -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/maxrs-default-cap.html" 2>/dev/null)
if echo "$CAP_RESULT" | grep -q "MesiBytesPayload"; then
	fail "Absent -max-response-size pins the 10 MB cap" "Expected 10 MB + 1 include to be rejected, got: $CAP_RESULT"
else
	pass "Absent flag rejects a 10 MB + 1 include (pins the 10 MB default end-to-end)"
fi

echo "Test 27: -max-response-size=-1 is rejected"
set +e
OVER_ERR=$("$CLI_BINARY" -max-response-size=-1 "$ROOT_DIR/tests/fixtures/05-comment.html" 2>&1)
OVER_CODE=$?
set -e
if [ "$OVER_CODE" -ne 0 ] && echo "$OVER_ERR" | grep -q "max-response-size"; then
	pass "max-response-size=-1 rejected"
else
	fail "max-response-size=-1 reject" "exit=$OVER_CODE err=$OVER_ERR"
fi

echo ""
echo "--- Max Concurrent Requests Tests ---"

# Deterministic peak-concurrency tracker (#192): tests/server serves
# /hold/<millis>/<label> — it increments a server-side peak counter at
# request start, sleeps <millis>, then returns a "<label> Held <millis>"
# fragment; /track/reset and /track/max zero/read the counter (mirrored
# from servers/apache/tests/server.py's endpoints added for #170). The
# peak is an observable counter, not a wall-clock assertion. Each page
# fans out to 20 DISTINCT labels so every include reaches the counter;
# the CLI's cache is off by default (no -cache-backend), so no dedup can
# swallow fetches either.
#
# Fan-out bound for the "unlimited" cases: MESIParse drains includes
# through a worker pool of min(MaxWorkers=NumCPU*4, 20) goroutines
# (mesi/parser.go), i.e. at least 4 on any machine — with 1500 ms holds,
# an uncapped parse must show peak >= 4, while a cap of 3 can never
# exceed 3 (hard semaphore invariant, mesi/fetch.go:148), and >= 2
# proves the cap is a multi-slot queue rather than a serialisation to 1
# (an exact peak == 3 would require all three first-wave dials to
# overlap — scheduling-dependent, deliberately not asserted, same as
# Apache Test 36).
#
# -timeout 60: the semaphore wait is bounded by the per-include fetch
# budget counted from fetch ENTRY (context.WithTimeout before the
# admission wait, mesi/fetch.go:139-154). Under the cap the first-wave
# waiters hold a budget started at t≈0 while later waves still queue
# behind 1500 ms holds (~10.5 s end-to-end) — only ~1 s of margin
# against the default 10 s, so an explicit budget removes timing
# sensitivity on slow CI runners.

cat > "$TEST_DIR/concurrent-20.html" <<'EOF'
<html><body>
<esi:include src="hold/1500/label1"/><esi:include src="hold/1500/label2"/><esi:include src="hold/1500/label3"/><esi:include src="hold/1500/label4"/><esi:include src="hold/1500/label5"/>
<esi:include src="hold/1500/label6"/><esi:include src="hold/1500/label7"/><esi:include src="hold/1500/label8"/><esi:include src="hold/1500/label9"/><esi:include src="hold/1500/label10"/>
<esi:include src="hold/1500/label11"/><esi:include src="hold/1500/label12"/><esi:include src="hold/1500/label13"/><esi:include src="hold/1500/label14"/><esi:include src="hold/1500/label15"/>
<esi:include src="hold/1500/label16"/><esi:include src="hold/1500/label17"/><esi:include src="hold/1500/label18"/><esi:include src="hold/1500/label19"/><esi:include src="hold/1500/label20"/>
</body></html>
EOF

echo "Test 28: -max-concurrent-requests 3 funnels 20 includes (peak <= 3, all delivered)"
curl -s "http://127.0.0.1:18080/track/reset" > /dev/null
RESULT=$("$CLI_BINARY" -max-concurrent-requests=3 -timeout 60 -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/concurrent-20.html" 2>/dev/null)
PEAK=$(curl -s "http://127.0.0.1:18080/track/max" || true)
FRAGMENTS=$(echo "$RESULT" | grep -o "Held 1500" | wc -l | tr -d ' ' || true)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -ge 2 ] && [ "$PEAK" -le 3 ] && ! echo "$RESULT" | grep -q '<esi:include'; then
	pass "cap 3 funneled: peak=$PEAK (<= 3 cap, >= 2 parallel slots), 20/20 fragments queued and delivered"
else
	fail "max-concurrent-requests 3 funnel" "peak=$PEAK fragments=$FRAGMENTS (expected peak in [2,3], 20 fragments)"
fi

echo "Test 29: absent -max-concurrent-requests is unlimited (peak >= 4)"
curl -s "http://127.0.0.1:18080/track/reset" > /dev/null
RESULT=$("$CLI_BINARY" -timeout 60 -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/concurrent-20.html" 2>/dev/null)
PEAK=$(curl -s "http://127.0.0.1:18080/track/max" || true)
FRAGMENTS=$(echo "$RESULT" | grep -o "Held 1500" | wc -l | tr -d ' ' || true)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -ge 4 ] && ! echo "$RESULT" | grep -q '<esi:include'; then
	pass "absent flag unlimited: peak=$PEAK >= 4, 20/20 fragments delivered"
else
	fail "absent -max-concurrent-requests default" "peak=$PEAK fragments=$FRAGMENTS (expected peak >= 4, 20 fragments)"
fi

echo "Test 30: explicit -max-concurrent-requests 0 is unlimited (peak >= 4)"
curl -s "http://127.0.0.1:18080/track/reset" > /dev/null
RESULT=$("$CLI_BINARY" -max-concurrent-requests=0 -timeout 60 -allow-private-ips -default-url "http://127.0.0.1:18080/" "$TEST_DIR/concurrent-20.html" 2>/dev/null)
PEAK=$(curl -s "http://127.0.0.1:18080/track/max" || true)
FRAGMENTS=$(echo "$RESULT" | grep -o "Held 1500" | wc -l | tr -d ' ' || true)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -ge 4 ] && ! echo "$RESULT" | grep -q '<esi:include'; then
	pass "explicit 0 unlimited: peak=$PEAK >= 4, 20/20 fragments delivered"
else
	fail "explicit -max-concurrent-requests 0" "peak=$PEAK fragments=$FRAGMENTS (expected peak >= 4, 20 fragments)"
fi

echo "Test 31: -max-concurrent-requests=-1 is rejected"
set +e
OVER_ERR=$("$CLI_BINARY" -max-concurrent-requests=-1 "$ROOT_DIR/tests/fixtures/05-comment.html" 2>&1)
OVER_CODE=$?
set -e
if [ "$OVER_CODE" -ne 0 ] && echo "$OVER_ERR" | grep -q "max-concurrent-requests"; then
	pass "max-concurrent-requests=-1 rejected"
else
	fail "max-concurrent-requests=-1 reject" "exit=$OVER_CODE err=$OVER_ERR"
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
