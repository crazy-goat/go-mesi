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

echo "=== Starting RR test server on :9090 ==="
"$RR_TEST_BINARY" -listen :9090 &
RR_PID=$!
sleep 2

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

echo ""
echo "=== All RoadRunner tests passed ==="
