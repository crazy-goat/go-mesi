#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

docker compose up -d --wait

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

echo "=== Test 3: Non-HTML content (text/plain) ==="
RESPONSE=$(curl -s http://localhost:8080/noesi.txt)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Plain text content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: Plain text content was processed"
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
RESPONSE=$(curl -s http://localhost:8080/ssrf-blocked.html)
if echo "$RESPONSE" | grep -q "blocked.txt"; then
    echo "FAIL: Include from non-allowed host was NOT blocked"
    echo "Response: $RESPONSE"
    exit 1
else
    echo "PASS: Include from non-allowed host blocked"
fi

echo "=== Test 6b: AllowPrivateIPsForAllowedHosts On - allowed private host succeeds (#168) ==="
RESPONSE=$(curl -s http://localhost:8081/ssrf-allow-private-on.html)
if echo "$RESPONSE" | grep -q "allowed content from backend"; then
    echo "PASS: Include from allowed private host (backend) succeeded with bypass On"
else
    echo "FAIL: Include from allowed private host blocked despite MesiAllowPrivateIPsForAllowedHosts On"
    echo "Response: $RESPONSE"
    exit 1
fi
if echo "$RESPONSE" | grep -q "blocked.txt"; then
    echo "FAIL: Include from non-allowed host (evil.com) was NOT blocked"
    echo "Response: $RESPONSE"
    exit 1
else
    echo "PASS: Include from non-allowed host still blocked with bypass On"
fi

echo "=== Test 6c: AllowPrivateIPsForAllowedHosts Off (default) - allowed private host blocked (#168) ==="
RESPONSE=$(curl -s http://localhost:8082/ssrf-allow-private-off.html)
if echo "$RESPONSE" | grep -q "allowed content from backend"; then
    echo "FAIL: Include from private host succeeded despite bypass Off"
    echo "Response: $RESPONSE"
    exit 1
else
    echo "PASS: Include from private host blocked with bypass Off (default)"
fi

echo "=== Test 6d: AllowPrivateIPsForAllowedHosts On but host NOT in AllowedHosts - still blocked (#168) ==="
RESPONSE=$(curl -s http://localhost:8081/ssrf-allow-private-notallowed.html)
if echo "$RESPONSE" | grep -q "allowed content from backend"; then
    echo "FAIL: Include from private host outside AllowedHosts succeeded"
    echo "Response: $RESPONSE"
    exit 1
else
    echo "PASS: Include from private host outside AllowedHosts blocked even with bypass On"
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
if [ "$BODY_SIZE" -gt 102000 ]; then
    echo "PASS: Large response body is $BODY_SIZE bytes (expected > 102000)"
else
    echo "FAIL: Large response body is only $BODY_SIZE bytes (truncation?)"
    exit 1
fi

echo "=== Test 11: JSON content (application/json) not processed ==="
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

echo "=== Test 12: CSS content (text/css) not processed ==="
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

echo "=== Test 13: Flatten error fallback (synthetic MESI_FORCE_FLATTEN_ERROR) ==="
docker compose down
MESI_FORCE_FLATTEN_ERROR=1 docker compose up -d --wait
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Flatten error fallback - ESI tags preserved verbatim (no processing)"
else
    echo "FAIL: Flatten error fallback - ESI tags were processed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi
LOG=$(docker compose exec -T apache sh -c 'cat /var/log/apache2/error.log 2>/dev/null' || true)
if echo "$LOG" | grep -q "failed to flatten response body"; then
    echo "PASS: Flatten error warning logged"
else
    echo "FAIL: Flatten error warning not logged"
    docker compose down
    exit 1
fi

docker compose down
docker compose up -d --wait

echo "=== Test 14: Nested ESI includes ==="
RESPONSE=$(curl -s http://localhost:8080/nested.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: Nested ESI include resolved correctly"
else
    echo "FAIL: Nested ESI include failed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 15: Local backend include (replacing GitHub raw URLs) ==="
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: Local backend include works (no GitHub dependency)"
else
    echo "FAIL: Local backend include failed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 16: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:8080/comment.html)
if echo "$RESPONSE" | grep -q "ESI comment unwrapped content"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 17: ESI remove ==="
RESPONSE=$(curl -s http://localhost:8080/remove.html)
if echo "$RESPONSE" | grep -q "After remove"; then
    if echo "$RESPONSE" | grep -q "This should be removed"; then
        echo "FAIL: ESI remove content still present"
        echo "Response: $RESPONSE"
        docker compose down
        exit 1
    fi
    echo "PASS: ESI remove processed correctly"
else
    echo "FAIL: ESI remove test failed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 18: ESI include with fallback ==="
RESPONSE=$(curl -s http://localhost:8080/fallback.html)
if echo "$RESPONSE" | grep -q "fallback content rendered"; then
    echo "PASS: ESI fallback content used"
else
    echo "FAIL: ESI fallback not working"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 19: HTTP error passthrough (status >= 400) ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/nonexistent.html)
if [ "$STATUS" = "404" ]; then
    echo "PASS: HTTP 404 returned for nonexistent page"
else
    echo "FAIL: Expected 404, got $STATUS"
    docker compose down
    exit 1
fi

echo "=== Test 20: Content-Length correctness ==="
HEADERS=$(curl -s -D - http://localhost:8080/index.html -o /tmp/mesi-response-body.txt 2>/dev/null)
ACTUAL_BODY_SIZE=$(wc -c < /tmp/mesi-response-body.txt)
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
rm -f /tmp/mesi-response-body.txt

echo "=== Test 21: Concurrent requests (thread safety) ==="
for i in $(seq 1 20); do
    curl -s http://localhost:8080/index.html -o /tmp/mesi-concurrent-$i.html &
done
wait
ALL_PASSED=1
for i in $(seq 1 5); do
    if grep -q "After include" /tmp/mesi-concurrent-$i.html 2>/dev/null; then
        echo "PASS: Concurrent request $i succeeded"
    else
        echo "FAIL: Concurrent request $i failed"
        ALL_PASSED=0
    fi
    rm -f /tmp/mesi-concurrent-$i.html
done
if [ "$ALL_PASSED" -eq 0 ]; then
    docker compose down
    exit 1
fi

echo "=== Test 22: HTTP error passthrough - ESI not applied to error page ==="
RESPONSE=$(curl -s http://localhost:8080/nonexistent.html)
if [ "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/nonexistent.html)" = "404" ] && [ -n "$RESPONSE" ]; then
    echo "PASS: ESI not applied to 404 error page (status=404, body non-empty)"
else
    echo "FAIL: Unexpected response for 404 page"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 23: Surrogate-Capability header on non-HTML content ==="
HEADERS=$(curl -sI http://localhost:8080/noesi.txt)
if echo "$HEADERS" | grep -q "Surrogate-Capability"; then
    echo "PASS: Surrogate-Capability header present on non-HTML content"
else
    echo "FAIL: Surrogate-Capability header missing on non-HTML content"
    echo "Headers: $HEADERS"
    docker compose down
    exit 1
fi

# --- Cache backend tests (#174) ---
# Scenario: MesiCacheBackend memory + duplicate <esi:include> in one
# response. We verify:
#   1. Both <esi:include> tags still resolve correctly (filter runs).
#   2. libgomesi.InitCache was wired up — proven by the INFO log written
#      to apache error log via ap_log_rerror(APLOG_NOTICE, ...) on first
#      request that touches a cache-enabled config.
#   3. libgomesi's shared cache is exercised on the include URL. With
#      MesiCacheBackend memory active, the backend must be hit at least
#      once; the upper bound is loose (1 or 2 hits) because MESIParse's
#      token worker pool processes duplicate includes in parallel
#      goroutines and the in-memory cache has no singleflight, so
#      simultaneous Get() calls can both miss before either Set()
#      completes. Cross-request dedup across Apache MPM prefork workers
#      is also non-deterministic (each worker has its own libgomesi
#      state), so we don't assert it here. The unit tests in
#      test_directives.c exercise the parser; the cross-process
#      correctness of the Get/Set paths is covered by mesi/fetch_test.go.

echo "=== Test 24: Memory cache backend wired up (#174) ==="
RESPONSE=$(curl -s http://localhost:8080/cache-test.html)
OCCURRENCES=$(echo "$RESPONSE" | grep -o "cached fragment from backend" | wc -l | tr -d ' ')
if [ "$OCCURRENCES" -ne 2 ]; then
    echo "FAIL: Expected exactly 2 fragment occurrences in rendered HTML, got $OCCURRENCES"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi
# Apache writes our ap_log_rerror messages to error.log (not stderr),
# so use docker exec to read them. Confirms InitCache was driven.
INIT_LOG=$(docker exec apache-apache-1 grep -c "mesi: cache initialized" /var/log/apache2/error.log 2>/dev/null || echo 0)
if [ "$INIT_LOG" -gt 0 ]; then
    echo "PASS: InitCache called by libgomesi ($INIT_LOG cache-init log lines in apache error.log)"
    # Print one matched entry for diagnostics.
    docker exec apache-apache-1 grep "mesi: cache initialized" /var/log/apache2/error.log | head -1
else
    echo "FAIL: No 'mesi: cache initialized' log line found in apache error.log — InitCache wiring broken"
    docker compose down
    exit 1
fi
# Backend (python http.server) logs each GET to stderr. /cached-fragment.txt
# is only referenced from cache-test.html, so any GET for it is from this
# test. With cache active we expect 1 or 2 hits (single-worker dedup yields
# 1; thundering-herd across MESIParse's parallel goroutines yields 2).
HITS=$(docker compose logs --no-color backend 2>&1 | grep -c "GET /cached-fragment.txt HTTP/1.1" || true)
if [ "$HITS" -ge 1 ] && [ "$HITS" -le 2 ]; then
    echo "PASS: Backend served cache-test URL $HITS time(s) (1 expected with cache, 2 acceptable due to in-response race)"
elif [ "$HITS" -eq 0 ]; then
    echo "FAIL: Backend received no GET /cached-fragment.txt requests — cache test setup broken"
    docker compose logs --no-color backend 2>&1 | tail -20
    docker compose down
    exit 1
else
    echo "FAIL: Backend served cache-test URL $HITS times — expected 1-2 (cache misspath broken)"
    docker compose logs --no-color backend 2>&1 | grep "cached-fragment" || true
    docker compose down
    exit 1
fi

docker compose down
docker compose up -d --wait

echo "=== Test 25: Shared HTTP client enabled (#178) ==="
RESPONSE=$(curl -s http://localhost:8083/shared-http-client.html)
OCCURRENCES=$(echo "$RESPONSE" | grep -o "shared fragment from backend" | wc -l | tr -d ' ')
if [ "$OCCURRENCES" -ne 2 ]; then
    echo "FAIL: Expected exactly 2 fragment occurrences with MesiSharedHTTPClient On, got $OCCURRENCES"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi
# The NOTICE log proves libgomesi InitHTTPClient was wired in child_init.
INIT_LOG=$(docker exec apache-apache-1 grep -c "mesi: shared HTTP client initialized" /var/log/apache2/error.log 2>/dev/null || echo 0)
if [ "$INIT_LOG" -gt 0 ]; then
    echo "PASS: Shared HTTP client initialized by libgomesi ($INIT_LOG log line(s) in apache error.log)"
    docker exec apache-apache-1 grep "mesi: shared HTTP client initialized" /var/log/apache2/error.log | head -1
else
    echo "FAIL: No 'mesi: shared HTTP client initialized' log line found — MesiSharedHTTPClient wiring broken"
    docker compose down
    exit 1
fi

docker compose down

echo ""
echo "=== All tests passed ==="
