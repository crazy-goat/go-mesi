#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

docker compose up -d --wait

echo "=== Test 1: ESI include processing ==="
RESPONSE=$(curl -s http://localhost:18080/index.html)
if echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: ESI include processed"
else
    echo "FAIL: ESI include not processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 2: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:18080/comment.html)
if echo "$RESPONSE" | grep -q "ESI comment unwrapped content"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 3: ESI remove ==="
RESPONSE=$(curl -s http://localhost:18080/remove.html)
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
HEADERS=$(curl -sI http://localhost:18080/index.html)
if echo "$HEADERS" | grep -q "Surrogate-Capability"; then
    echo "PASS: Surrogate-Capability header present"
else
    echo "FAIL: Surrogate-Capability header missing"
    echo "Headers: $HEADERS"
    exit 1
fi

echo "=== Test 5: Non-HTML content (text/plain) ==="
RESPONSE=$(curl -s http://localhost:18080/noesi.txt)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Plain text content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: Plain text content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 6: Content-Length correctness ==="
HEADERS=$(curl -s -D - http://localhost:18080/index.html -o /tmp/mesi-response-body.txt 2>/dev/null)
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
RESPONSE=$(curl -s http://localhost:18080/nested.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: Nested ESI include resolved correctly"
else
    echo "FAIL: Nested ESI include failed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 8: ESI include with fallback ==="
RESPONSE=$(curl -s http://localhost:18080/fallback.html)
if echo "$RESPONSE" | grep -q "fallback content rendered"; then
    echo "PASS: ESI fallback content used"
else
    echo "FAIL: ESI fallback not working"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 9: HTTP error passthrough (status >= 400) ==="
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:18080/nonexistent.html)
if [ "$STATUS" = "404" ]; then
    echo "PASS: HTTP 404 returned for nonexistent page"
else
    echo "FAIL: Expected 404, got $STATUS"
    exit 1
fi

echo "=== Test 10: JSON content (application/json) not processed ==="
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

echo "=== Test 11: CSS content (text/css) not processed ==="
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

echo "=== Test 12: Content-Type check ==="
CT=$(curl -sI http://localhost:18080/index.html | grep -i "Content-Type")
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Wrong Content-Type"
    echo "Content-Type: $CT"
    exit 1
fi

echo "=== Test 13: Cache hit in same page (two includes, same URL) ==="
RESPONSE=$(curl -s http://localhost:18080/cache/cache.html)
# Extract counter values (bare digits on their own line, possibly indented).
COUNTERS=$(echo "$RESPONSE" | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ')
FIRST_NUM=$(echo "$COUNTERS" | head -1)
SECOND_NUM=$(echo "$COUNTERS" | tail -1)
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
RESPONSE1=$(curl -s http://localhost:18080/cache/cache_ttl.html)
NUM1=$(echo "$RESPONSE1" | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
sleep 1
RESPONSE2=$(curl -s http://localhost:18080/cache/cache_ttl.html)
NUM2=$(echo "$RESPONSE2" | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
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
RESPONSE=$(curl -s http://localhost:18080/index.html)
if echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: ESI still works without cache backend configured"
else
    echo "FAIL: ESI processing broken in non-cache location"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 16: Memcached cache — same page, two includes, same URL ==="
# Restart nginx to clear cache_initialized so the memcached backend is
# re-initialised with InitCacheWithConfig.
docker compose restart nginx
# Wait for nginx health check to pass again.
docker compose up -d --wait

RESPONSE=$(curl -s http://localhost:18080/cache/memcached/cache_memcached.html)
COUNTERS=$(echo "$RESPONSE" | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ')
FIRST_NUM=$(echo "$COUNTERS" | head -1)
SECOND_NUM=$(echo "$COUNTERS" | tail -1)
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
RESPONSE1=$(curl -s http://localhost:18080/cache/memcached/cache_ttl.html)
NUM1=$(echo "$RESPONSE1" | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
sleep 1
RESPONSE2=$(curl -s http://localhost:18080/cache/memcached/cache_ttl.html)
NUM2=$(echo "$RESPONSE2" | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
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

echo "=== Test 18: SSRF default (unset) blocks private IP ==="
RESPONSE=$(curl -s http://localhost:18080/ssrf-default/ssrf.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "FAIL: SSRF default did not block private IP include"
    echo "Response: $RESPONSE"
    exit 1
elif echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: SSRF default (unset) blocks private IP include"
else
    echo "FAIL: SSRF default test page not rendered as expected"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 19: SSRF on blocks private IP ==="
RESPONSE=$(curl -s http://localhost:18080/ssrf-on/ssrf.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "FAIL: mesi_block_private_ips on did not block private IP include"
    echo "Response: $RESPONSE"
    exit 1
elif echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: mesi_block_private_ips on blocks private IP include"
else
    echo "FAIL: SSRF on test page not rendered as expected"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 20: SSRF off allows private IP ==="
RESPONSE=$(curl -s http://localhost:18080/ssrf-off/ssrf.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: mesi_block_private_ips off allows private IP include"
else
    echo "FAIL: mesi_block_private_ips off blocked a private IP include"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 21: AllowedHosts — listed host works ==="
RESPONSE=$(curl -s http://localhost:18080/allowed/allowed.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: mesi_allowed_hosts allows the listed host"
else
    echo "FAIL: mesi_allowed_hosts blocked the listed host"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 22: AllowedHosts — non-listed hosts blocked ==="
RESPONSE=$(curl -s http://localhost:18080/allowed/allowed_blocked.html)
if echo "$RESPONSE" | grep -q "FALLBACK-NOTBACKEND"; then
    if echo "$RESPONSE" | grep -q "FALLBACK-ATTACKER"; then
        if echo "$RESPONSE" | grep -q "FALLBACK-EVIL"; then
            if echo "$RESPONSE" | grep -q "included content from backend"; then
                echo "FAIL: blocked include content leaked into response"
                echo "Response: $RESPONSE"
                exit 1
            fi
            echo "PASS: notbackend.com, attacker-example.com and evil.com all blocked"
        else
            echo "FAIL: evil.com include did not fall back (was it allowed?)"
            echo "Response: $RESPONSE"
            exit 1
        fi
    else
        echo "FAIL: attacker-example.com include did not fall back (was it allowed?)"
        echo "Response: $RESPONSE"
        exit 1
    fi
else
    echo "FAIL: notbackend.com include did not fall back (was it allowed?)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 23: AllowedHosts — subdomain of allowed host works ==="
RESPONSE=$(curl -s http://localhost:18080/allowed/allowed_subdomain.html)
if echo "$RESPONSE" | grep -q "included content from subdomain"; then
    echo "PASS: sub.backend (subdomain of backend) allowed"
else
    echo "FAIL: sub.backend include blocked or failed to fetch"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 24: AllowedHosts — suffix injection blocked (example.com list) ==="
RESPONSE=$(curl -s http://localhost:18080/allowed-inject/allowed_inject.html)
if echo "$RESPONSE" | grep -q "FALLBACK-ATTACKER"; then
    if echo "$RESPONSE" | grep -q "FALLBACK-SUFFIX"; then
        echo "PASS: attacker-example.com and example.com.evil.com do not match example.com"
    else
        echo "FAIL: example.com.evil.com include did not fall back (was it allowed?)"
        echo "Response: $RESPONSE"
        exit 1
    fi
else
    echo "FAIL: attacker-example.com include did not fall back (was it allowed?)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 25: AllowedHosts unset — all hosts allowed (backward compatible) ==="
RESPONSE=$(curl -s http://localhost:18080/allowed-unset/allowed_unset.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    if echo "$RESPONSE" | grep -q "included content from cdn alias"; then
        echo "PASS: unset mesi_allowed_hosts allows any host (incl. unlisted cdn.example.net)"
    else
        echo "FAIL: unlisted cdn.example.net include failed"
        echo "Response: $RESPONSE"
        exit 1
    fi
else
    echo "FAIL: unset mesi_allowed_hosts blocked a backend include"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 26: AllowedHosts — multiple space-separated hosts ==="
RESPONSE=$(curl -s http://localhost:18080/allowed-multi/allowed_multi.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    if echo "$RESPONSE" | grep -q "included content from subdomain"; then
        if echo "$RESPONSE" | grep -q "FALLBACK-NOTBACKEND"; then
            echo "PASS: backend and sub.backend both allowed, notbackend.com blocked"
        else
            echo "FAIL: notbackend.com include did not fall back (was it allowed?)"
            echo "Response: $RESPONSE"
            exit 1
        fi
    else
        echo "FAIL: sub.backend include failed in multi-host location"
        echo "Response: $RESPONSE"
        exit 1
    fi
else
    echo "FAIL: backend include failed in multi-host location"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 27: AllowedHosts — redirect to non-listed host NOT followed ==="
RESPONSE=$(curl -s http://localhost:18080/allowed-redirect/)
if echo "$RESPONSE" | grep -q "FALLBACK-REDIRECT"; then
    if echo "$RESPONSE" | grep -q "included content from redirect target"; then
        echo "FAIL: redirect target outside whitelist was fetched"
        echo "Response: $RESPONSE"
        exit 1
    fi
    echo "PASS: redirect from allowed backend to cdn.example.net blocked (fallback rendered)"
else
    echo "FAIL: redirect target include did not fall back (was it followed?)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 28: AllowedHosts — redirect to listed host IS followed ==="
RESPONSE=$(curl -s http://localhost:18080/allowed-redirect-cdn/)
if echo "$RESPONSE" | grep -q "included content from redirect target"; then
    echo "PASS: redirect to allowlisted cdn.example.net followed"
else
    echo "FAIL: redirect to allowlisted cdn.example.net did not resolve or was blocked"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 29: Config validation — ASCII-whitespace-only mesi_allowed_hosts rejected ==="
# Regression: a value made only of ASCII whitespace must fail nginx -t, not
# silently become an empty allowlist (allow all hosts) in libgomesi.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_allowed_hosts "  \t\r\n";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-allowed-host-ascii-ws.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-allowed-host-ascii-ws.conf' < /tmp/nginx-allowed-host-ascii-ws.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-allowed-host-ascii-ws.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "must contain at least one hostname"; then
    echo "PASS: ASCII-whitespace-only mesi_allowed_hosts rejected by nginx -t"
else
    echo "FAIL: nginx did not reject ASCII-whitespace-only mesi_allowed_hosts with the expected error"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

echo "=== Test 30: Config validation — Unicode-whitespace-only mesi_allowed_hosts rejected ==="
# U+00A0 no-break space (bytes c2 a0) only: passes the ASCII whitespace
# check, but libgomesi splits the value with strings.Fields, which strips
# all Unicode whitespace — the allowlist would silently become empty
# (allow all hosts, fail-open). Must be rejected at config load.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_allowed_hosts "\302\240";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-allowed-host-nbsp.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-allowed-host-nbsp.conf' < /tmp/nginx-allowed-host-nbsp.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-allowed-host-nbsp.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "must contain at least one hostname"; then
    echo "PASS: Unicode-whitespace-only mesi_allowed_hosts rejected by nginx -t"
else
    echo "FAIL: nginx did not reject Unicode-whitespace-only mesi_allowed_hosts with the expected error"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

echo "=== Test 31: Config validation — valid mesi_allowed_hosts values still pass ==="
# Legitimate values must keep passing: leading/trailing whitespace around a
# real hostname, ASCII-whitespace separation as today, and Unicode
# whitespace between two real hostnames (tokenizes to multiple entries in
# libgomesi — only zero-token values are rejected).
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_allowed_hosts "  backend   sub.backend \302\240cdn.example.net \t";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-allowed-host-valid.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-allowed-host-valid.conf' < /tmp/nginx-allowed-host-valid.conf
if ! docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-allowed-host-valid.conf >/dev/null 2>&1; then
    echo "FAIL: nginx rejected a valid mesi_allowed_hosts with surrounding whitespace"
    docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-allowed-host-valid.conf 2>&1 || true
    exit 1
fi
echo "PASS: valid mesi_allowed_hosts (leading/trailing whitespace, ASCII and Unicode separators between hosts) accepted by nginx -t"

docker compose down

echo ""
echo "=== All tests passed ==="
