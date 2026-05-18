#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TEST_DIR="$(mktemp -d)"
PROXY_BINARY="$TEST_DIR/mesi-proxy-test"
TEST_SERVER_BINARY="$TEST_DIR/mesi-test-server"
PROXY_PID=""
SERVER_PID=""

cleanup() {
	echo "Cleaning up..."
	[ -n "$PROXY_PID" ] && kill "$PROXY_PID" 2>/dev/null || true
	[ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
	sleep 1
	[ -n "$PROXY_PID" ] && kill -9 "$PROXY_PID" 2>/dev/null || true
	[ -n "$SERVER_PID" ] && kill -9 "$SERVER_PID" 2>/dev/null || true
	rm -rf "$TEST_DIR"
}

start_proxy() {
	local extra_flags="$1"
	[ -n "$PROXY_PID" ] && kill "$PROXY_PID" 2>/dev/null || true
	sleep 2
	"$PROXY_BINARY" --listen ":$PROXY_PORT" --backend "http://localhost:$TEST_SERVER_PORT" $extra_flags &
	PROXY_PID=$!
	sleep 1
}

trap cleanup EXIT

TEST_SERVER_PORT=8080
PROXY_PORT=9090

echo "=== Building binaries ==="
go build -o "$PROXY_BINARY" "$SCRIPT_DIR"
go build -o "$TEST_SERVER_BINARY" "$SCRIPT_DIR/../../tests/server"

echo "=== Starting test server on :$TEST_SERVER_PORT ==="
"$TEST_SERVER_BINARY" &
SERVER_PID=$!
sleep 1

echo "=== Starting proxy on :$PROXY_PORT -> http://localhost:$TEST_SERVER_PORT ==="
start_proxy "--block-private-ips=false"

echo ""
echo "=== Test 1: ESI include processing ==="
RESPONSE=$(curl -s "http://localhost:$PROXY_PORT/returnEsi")
if echo "$RESPONSE" | grep -q "Hello World"; then
	echo "PASS: ESI include processed"
else
	echo "FAIL: ESI include not processed"
	echo "Response: $RESPONSE"
	exit 1
fi

echo ""
echo "=== Test 2: ParseOnHeader bypass (no Edge-control) ==="
start_proxy "--parse-on-header --block-private-ips=false"
RESPONSE=$(curl -s "http://localhost:$PROXY_PORT/returnNonEsiHeader")
if echo "$RESPONSE" | grep -q "<esi:include"; then
	echo "PASS: ParseOnHeader bypass works (raw ESI preserved)"
else
	echo "FAIL: ParseOnHeader bypass failed"
	echo "Response: $RESPONSE"
	exit 1
fi

echo ""
echo "=== Test 3: ParseOnHeader active (Edge-control present) ==="
start_proxy "--parse-on-header --block-private-ips=false"
RESPONSE=$(curl -s "http://localhost:$PROXY_PORT/returnEsi")
if echo "$RESPONSE" | grep -q "Hello World"; then
	echo "PASS: ParseOnHeader active - ESI processed"
else
	echo "FAIL: ParseOnHeader active - ESI not processed"
	echo "Response: $RESPONSE"
	exit 1
fi

echo ""
echo "=== Test 4: Surrogate headers ==="
start_proxy "--block-private-ips=false"
HEADERS=$(curl -sI "http://localhost:$PROXY_PORT/returnEsi")
if echo "$HEADERS" | grep -qi "Surrogate-Control"; then
	echo "PASS: Surrogate-Control header in response for processed HTML"
else
	echo "FAIL: Surrogate-Control header missing"
	echo "Headers: $HEADERS"
	exit 1
fi

echo ""
echo "=== Test 5: Non-HTML passthrough ==="
start_proxy "--block-private-ips=false"
RESPONSE=$(curl -s "http://localhost:$PROXY_PORT/returnString/test.txt")
if echo "$RESPONSE" | grep -q "test.txt"; then
	echo "PASS: Non-HTML passthrough works"
else
	echo "FAIL: Non-HTML passthrough failed"
	echo "Response: $RESPONSE"
	exit 1
fi

echo ""
echo "=== Test 6: Max depth ==="
start_proxy "--max-depth 1 --block-private-ips=false"
RESPONSE=$(curl -s "http://localhost:$PROXY_PORT/recursive")
COUNT=$(echo "$RESPONSE" | grep -o "included:" | wc -l)
if [ "$COUNT" -le 2 ]; then
	echo "PASS: Max depth 1 limited recursion (got $COUNT levels)"
else
	echo "FAIL: Max depth 1 allowed $COUNT recursion levels"
	echo "Response: $RESPONSE"
	exit 1
fi

echo ""
echo "=== Test 7: Timeout ==="
start_proxy "--timeout 1 --block-private-ips=false"
START=$(date +%s%N)
set +e
RESPONSE=$(curl -s --max-time 10 "http://localhost:$PROXY_PORT/returnEsi?slow=5" 2>&1)
set -e
END=$(date +%s%N)
DURATION_MS=$(( (END - START) / 1000000 ))
if [ "$DURATION_MS" -lt 5000 ]; then
	echo "PASS: ESI timeout triggered (completed in ${DURATION_MS}ms)"
else
	echo "FAIL: ESI timeout not triggered (took ${DURATION_MS}ms)"
	exit 1
fi

echo ""
echo "=== Test 8: Error passthrough (404) ==="
start_proxy "--block-private-ips=false"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:$PROXY_PORT/status/code/404")
if [ "$STATUS" = "404" ]; then
	echo "PASS: HTTP 404 passed through correctly"
else
	echo "FAIL: Expected 404, got $STATUS"
	exit 1
fi

echo ""
echo "=== Test 9: Content-Length correctness ==="
start_proxy "--block-private-ips=false"
HEADERS=$(curl -s -D - "http://localhost:$PROXY_PORT/returnEsi" -o "$TEST_DIR/body.txt" 2>/dev/null)
BODY_SIZE=$(wc -c < "$TEST_DIR/body.txt")
HEADER_CL=$(echo "$HEADERS" | grep -i "Content-Length" | awk '{print $2}' | tr -d '\r')
if [ -n "$HEADER_CL" ]; then
	if [ "$HEADER_CL" -eq "$BODY_SIZE" ] 2>/dev/null; then
		echo "PASS: Content-Length ($HEADER_CL) matches body size ($BODY_SIZE)"
	else
		echo "FAIL: Content-Length ($HEADER_CL) != body size ($BODY_SIZE)"
		exit 1
	fi
else
	echo "FAIL: Content-Length header missing"
	exit 1
fi

echo ""
echo "=== All tests passed ==="
