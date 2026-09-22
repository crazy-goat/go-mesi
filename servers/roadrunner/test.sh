#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TEST_DIR="$(mktemp -d)"
RR_TEST_BINARY="$TEST_DIR/rrtest"
RR_PID=""

cleanup() {
	echo "Cleaning up..."
	[ -n "$RR_PID" ] && kill "$RR_PID" 2>/dev/null || true
	sleep 1
	[ -n "$RR_PID" ] && kill -9 "$RR_PID" 2>/dev/null || true
	rm -rf "$TEST_DIR"
}

trap cleanup EXIT

echo "=== Building binary ==="
cd "$SCRIPT_DIR/cmd/rrtest" && go build -o "$RR_TEST_BINARY" .

# start_rr restarts the test server with the given flags so each case runs
# with its own plugin config.
start_rr() {
	[ -n "$RR_PID" ] && kill "$RR_PID" 2>/dev/null || true
	sleep 1
	[ -n "$RR_PID" ] && kill -9 "$RR_PID" 2>/dev/null || true
	"$RR_TEST_BINARY" -listen :9090 "$@" &
	RR_PID=$!
	sleep 2
}

cd "$SCRIPT_DIR"
echo "=== Starting RR test server on :9090 ==="
start_rr

echo "=== Test 1: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:9090/)
if echo "$RESPONSE" | grep -q "<h1>Welcome to ESI Test</h1>"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 2: ESI remove ==="
RESPONSE=$(curl -s http://localhost:9090/)
if echo "$RESPONSE" | grep -q "Failed to include ESI"; then
    echo "FAIL: ESI remove content still present"
    echo "Response: $RESPONSE"
    exit 1
else
    echo "PASS: ESI remove processed correctly"
fi

echo "=== Test 3: Non-HTML content bypass ==="
RESPONSE=$(curl -s http://localhost:9090/plain)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Plain text content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: Plain text content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 4: Content-Length correctness ==="
HEADERS=$(curl -sD - http://localhost:9090/ -o "$TEST_DIR/body.txt" 2>/dev/null)
ACTUAL_BODY_SIZE=$(wc -c < "$TEST_DIR/body.txt")
HEADER_CL=$(echo "$HEADERS" | grep -i "Content-Length" | awk '{print $2}' | tr -d '\r')
if [ -n "$HEADER_CL" ]; then
    if [ "$HEADER_CL" -eq "$ACTUAL_BODY_SIZE" ] 2>/dev/null; then
        echo "PASS: Content-Length ($HEADER_CL) matches actual body size ($ACTUAL_BODY_SIZE)"
    else
        echo "FAIL: Content-Length ($HEADER_CL) != body size ($ACTUAL_BODY_SIZE)"
        exit 1
    fi
else
    echo "FAIL: Content-Length header missing"
    exit 1
fi

echo "=== Test 5: Content-Type preserved ==="
CT=$(curl -sI http://localhost:9090/ | grep -i "Content-Type")
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Content-Type missing or wrong"
    echo "Content-Type: $CT"
    exit 1
fi

echo "--- Allowed Hosts Tests ---"

echo "=== Test 6: allowed_hosts allows include from listed host ==="
start_rr -allowed-hosts 127.0.0.1 -block-private-ips=false
RESPONSE=$(curl -s http://localhost:9090/allowed)
if echo "$RESPONSE" | grep -q "FRAGMENT_OK"; then
    echo "PASS: allowed_hosts include from listed host resolved"
else
    echo "FAIL: allowed_hosts include from listed host blocked"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 7: allowed_hosts blocks include from unlisted host ==="
start_rr -allowed-hosts other.example.com -block-private-ips=false
RESPONSE=$(curl -s http://localhost:9090/allowed)
if echo "$RESPONSE" | grep -q "FRAGMENT_OK"; then
    echo "FAIL: allowed_hosts include from unlisted host resolved"
    echo "Response: $RESPONSE"
    exit 1
else
    echo "PASS: allowed_hosts include from unlisted host blocked"
fi

echo "--- AllowPrivateIPsForAllowedHosts Tests ---"

echo "=== Test 8: allow_private_ips_for_allowed_hosts bypass enables listed private host ==="
start_rr -allowed-hosts 127.0.0.1 -allow-private-ips-for-allowed-hosts
RESPONSE=$(curl -s http://localhost:9090/allowed)
if echo "$RESPONSE" | grep -q "FRAGMENT_OK"; then
    echo "PASS: listed private host resolved with bypass enabled"
else
    echo "FAIL: listed private host blocked despite bypass enabled"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 9: allow_private_ips_for_allowed_hosts default off blocks listed private host ==="
start_rr -allowed-hosts 127.0.0.1
RESPONSE=$(curl -s http://localhost:9090/allowed)
if echo "$RESPONSE" | grep -q "FRAGMENT_OK"; then
    echo "FAIL: listed private host resolved with bypass default off"
    echo "Response: $RESPONSE"
    exit 1
else
    echo "PASS: listed private host blocked with bypass default off"
fi

echo "--- Max Depth Tests ---"

echo "=== Test 10: max_depth 1 — inner nest not processed (#183) ==="
# /nested-depth: depth 1 fetches the OUTER include (its visible marker
# proves the fetch happened), then re-parses the fragment with
# MaxDepth=0 (ParseOnly) — the inner tag goes through the include-error
# path and is replaced with the empty default marker (never fetched,
# never left raw). Same contract as nginx Test 38 / Apache Test 27.
start_rr -block-private-ips=false -max-depth 1
RESPONSE=$(curl -s http://localhost:9090/nested-depth)
if echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: max_depth 1 fetched the outer include and stripped the inner tag"
else
    echo "FAIL: max_depth 1 did not match the depth-1 contract (outer body, no inner body, no leftover tag)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 11: max_depth 5 (explicit) — both nest levels processed (#183) ==="
start_rr -block-private-ips=false -max-depth 5
RESPONSE=$(curl -s http://localhost:9090/nested-depth)
if echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: explicit max_depth 5 processed both nest levels"
else
    echo "FAIL: explicit max_depth 5 did not process both nest levels"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 12: max_depth unset → 5 (backward compat, #183) ==="
# No -max-depth flag: the plugin default (5) must keep behaving exactly
# like the historical hardcoded 5 — both nest levels processed (the
# marker assertions from Test 11, now covering the omitted-flag path).
start_rr -block-private-ips=false
RESPONSE=$(curl -s http://localhost:9090/nested-depth)
if echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY"; then
    echo "PASS: unset max_depth defaults to 5 (both nest levels processed)"
else
    echo "FAIL: unset max_depth no longer behaves like depth 5"
    echo "Response: $RESPONSE"
    exit 1
fi

echo ""
echo "=== All RoadRunner tests passed ==="
