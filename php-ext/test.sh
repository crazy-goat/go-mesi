#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "Building and starting services..."
docker compose up -d --build

echo "Waiting for PHP extension server to be ready..."
for i in $(seq 1 60); do
    if curl -s -o /dev/null http://localhost:8080/health 2>/dev/null; then
        echo "PHP extension server ready after ${i}s"
        break
    fi
    if [ "$i" -eq 60 ]; then
        echo "FAIL: PHP extension server did not become ready within 60s"
        docker compose logs php-ext
        docker compose down
        exit 1
    fi
    sleep 2
done

echo ""
echo "=== Test 1: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:8080/)
if echo "$RESPONSE" | grep -q "Unwrapped content"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 2: ESI include ==="
RESPONSE=$(curl -s http://localhost:8080/)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: ESI include processed correctly"
else
    echo "FAIL: ESI include not processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 3: ESI remove ==="
RESPONSE=$(curl -s http://localhost:8080/)
if echo "$RESPONSE" | grep -q "Failed to include ESI"; then
    echo "FAIL: ESI remove content still present"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
else
    echo "PASS: ESI remove processed correctly"
fi

echo ""
echo "=== Test 4: ESI remove (dedicated route) ==="
RESPONSE=$(curl -s http://localhost:8080/remove)
if echo "$RESPONSE" | grep -q "remove this"; then
    echo "FAIL: ESI remove content still present in dedicated route"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi
if echo "$RESPONSE" | grep -q "keep this" && echo "$RESPONSE" | grep -q "also keep this"; then
    echo "PASS: ESI remove processed correctly, kept content preserved"
else
    echo "FAIL: Kept content missing after ESI remove"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 5: Non-HTML content bypass ==="
RESPONSE=$(curl -s http://localhost:8080/plain)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: text/plain content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: text/plain content was processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 6: JSON content bypass ==="
RESPONSE=$(curl -s http://localhost:8080/json)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: JSON content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: JSON content was processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 7: Content-Type preserved ==="
HEADERS=$(curl -sI http://localhost:8080/)
if echo "$HEADERS" | grep -qi "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Content-Type missing or wrong"
    echo "Headers: $HEADERS"
    docker compose down
    exit 1
fi

echo ""
echo "=== Test 8: Content-Length correctness ==="
HEADERS=$(curl -sD - http://localhost:8080/remove -o /tmp/mesi-php-ext-response.txt 2>/dev/null)
ACTUAL_BODY_SIZE=$(wc -c < /tmp/mesi-php-ext-response.txt)
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
    echo "PASS: Content-Length correctly absent (processed by PHP built-in server)"
fi
rm -f /tmp/mesi-php-ext-response.txt

docker compose down

echo ""
echo "=== All PHP extension tests passed ==="
