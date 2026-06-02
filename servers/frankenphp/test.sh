#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

docker compose up -d --build

echo "Waiting for FrankenPHP to be ready..."
for i in $(seq 1 60); do
    if curl -s -o /dev/null http://localhost:8080/index.html 2>/dev/null; then
        echo "FrankenPHP ready after ${i}s"
        break
    fi
    if [ "$i" -eq 60 ]; then
        echo "FAIL: FrankenPHP did not become ready within 60s"
        docker compose logs
        docker compose down
        exit 1
    fi
    sleep 2
done

echo "=== Test 1: Module loaded ==="
MODULES=$(docker compose exec frankenphp frankenphp list-modules 2>&1)
if echo "$MODULES" | grep -q "http.handlers.mesi"; then
    echo "PASS: http.handlers.mesi module loaded"
else
    echo "FAIL: mesi module not found"
    echo "Modules: $MODULES"
    docker compose down
    exit 1
fi

echo "=== Test 2: ESI include in static HTML ==="
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: ESI include processed correctly in static HTML"
else
    echo "FAIL: ESI include not processed in static HTML"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 3: ESI include in PHP-generated HTML ==="
RESPONSE=$(curl -s http://localhost:8080/esi.php)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: ESI include processed correctly in PHP-generated HTML"
else
    echo "FAIL: ESI include not processed in PHP-generated HTML"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 4: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "Unwrapped content"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 5: ESI remove ==="
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "Failed to include ESI"; then
    echo "FAIL: ESI remove content still present"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
else
    echo "PASS: ESI remove processed correctly"
fi

echo "=== Test 6: Surrogate-Capability header ==="
HEADERS=$(curl -sI http://localhost:8080/proxy/)
if echo "$HEADERS" | grep -q "Surrogate-Capability"; then
    echo "PASS: Surrogate-Capability header present"
else
    echo "FAIL: Surrogate-Capability header missing"
    echo "Headers: $HEADERS"
    docker compose down
    exit 1
fi

echo "=== Test 7: Non-HTML content bypass (PHP-generated) ==="
RESPONSE=$(curl -s http://localhost:8080/plain.php)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: text/plain content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: text/plain content was processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 8: Content-Length correctness ==="
HEADERS=$(curl -sD - http://localhost:8080/index.html -o /tmp/mesi-frankenphp-response.txt 2>/dev/null)
ACTUAL_BODY_SIZE=$(wc -c < /tmp/mesi-frankenphp-response.txt)
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
rm -f /tmp/mesi-frankenphp-response.txt

echo "=== Test 9: Content-Type preserved ==="
CT_HEADERS=$(curl -sI http://localhost:8080/esi.php)
if echo "$CT_HEADERS" | grep -qi "text/html"; then
    echo "PASS: Content-Type is text/html for PHP-generated HTML"
else
    echo "FAIL: Content-Type missing or wrong for PHP-generated HTML"
    echo "Content-Type: $CT_HEADERS"
    docker compose down
    exit 1
fi

echo "=== Test 10: Non-HTML JSON response preserved ==="
RESPONSE=$(curl -s http://localhost:8080/esi-non-html.php)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: JSON content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: JSON content was processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

docker compose down

echo ""
echo "=== All FrankenPHP tests passed ==="
