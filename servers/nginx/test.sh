#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

docker compose up -d --wait

echo "=== Test 1: ESI include processing ==="
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: ESI include processed"
else
    echo "FAIL: ESI include not processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 2: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:8080/comment.html)
if echo "$RESPONSE" | grep -q "ESI comment unwrapped content"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 3: ESI remove ==="
RESPONSE=$(curl -s http://localhost:8080/remove.html)
if echo "$RESPONSE" | grep -q "After remove"; then
    if echo "$RESPONSE" | grep -q "This should be removed"; then
        echo "FAIL: ESI remove content still present"
        echo "Response: $RESPONSE"
        exit 1
    fi
    echo "PASS: ESI remove processed correctly"
else
    echo "FAIL: ESI remove test failed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 4: Surrogate-Capability header ==="
HEADERS=$(curl -sI http://localhost:8080/index.html)
if echo "$HEADERS" | grep -q "Surrogate-Capability"; then
    echo "PASS: Surrogate-Capability header present"
else
    echo "FAIL: Surrogate-Capability header missing"
    echo "Headers: $HEADERS"
    exit 1
fi

echo "=== Test 5: Non-HTML content (text/plain) ==="
RESPONSE=$(curl -s http://localhost:8080/noesi.txt)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Plain text content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: Plain text content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 6: Content-Length correctness ==="
HEADERS=$(curl -s -D - http://localhost:8080/index.html -o /tmp/mesi-response-body.txt 2>/dev/null)
ACTUAL_BODY_SIZE=$(wc -c < /tmp/mesi-response-body.txt)
HEADER_CL=$(echo "$HEADERS" | grep -i "Content-Length" | awk '{print $2}' | tr -d '\r')
if [ -n "$HEADER_CL" ]; then
    if [ "$HEADER_CL" -eq "$ACTUAL_BODY_SIZE" ] 2>/dev/null; then
        echo "PASS: Content-Length ($HEADER_CL) matches actual body size ($ACTUAL_BODY_SIZE)"
    else
        echo "FAIL: Content-Length ($HEADER_CL) != body size ($ACTUAL_BODY_SIZE)"
        exit 1
    fi
else
    echo "PASS: Content-Length correctly absent (truncated after ESI processing)"
fi
rm -f /tmp/mesi-response-body.txt

echo "=== Test 7: Nested ESI includes ==="
RESPONSE=$(curl -s http://localhost:8080/nested.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: Nested ESI include resolved correctly"
else
    echo "FAIL: Nested ESI include failed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 8: ESI include with fallback ==="
RESPONSE=$(curl -s http://localhost:8080/fallback.html)
if echo "$RESPONSE" | grep -q "fallback content rendered"; then
    echo "PASS: ESI fallback content used"
else
    echo "FAIL: ESI fallback not working"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 9: HTTP error passthrough (status >= 400) ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/nonexistent.html)
if [ "$STATUS" = "404" ]; then
    echo "PASS: HTTP 404 returned for nonexistent page"
else
    echo "FAIL: Expected 404, got $STATUS"
    exit 1
fi

echo "=== Test 10: JSON content (application/json) not processed ==="
RESPONSE=$(curl -s http://localhost:8080/noesi.json)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: JSON content not processed (raw esi:include preserved)"
else
    echo "FAIL: JSON content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi
CT=$(curl -sI http://localhost:8080/noesi.json | grep -i "Content-Type")
if echo "$CT" | grep -qi "application/json"; then
    echo "PASS: JSON Content-Type is application/json"
else
    echo "FAIL: JSON Content-Type is wrong: $CT"
    exit 1
fi

echo "=== Test 11: CSS content (text/css) not processed ==="
RESPONSE=$(curl -s http://localhost:8080/noesi.css)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: CSS content not processed (raw esi:include preserved)"
else
    echo "FAIL: CSS content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi
CT=$(curl -sI http://localhost:8080/noesi.css | grep -i "Content-Type")
if echo "$CT" | grep -qi "text/css"; then
    echo "PASS: CSS Content-Type is text/css"
else
    echo "FAIL: CSS Content-Type is wrong: $CT"
    exit 1
fi

echo "=== Test 12: Content-Type check ==="
CT=$(curl -sI http://localhost:8080/index.html | grep -i "Content-Type")
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Wrong Content-Type"
    echo "Content-Type: $CT"
    exit 1
fi

echo "=== Test 13: Cache hit in same page (two includes, same URL) ==="
RESPONSE=$(curl -s http://localhost:8080/cache/cache.html)
FIRST_NUM=$(echo "$RESPONSE" | grep -oE '[0-9]+' | head -1)
SECOND_NUM=$(echo "$RESPONSE" | grep -oE '[0-9]+' | tail -1)
if [ -n "$FIRST_NUM" ] && [ -n "$SECOND_NUM" ]; then
    if [ "$FIRST_NUM" = "$SECOND_NUM" ]; then
        echo "PASS: Both includes returned same value ($FIRST_NUM) — cache serving same entry"
    else
        echo "FAIL: Cache should serve same value for same URL (got $FIRST_NUM vs $SECOND_NUM)"
        echo "Response: $RESPONSE"
        exit 1
    fi
else
    echo "FAIL: Could not extract counter values from response"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 14: Cache hit across requests (within TTL) ==="
RESPONSE1=$(curl -s http://localhost:8080/cache/cache_ttl.html)
NUM1=$(echo "$RESPONSE1" | grep -oE '[0-9]+')
sleep 1
RESPONSE2=$(curl -s http://localhost:8080/cache/cache_ttl.html)
NUM2=$(echo "$RESPONSE2" | grep -oE '[0-9]+')
if [ -n "$NUM1" ] && [ -n "$NUM2" ]; then
    if [ "$NUM1" = "$NUM2" ]; then
        echo "PASS: Second request served from cache (both $NUM1)"
    else
        echo "FAIL: Cache miss — values differ ($NUM1 vs $NUM2)"
        echo "Response1: $RESPONSE1"
        echo "Response2: $RESPONSE2"
        exit 1
    fi
else
    echo "FAIL: Could not extract counter values"
    echo "Response1: $RESPONSE1"
    echo "Response2: $RESPONSE2"
    exit 1
fi

echo "=== Test 15: Cache backend unset — no caching ==="
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: ESI still works without cache backend configured"
else
    echo "FAIL: ESI processing broken in non-cache location"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 16: Memcached cache — same page, two includes, same URL ==="
# Start fresh nginx + memcached, no memory cache location to avoid
# the process-level cache_initialized global interfering.
docker compose up -d --wait

RESPONSE=$(curl -s http://localhost:8080/cache/memcached/cache_memcached.html)
FIRST_NUM=$(echo "$RESPONSE" | grep -oE '[0-9]+' | head -1)
SECOND_NUM=$(echo "$RESPONSE" | grep -oE '[0-9]+' | tail -1)
if [ -n "$FIRST_NUM" ] && [ -n "$SECOND_NUM" ]; then
    if [ "$FIRST_NUM" = "$SECOND_NUM" ]; then
        echo "PASS: Memcached cache — both includes returned same value ($FIRST_NUM)"
    else
        echo "FAIL: Memcached cache — values differ ($FIRST_NUM vs $SECOND_NUM)"
        echo "Response: $RESPONSE"
        exit 1
    fi
else
    echo "FAIL: Could not extract counter values from memcached response"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 17: Memcached cache — cross-request hit within TTL ==="
RESPONSE1=$(curl -s http://localhost:8080/cache/memcached/cache_ttl.html)
NUM1=$(echo "$RESPONSE1" | grep -oE '[0-9]+')
sleep 1
RESPONSE2=$(curl -s http://localhost:8080/cache/memcached/cache_ttl.html)
NUM2=$(echo "$RESPONSE2" | grep -oE '[0-9]+')
if [ -n "$NUM1" ] && [ -n "$NUM2" ]; then
    if [ "$NUM1" = "$NUM2" ]; then
        echo "PASS: Memcached cache — second request served from cache (both $NUM1)"
    else
        echo "FAIL: Memcached cache — cache miss across requests ($NUM1 vs $NUM2)"
        echo "Response1: $RESPONSE1"
        echo "Response2: $RESPONSE2"
        exit 1
    fi
else
    echo "FAIL: Could not extract counter values from memcached cache"
    echo "Response1: $RESPONSE1"
    echo "Response2: $RESPONSE2"
    exit 1
fi

docker compose down

docker compose down

echo ""
echo "=== All tests passed ==="
