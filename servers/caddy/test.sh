#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

HTTP_PORT=${CADDY_HTTP_PORT:-18080}
PARSE_ON_HEADER_PORT=${CADDY_PARSE_ON_HEADER_PORT:-18081}

docker compose up -d --build

echo "Waiting for services to be ready..."
for i in $(seq 1 30); do
    if curl -s -o /dev/null http://localhost:"$HTTP_PORT"/ 2>/dev/null; then
        echo "Services ready after ${i}s"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "FAIL: Services did not become ready within 30s"
        docker compose logs
        docker compose down
        exit 1
    fi
    sleep 1
done

echo "=== Test 1: ESI include processing ==="
RESPONSE=$(curl -s http://localhost:"$HTTP_PORT"/)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: ESI include processed correctly"
else
    echo "FAIL: ESI include not processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 2: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:"$HTTP_PORT"/)
if echo "$RESPONSE" | grep -q "Welcome to ESI Test"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 3: ESI remove ==="
RESPONSE=$(curl -s http://localhost:"$HTTP_PORT"/)
if echo "$RESPONSE" | grep -q "Failed to include ESI"; then
    echo "FAIL: ESI remove content still present"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
else
    echo "PASS: ESI remove processed correctly"
fi

echo "=== Test 4: Surrogate-Capability header ==="
HEADERS=$(curl -sI http://localhost:"$HTTP_PORT"/)
if echo "$HEADERS" | grep -q "Surrogate-Capability"; then
    echo "PASS: Surrogate-Capability header present"
else
    echo "FAIL: Surrogate-Capability header missing"
    echo "Headers: $HEADERS"
    docker compose down
    exit 1
fi

echo "=== Test 5: Non-HTML content bypass ==="
RESPONSE=$(curl -s http://localhost:"$HTTP_PORT"/plain)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Plain text content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: Plain text content was processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 6: Content-Length correctness ==="
HEADERS=$(curl -sD - http://localhost:"$HTTP_PORT"/ -o /tmp/mesi-caddy-response.txt 2>/dev/null)
ACTUAL_BODY_SIZE=$(wc -c < /tmp/mesi-caddy-response.txt)
HEADER_CL=$(echo "$HEADERS" | grep -i "Content-Length" | awk '{print $2}' | tr -d '\r')
if [ -n "$HEADER_CL" ]; then
    if [ "$HEADER_CL" -eq "$ACTUAL_BODY_SIZE" ] 2>/dev/null; then
        echo "PASS: Content-Length ($HEADER_CL) matches actual body size ($ACTUAL_BODY_SIZE)"
    else
        echo "FAIL: Content-Length ($HEADER_CL) != body size ($ACTUAL_BODY_SIZE)"
        docker compose down
        exit 1
    fi
else
    echo "PASS: Content-Length correctly absent (truncated after ESI processing)"
fi
rm -f /tmp/mesi-caddy-response.txt

echo "=== Test 7: Content-Type preserved ==="
CT=$(curl -sI http://localhost:"$HTTP_PORT"/ | grep -i "Content-Type")
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Content-Type missing or wrong"
    echo "Content-Type: $CT"
    docker compose down
    exit 1
fi

echo "=== Test 8: parse_on_header with Edge-control processes ESI ==="
RESPONSE=$(curl -s http://localhost:"$PARSE_ON_HEADER_PORT"/returnEsi)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: parse_on_header processed a signalled page"
else
    echo "FAIL: parse_on_header did not process a signalled page"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 9: parse_on_header without Edge-control passes through ==="
RESPONSE=$(curl -s http://localhost:"$PARSE_ON_HEADER_PORT"/returnNonEsiHeader)
if echo "$RESPONSE" | grep -q "<esi:include" && ! echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: parse_on_header left an unsignalled page unchanged"
else
    echo "FAIL: parse_on_header processed a page without Edge-control"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi
HEADERS=$(curl -s -D - -o /dev/null http://localhost:"$PARSE_ON_HEADER_PORT"/returnNonEsiHeader)
if echo "$HEADERS" | grep -qi "^Surrogate-Control"; then
    echo "FAIL: a bypassed page must not get Surrogate-Control"
    echo "Headers: $HEADERS"
    docker compose down
    exit 1
else
    echo "PASS: no Surrogate-Control on a bypassed page"
fi

echo "=== Test 10: parse_on_header absent still parses HTML without Edge-control ==="
RESPONSE=$(curl -s http://localhost:"$HTTP_PORT"/returnNonEsiHeader)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: the directive absent keeps the always-parse behaviour"
else
    echo "FAIL: HTML without Edge-control was not parsed without the directive"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 11: parse_on_header leaves non-HTML untouched ==="
RESPONSE=$(curl -s http://localhost:"$PARSE_ON_HEADER_PORT"/plain)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: non-HTML response bypassed ESI processing"
else
    echo "FAIL: non-HTML response was processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

docker compose down

echo ""
echo "=== All Caddy tests passed ==="
