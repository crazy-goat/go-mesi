#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

docker compose up -d --build

sleep 5

echo "=== Test 1: Simple ESI include ==="
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: ESI include processed"
else
    echo "FAIL: ESI include not processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 2: Surrogate-Capability header ==="
HEADERS=$(curl -sI http://localhost:8080/index.html)
if echo "$HEADERS" | grep -q "Surrogate-Capability"; then
    echo "PASS: Surrogate-Capability header present"
else
    echo "FAIL: Surrogate-Capability header missing"
    echo "Headers: $HEADERS"
    exit 1
fi

echo "=== Test 3: Non-HTML content ==="
RESPONSE=$(curl -s http://localhost:8080/noesi.txt)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Non-HTML content not processed"
else
    echo "FAIL: Non-HTML content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 4: Content-Type check ==="
CT=$(curl -sI http://localhost:8080/index.html | grep -i "Content-Type")
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Wrong Content-Type"
    echo "Content-Type: $CT"
    exit 1
fi

echo "=== Test 5: AllowedHosts - allowed host (backend) ==="
RESPONSE=$(curl -s http://localhost:8080/ssrf-allowed.html)
if echo "$RESPONSE" | grep -q "allowed content"; then
    echo "PASS: Include from allowed host (backend) succeeded"
else
    echo "FAIL: Include from allowed host failed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 6: AllowedHosts - blocked host (evil.com) ==="
# The include to evil.com should be blocked
if echo "$RESPONSE" | grep -q "blocked.txt"; then
    echo "FAIL: Include from non-allowed host was NOT blocked"
    echo "Response: $RESPONSE"
    exit 1
else
    echo "PASS: Include from non-allowed host blocked"
fi

echo "=== Test 7: Large response (multi-brigade) - direct ==="
RESPONSE=$(curl -s http://localhost:8080/large.html)
if echo "$RESPONSE" | grep -q "After include"; then
    PASS_LARGE=1
    echo "PASS: Large response ESI include processed (direct)"
else
    PASS_LARGE=0
    echo "FAIL: Large response ESI include not processed (direct)"
    echo "Response length: $(echo "$RESPONSE" | wc -c)"
    echo "Response (first 500 chars): $(echo "$RESPONSE" | head -c 500)"
    # Continue to collect more diagnostic info before failing
fi

echo "=== Test 8: Large response (multi-brigade) - via ProxyPass ==="
RESPONSE=$(curl -s http://localhost:8080/backend/large.html)
if echo "$RESPONSE" | grep -q "allowed content"; then
    PASS_PROXY=1
    echo "PASS: Large response ESI include processed (proxied)"
else
    PASS_PROXY=0
    echo "FAIL: Large response ESI include not processed (proxied)"
    echo "Response length: $(echo "$RESPONSE" | wc -c)"
    echo "Response (first 500 chars): $(echo "$RESPONSE" | head -c 500)"
fi

# Fail now if either large test failed (after collecting both results)
if [ "$PASS_LARGE" -eq 0 ]; then exit 1; fi
if [ "$PASS_PROXY" -eq 0 ]; then exit 1; fi

echo "=== Test 9: Content-Type preserved after ESI processing ==="
CT=$(curl -s -D - http://localhost:8080/large.html -o /dev/null 2>/dev/null | grep -i "Content-Type" || true)
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Content-Type missing or wrong"
    echo "Headers:"
    curl -s -D - http://localhost:8080/large.html -o /dev/null
    exit 1
fi

echo "=== Test 10: Large response body size matches (no truncation) ==="
BODY_SIZE=$(curl -s http://localhost:8080/large.html | wc -c)
# Body should be at least the original file size (100KB) since ESI include adds content
if [ "$BODY_SIZE" -gt 102000 ]; then
    echo "PASS: Large response body is $BODY_SIZE bytes (expected > 102000)"
else
    echo "FAIL: Large response body is only $BODY_SIZE bytes (truncation?)"
    exit 1
fi

docker compose down

echo ""
echo "=== All tests passed ==="
