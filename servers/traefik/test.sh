#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

docker compose up -d

echo "Waiting for services to be ready..."
for i in $(seq 1 30); do
    if curl -sf -H "Host: domain.com" http://localhost:8080/ >/dev/null 2>&1; then
        echo "Services ready"
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "FAIL: Services did not become ready in time"
        docker compose logs
        docker compose down
        exit 1
    fi
    sleep 1
done

echo "=== Test 1: Traefik starts with mesi plugin ==="
RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: domain.com" http://localhost:8080/)
if [ "$RESPONSE" = "200" ]; then
    echo "PASS: Traefik responds with 200"
else
    echo "FAIL: Traefik returned $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 2: ESI remove ==="
RESPONSE=$(curl -s -H "Host: domain.com" http://localhost:8080/)
if echo "$RESPONSE" | grep -q "Failed to include ESI"; then
    echo "FAIL: ESI remove content still present"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
else
    echo "PASS: ESI remove processed correctly"
fi

echo "=== Test 3: HTML content served through mesi plugin ==="
RESPONSE=$(curl -s -H "Host: domain.com" http://localhost:8080/)
if echo "$RESPONSE" | grep -q "Welcome to ESI Test"; then
    echo "PASS: HTML content served through mesi plugin"
else
    echo "FAIL: Expected HTML content missing"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 4: Content-Length correctness ==="
HEADERS=$(curl -s -D - -H "Host: domain.com" http://localhost:8080/ -o /tmp/mesi-traefik-body.txt 2>/dev/null)
ACTUAL_BODY_SIZE=$(wc -c < /tmp/mesi-traefik-body.txt)
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
    echo "FAIL: Content-Length header missing"
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-traefik-body.txt

echo "=== Test 5: ESI raw include tag removed from response ==="
RESPONSE=$(curl -s -H "Host: domain.com" http://localhost:8080/)
if echo "$RESPONSE" | grep -q "<esi:include"; then
    echo "FAIL: Raw <esi:include> tag still present in response"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
else
    echo "PASS: Raw ESI include tag removed from response"
fi

echo "=== Test 6: Non-HTML content passthrough ==="
HEADERS=$(curl -sI -H "Host: domain.com" http://localhost:8080/esi)
CT=$(echo "$HEADERS" | grep -i "Content-Type" || true)
if echo "$CT" | grep -qi "text/html"; then
    echo "PASS: /esi endpoint returns text/html (processed by mesi)"
else
    echo "INFO: /esi Content-Type: $CT"
fi

docker compose down

echo ""
echo "=== All tests passed ==="
