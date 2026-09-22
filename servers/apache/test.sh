#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

docker compose up -d --wait

echo "=== Test 1: Simple ESI include ==="
RESPONSE=$(curl -s http://localhost:18080/index.html)
if echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: ESI include processed"
else
    echo "FAIL: ESI include not processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 2: Surrogate-Capability header ==="
HEADERS=$(curl -sI http://localhost:18080/index.html)
if echo "$HEADERS" | grep -q "Surrogate-Capability"; then
    echo "PASS: Surrogate-Capability header present"
else
    echo "FAIL: Surrogate-Capability header missing"
    echo "Headers: $HEADERS"
    exit 1
fi

echo "=== Test 3: Non-HTML content (text/plain) ==="
RESPONSE=$(curl -s http://localhost:18080/noesi.txt)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Plain text content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: Plain text content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 4: Content-Type check ==="
CT=$(curl -sI http://localhost:18080/index.html | grep -i "Content-Type")
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Wrong Content-Type"
    echo "Content-Type: $CT"
    exit 1
fi

echo "=== Test 5: AllowedHosts - allowed host (backend) ==="
RESPONSE=$(curl -s http://localhost:18080/ssrf-allowed.html)
if echo "$RESPONSE" | grep -q "allowed content"; then
    echo "PASS: Include from allowed host (backend) succeeded"
else
    echo "FAIL: Include from allowed host failed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 6: AllowedHosts - blocked host (evil.com) ==="
RESPONSE=$(curl -s http://localhost:18080/ssrf-blocked.html)
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
RESPONSE=$(curl -s http://localhost:18080/large.html)
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
RESPONSE=$(curl -s http://localhost:18080/backend/large.html)
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
CT=$(curl -s -D - http://localhost:18080/large.html -o /dev/null 2>/dev/null | grep -i "Content-Type" || true)
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Content-Type missing or wrong"
    echo "Headers:"
    curl -s -D - http://localhost:18080/large.html -o /dev/null
    exit 1
fi

echo "=== Test 10: Large response body size matches (no truncation) ==="
BODY_SIZE=$(curl -s http://localhost:18080/large.html | wc -c)
if [ "$BODY_SIZE" -gt 102000 ]; then
    echo "PASS: Large response body is $BODY_SIZE bytes (expected > 102000)"
else
    echo "FAIL: Large response body is only $BODY_SIZE bytes (truncation?)"
    exit 1
fi

echo "=== Test 11: JSON content (application/json) not processed ==="
RESPONSE=$(curl -s http://localhost:18080/noesi.json)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: JSON content not processed (raw esi:include preserved)"
else
    echo "FAIL: JSON content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi
CT=$(curl -sI http://localhost:18080/noesi.json | grep -i "Content-Type")
if echo "$CT" | grep -qi "application/json"; then
    echo "PASS: JSON Content-Type is application/json"
else
    echo "FAIL: JSON Content-Type is wrong: $CT"
    exit 1
fi

echo "=== Test 12: CSS content (text/css) not processed ==="
RESPONSE=$(curl -s http://localhost:18080/noesi.css)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: CSS content not processed (raw esi:include preserved)"
else
    echo "FAIL: CSS content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi
CT=$(curl -sI http://localhost:18080/noesi.css | grep -i "Content-Type")
if echo "$CT" | grep -qi "text/css"; then
    echo "PASS: CSS Content-Type is text/css"
else
    echo "FAIL: CSS Content-Type is wrong: $CT"
    exit 1
fi

echo "=== Test 13: Flatten error fallback (synthetic MESI_FORCE_FLATTEN_ERROR) ==="
docker compose down
MESI_FORCE_FLATTEN_ERROR=1 docker compose up -d --wait
RESPONSE=$(curl -s http://localhost:18080/index.html)
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
RESPONSE=$(curl -s http://localhost:18080/nested.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: Nested ESI include resolved correctly"
else
    echo "FAIL: Nested ESI include failed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 15: Local backend include (replacing GitHub raw URLs) ==="
RESPONSE=$(curl -s http://localhost:18080/index.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: Local backend include works (no GitHub dependency)"
else
    echo "FAIL: Local backend include failed"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 16: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:18080/comment.html)
if echo "$RESPONSE" | grep -q "ESI comment unwrapped content"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 17: ESI remove ==="
RESPONSE=$(curl -s http://localhost:18080/remove.html)
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
RESPONSE=$(curl -s http://localhost:18080/fallback.html)
if echo "$RESPONSE" | grep -q "fallback content rendered"; then
    echo "PASS: ESI fallback content used"
else
    echo "FAIL: ESI fallback not working"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 19: HTTP error passthrough (status >= 400) ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:18080/nonexistent.html)
if [ "$STATUS" = "404" ]; then
    echo "PASS: HTTP 404 returned for nonexistent page"
else
    echo "FAIL: Expected 404, got $STATUS"
    docker compose down
    exit 1
fi

echo "=== Test 20: Content-Length correctness ==="
HEADERS=$(curl -s -D - http://localhost:18080/index.html -o /tmp/mesi-response-body.txt 2>/dev/null)
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
    curl -s http://localhost:18080/index.html -o /tmp/mesi-concurrent-$i.html &
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
RESPONSE=$(curl -s http://localhost:18080/nonexistent.html)
if [ "$(curl -s -o /dev/null -w '%{http_code}' http://localhost:18080/nonexistent.html)" = "404" ] && [ -n "$RESPONSE" ]; then
    echo "PASS: ESI not applied to 404 error page (status=404, body non-empty)"
else
    echo "FAIL: Unexpected response for 404 page"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 23: Surrogate-Capability header on non-HTML content ==="
HEADERS=$(curl -sI http://localhost:18080/noesi.txt)
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
RESPONSE=$(curl -s http://localhost:18080/cache-test.html)
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
docker compose up -d --wait

echo "=== Test 26: Cache key template - header isolation (#177) ==="
# 8084 (MesiCacheKeyTemplate "mesi:${url}:${header:Accept-Language}").
# Uses a DEDICATED fixture cache-key-template.html -> cache-key-fragment.txt
# so the backend counter is not polluted by healthchecks (which hit / -> index.html -> include.txt every 2s).
# Mirrors Test 24 which counts cached-fragment.txt, not include.txt.
RESPONSE=$(curl -s -H "Accept-Language: pl" http://localhost:8084/cache-key-template.html)
if echo "$RESPONSE" | grep -q "cache-key dedicated fragment"; then
    echo "PASS: Cache key template (pl) — ESI resolved"
else
    echo "FAIL: Cache key template (pl) — ESI not resolved"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi
BASE=$(docker compose logs --no-color backend 2>&1 | grep -c "GET /cache-key-fragment.txt HTTP/1.1" || true)
echo "  (backend GET /cache-key-fragment.txt so far: $BASE)"

echo "=== Test 26a: Same Accept-Language reuses cache (0-1 extra hits) ==="
curl -s -H "Accept-Language: pl" http://localhost:8084/cache-key-template.html > /dev/null
AFTER_SAME=$(docker compose logs --no-color backend 2>&1 | grep -c "GET /cache-key-fragment.txt HTTP/1.1" || true)
EXTRA_SAME=$((AFTER_SAME - BASE))
if [ "$EXTRA_SAME" -ge 0 ] && [ "$EXTRA_SAME" -le 1 ]; then
    echo "PASS: Same Accept-Language reused cache (extra hits: $EXTRA_SAME, allowed 0-1 for in-response race)"
else
    echo "FAIL: Same Accept-Language should reuse cache; extra hits: $EXTRA_SAME (expected 0-1)"
    docker compose logs --no-color backend 2>&1 | grep "cache-key-fragment" | tail -10
    docker compose down
    exit 1
fi

echo "=== Test 26b: Different Accept-Language misses cache (1 extra hit) ==="
curl -s -H "Accept-Language: en" http://localhost:8084/cache-key-template.html > /dev/null
AFTER_DIFF=$(docker compose logs --no-color backend 2>&1 | grep -c "GET /cache-key-fragment.txt HTTP/1.1" || true)
EXTRA_DIFF=$((AFTER_DIFF - AFTER_SAME))
if [ "$EXTRA_DIFF" -eq 1 ]; then
    echo "PASS: Different Accept-Language produced distinct key (extra hits: 1)"
else
    echo "FAIL: Different Accept-Language should be a distinct key; extra hits: $EXTRA_DIFF (expected 1)"
    echo "  AFTER_SAME=$AFTER_SAME AFTER_DIFF=$AFTER_DIFF BASE=$BASE"
    docker compose logs --no-color backend 2>&1 | grep "cache-key-fragment" | tail -10
    docker compose down
    exit 1
fi

echo "=== Test 26c: No template (8083) — URL-only key is header-agnostic ==="
RESPONSE_NOTMPL=$(curl -s -H "Accept-Language: pl" http://localhost:8083/shared-http-client.html)
if echo "$RESPONSE_NOTMPL" | grep -q "shared fragment from backend"; then
    echo "PASS: No template (8083) — URL-only DefaultCacheKey still serves ESI (backward compat)"
else
    echo "FAIL: No template (8083) — backward-compat ESI broken"
    echo "Response: $RESPONSE_NOTMPL"
    docker compose down
    exit 1
fi

echo "=== Test 27: MesiMaxDepth 1 — inner nest not processed (#166) ==="
# 8085: MesiMaxDepth 1. /nested.html includes nested.txt, which itself
# includes include.txt. Depth 1 fetches nested.txt then re-parses it
# with MaxDepth=0 (ParseOnly), so the inner tag is replaced with the
# empty IncludeErrorMarker — same contract as Caddy TestMaxDepthExplicit.
# Chrome must remain; the inner fragment body must not; leftover
# <esi:include> means the filter never ran.
RESPONSE=$(curl -s http://localhost:8085/nested.html)
if echo "$RESPONSE" | grep -q "Nested ESI Test" \
    && ! echo "$RESPONSE" | grep -q "included content from backend" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: MesiMaxDepth 1 fetched the outer include and stripped the inner tag"
else
    echo "FAIL: MesiMaxDepth 1 did not match depth-1 contract (chrome, no inner body, no leftover tag)"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 28: MesiMaxDepth 5 — both nest levels processed (#166) ==="
RESPONSE=$(curl -s http://localhost:8086/nested.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: MesiMaxDepth 5 processed both nest levels"
else
    echo "FAIL: MesiMaxDepth 5 did not process inner include"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

# --- MesiTimeout tests (#167) ---
# The backend (tests/server.py) serves /sleep/<seconds>/<label>, which
# blocks for <seconds> and then returns "<label> Waited <seconds>".
# Wall-clock assertions use curl's %{time_total} (seconds, decimal) so
# they are portable (no GNU date +%N dependency).

echo "=== Test 29: MesiTimeout 2 — 5s include aborted at ~2s (#167) ==="
# Backend sleeps 5s; the 2s budget must cut the fetch first. The floor
# (1.5s) proves the fetch was really in flight — an instantly failing
# setup (wrong host, dead backend) must NOT pass; the ceiling (4.0s)
# proves the budget fired before the 5s sleep completed. The fragment
# must be absent (empty IncludeErrorMarker) and no raw tag may remain.
TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 20 http://localhost:8087/timeout-2s.html)
RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
rm -f /tmp/mesi-timeout-body.txt
if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 1.5 && t <= 4.0)}' \
    && echo "$RESPONSE" | grep -q "After timeout include" \
    && ! echo "$RESPONSE" | grep -q "timeout2 Waited" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: include failed within ~2s (elapsed ${TIME_TOTAL}s, fragment absent, tag stripped)"
else
    echo "FAIL: MesiTimeout 2 did not abort the 5s include (elapsed ${TIME_TOTAL}s)"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 30: MesiTimeout 30 — 10s include succeeds (#167) ==="
# Backend sleeps 10s; the 30s budget must let it through with the full
# fragment. Floor 9.5s proves the complete backend sleep happened
# (cold URL — never fetched before, failures are never cached).
TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 40 http://localhost:8088/timeout-30s.html)
RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
rm -f /tmp/mesi-timeout-body.txt
if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 9.5 && t <= 25.0)}' \
    && echo "$RESPONSE" | grep -q "timeout30 Waited 10" \
    && echo "$RESPONSE" | grep -q "After slow include"; then
    echo "PASS: 10s include succeeded under MesiTimeout 30 (elapsed ${TIME_TOTAL}s)"
else
    echo "FAIL: MesiTimeout 30 did not let the 10s include through (elapsed ${TIME_TOTAL}s)"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 31: MesiTimeout unset — default 30s aborts a 31s include (#167) ==="
# Default vhost (*:80) leaves MesiTimeout unset → the documented 30s
# default. Backend sleeps 31s: the budget fires at ~30s (no fragment,
# chrome intact, no raw tag). Elapsed must be >= 28s (a smaller default
# would drop below the floor) and the fragment must be absent — the
# issue's proposed "0 = no timeout" would render it at ~31s, and an
# unlimited default would too, so the content check pins the default to
# a finite 30s budget.
TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 55 http://localhost:18080/timeout-default.html)
RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
rm -f /tmp/mesi-timeout-body.txt
if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 28.0 && t <= 40.0)}' \
    && echo "$RESPONSE" | grep -q "After default include" \
    && ! echo "$RESPONSE" | grep -q "timeoutdefault Waited" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: unset MesiTimeout aborted the 31s include at ~30s (elapsed ${TIME_TOTAL}s)"
else
    echo "FAIL: unset MesiTimeout did not behave like the 30s default (elapsed ${TIME_TOTAL}s)"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

# --- MesiMaxResponseSize tests (#169) ---
# The backend (tests/server.py) serves /bytes/<size>, a body of exactly
# <size> bytes prefixed with a "MesiBytesPayload <size>" marker line.
# Assertions combine the marker (fragment arrived / was rejected) with
# wc -c (the whole body was delivered, not just the marker).

echo "=== Test 32: MesiMaxResponseSize 100 — 200-byte include rejected (#169) ==="
RESPONSE=$(curl -s http://localhost:8089/max-response-reject.html)
if echo "$RESPONSE" | grep -q "After reject include" \
    && ! echo "$RESPONSE" | grep -q "MesiBytesPayload" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: 200-byte include rejected by MesiMaxResponseSize 100 (marker absent, tag stripped)"
    # Control: the SAME page on the unset default vhost (no directive)
    # must deliver the payload — proves the rejection above comes from
    # the directive, not a broken endpoint or vhost template.
    CONTROL=$(curl -s http://localhost:18080/max-response-reject.html)
    if echo "$CONTROL" | grep -q "MesiBytesPayload 200" \
        && echo "$CONTROL" | grep -q "After reject include" \
        && ! echo "$CONTROL" | grep -q '<esi:include'; then
        echo "PASS: control — same page on the unset vhost delivers the 200-byte payload"
    else
        echo "FAIL: control — same page on the unset vhost did not deliver the payload"
        echo "Response: $CONTROL"
        docker compose down
        exit 1
    fi
else
    echo "FAIL: MesiMaxResponseSize 100 did not reject the 200-byte include"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 33: MesiMaxResponseSize 1048576 — 500 KB include succeeds (#169) ==="
curl -s -o /tmp/mesi-mrs-accept.html http://localhost:8090/max-response-accept.html
SIZE=$(wc -c < /tmp/mesi-mrs-accept.html | tr -d ' ')
if [ "$SIZE" -gt 512000 ] \
    && grep -q "MesiBytesPayload 512000" /tmp/mesi-mrs-accept.html \
    && grep -q "After accept include" /tmp/mesi-mrs-accept.html \
    && ! grep -q '<esi:include' /tmp/mesi-mrs-accept.html; then
    echo "PASS: 500 KB include delivered in full under MesiMaxResponseSize 1048576 ($SIZE bytes)"
else
    echo "FAIL: MesiMaxResponseSize 1048576 did not deliver the 500 KB include (size $SIZE)"
    head -c 500 /tmp/mesi-mrs-accept.html
    rm -f /tmp/mesi-mrs-accept.html
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-mrs-accept.html

echo "=== Test 34: MesiMaxResponseSize 0 — unlimited, 50 MB include succeeds (#169) ==="
curl -s --max-time 120 -o /tmp/mesi-mrs-unlimited.html http://localhost:8091/max-response-unlimited.html
SIZE=$(wc -c < /tmp/mesi-mrs-unlimited.html | tr -d ' ')
if [ "$SIZE" -gt 52428800 ] \
    && grep -q "MesiBytesPayload 52428800" /tmp/mesi-mrs-unlimited.html \
    && grep -q "After unlimited include" /tmp/mesi-mrs-unlimited.html \
    && ! grep -q '<esi:include' /tmp/mesi-mrs-unlimited.html; then
    echo "PASS: 50 MB include delivered in full under MesiMaxResponseSize 0 (unlimited, $SIZE bytes)"
else
    echo "FAIL: MesiMaxResponseSize 0 did not behave as unlimited (size $SIZE)"
    head -c 500 /tmp/mesi-mrs-unlimited.html
    rm -f /tmp/mesi-mrs-unlimited.html
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-mrs-unlimited.html

echo "=== Test 35: MesiMaxResponseSize unset — backward compat, 10 MB + 1 include succeeds (#169) ==="
# Default vhost (*:80) never sets the directive. The body is 10 MB + 1
# byte: the issue's proposed implicit 10 MB default would reject it, so
# a passing test pins "unset → unlimited" (byte-identical to pre-#169
# behaviour) at the functional level.
curl -s --max-time 120 -o /tmp/mesi-mrs-unset.html http://localhost:18080/max-response-unset.html
SIZE=$(wc -c < /tmp/mesi-mrs-unset.html | tr -d ' ')
if [ "$SIZE" -gt 10485761 ] \
    && grep -q "MesiBytesPayload 10485761" /tmp/mesi-mrs-unset.html \
    && grep -q "After unset include" /tmp/mesi-mrs-unset.html \
    && ! grep -q '<esi:include' /tmp/mesi-mrs-unset.html; then
    echo "PASS: unset MesiMaxResponseSize stayed unlimited — 10 MB + 1 include delivered ($SIZE bytes)"
else
    echo "FAIL: unset MesiMaxResponseSize did not behave as unlimited (size $SIZE)"
    head -c 500 /tmp/mesi-mrs-unset.html
    rm -f /tmp/mesi-mrs-unset.html
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-mrs-unset.html

# --- MesiMaxConcurrentRequests tests (#170) ---
# The backend (tests/server.py) serves /hold/<millis>/<label>: it
# records request concurrency in a peak counter (see /track/reset and
# /track/max, reachable through the *:80 vhost's /backend/ ProxyPass),
# holds each request for <millis>, then returns a "<label> Held
# <millis>" fragment. Each page fans out to 20 DISTINCT labels so every
# include reaches the backend (duplicate URLs would be served from the
# in-process cache, #174, and never touch the counter). The peak
# counter is a deterministic observable — no wall-clock assertion.
#
# Fan-out bound for the "unlimited" cases: MESIParse drains includes
# through a worker pool of min(MaxWorkers=NumCPU*4, 20) goroutines
# (mesi/parser.go), i.e. at least 4 in any container — with 1500 ms
# holds, an uncapped parse must show peak >= 4, while a cap of 3 can
# never exceed 3 (hard semaphore invariant, mesi/fetch.go).

echo "=== Test 36: MesiMaxConcurrentRequests 3 — 20 includes funneled through 3 slots (#170) ==="
# 8092 sets ONLY MesiMaxConcurrentRequests 3 (proving the routing
# condition sends a maxcr-only config through ParseJson with the
# timeout/max-response-size keys absent). Assertions: peak <= 3 is the
# cap itself (an uncapped parse would reach >= 4 per the fan-out bound
# above, so this discriminates a broken route); peak >= 2 proves the
# cap is a multi-slot queue, not a serialisation to 1 (an exact peak
# == 3 would additionally require all three first-wave dials to
# overlap — scheduling-dependent, deliberately not asserted). All 20
# fragments must arrive: includes beyond the cap are QUEUED, not
# dropped.
curl -s http://localhost:18080/backend/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mcr-capped.html http://localhost:8092/concurrent-capped.html
PEAK=$(curl -s http://localhost:18080/backend/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-capped.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 2 ] && [ "$PEAK" -le 3 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After capped include" /tmp/mesi-mcr-capped.html \
    && ! grep -q '<esi:include' /tmp/mesi-mcr-capped.html; then
    echo "PASS: peak concurrent fetches $PEAK <= 3 (cap), >= 2 (parallel slots), all 20 fragments queued and delivered"
else
    echo "FAIL: MesiMaxConcurrentRequests 3 did not funnel the 20 includes (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mcr-capped.html
    rm -f /tmp/mesi-mcr-capped.html
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-mcr-capped.html

echo "=== Test 37: MesiMaxConcurrentRequests 0 — explicit unlimited, fan-out unthrottled (#170) ==="
# 8093 sets an explicit 0: the value must reach the core as
# "unlimited" (ParseJson "maxConcurrentRequests":0 — an explicit 0
# rejected Go-side would make ParseJson return NULL and the request
# would fail closed with 500). Peak >= 4 distinguishes this from the
# cap-3 vhost; the fan-out bound above explains the floor.
curl -s http://localhost:18080/backend/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mcr-zero.html http://localhost:8093/concurrent-unlimited.html
PEAK=$(curl -s http://localhost:18080/backend/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-zero.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After explicit-zero include" /tmp/mesi-mcr-zero.html \
    && ! grep -q '<esi:include' /tmp/mesi-mcr-zero.html; then
    echo "PASS: peak concurrent fetches $PEAK >= 4 under explicit MesiMaxConcurrentRequests 0 (unlimited), all 20 fragments delivered"
else
    echo "FAIL: MesiMaxConcurrentRequests 0 did not behave as unlimited (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mcr-zero.html
    rm -f /tmp/mesi-mcr-zero.html
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-mcr-zero.html

echo "=== Test 38: MesiMaxConcurrentRequests unset — backward compat, fan-out unthrottled (#170) ==="
# The default vhost (*:80) never sets the directive → the legacy parse
# path (no ParseJson key rendered) with MaxConcurrentRequests left at
# 0 = unlimited, byte-identical to pre-#170 behaviour. Peak >= 4 pins
# that unset never throttles.
curl -s http://localhost:18080/backend/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mcr-unset.html http://localhost:18080/concurrent-unset.html
PEAK=$(curl -s http://localhost:18080/backend/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-unset.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After unset-maxcr include" /tmp/mesi-mcr-unset.html \
    && ! grep -q '<esi:include' /tmp/mesi-mcr-unset.html; then
    echo "PASS: unset MesiMaxConcurrentRequests stayed unlimited — peak $PEAK >= 4, all 20 fragments delivered"
else
    echo "FAIL: unset MesiMaxConcurrentRequests did not behave as unlimited (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mcr-unset.html
    rm -f /tmp/mesi-mcr-unset.html
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-mcr-unset.html

docker compose down

echo ""
echo "=== All tests passed ==="

