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
# (a) U+00A0 no-break space (bytes c2 a0) only, (b) multi-rune whitespace =
# NBSP + U+2000 EN QUAD (c2 a0 e2 80 80): both pass the ASCII whitespace
# check, but libgomesi splits the value with strings.Fields, which strips
# all Unicode whitespace — the allowlist would silently become empty
# (allow all hosts, fail-open). Both must be rejected at config load.
for ALLOWED_HOSTS_VAL in '"\302\240"' '"\302\240\342\200\200"'; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_allowed_hosts ${ALLOWED_HOSTS_VAL};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-allowed-host-nbsp.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-allowed-host-nbsp.conf' < /tmp/nginx-allowed-host-nbsp.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-allowed-host-nbsp.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "must contain at least one hostname"; then
        echo "PASS: Unicode-whitespace-only mesi_allowed_hosts ${ALLOWED_HOSTS_VAL} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject Unicode-whitespace-only mesi_allowed_hosts ${ALLOWED_HOSTS_VAL} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

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

echo "=== Test 32: AllowPrivateIPsForAllowedHosts — directive on allows the listed private host ==="
RESPONSE=$(curl -s http://localhost:18080/bypass-on/allowed.html)
if echo "$RESPONSE" | grep -q "included content from backend"; then
    echo "PASS: mesi_allow_private_ips_for_allowed on lets the listed host through (block_private_ips stays on)"
else
    echo "FAIL: allowed listed host on a private IP was not fetched with the bypass on"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 33: AllowPrivateIPsForAllowedHosts — default off blocks the listed private host ==="
RESPONSE=$(curl -s http://localhost:18080/bypass-off/bypass_off.html)
if echo "$RESPONSE" | grep -q "FALLBACK-BY-PASS-OFF"; then
    if echo "$RESPONSE" | grep -q "included content from backend"; then
        echo "FAIL: allowlisted private host leaked content despite the bypass being off"
        echo "Response: $RESPONSE"
        exit 1
    fi
    echo "PASS: allowlisted host on a private IP blocked with the bypass off (default)"
else
    echo "FAIL: include did not fall back (was it fetched?)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 34: AllowPrivateIPsForAllowedHosts — directive on does NOT bypass for hosts outside allowed_hosts ==="
RESPONSE=$(curl -s http://localhost:18080/bypass-unlisted/bypass_unlisted.html)
if echo "$RESPONSE" | grep -q "FALLBACK-UNLISTED-BY-PASS"; then
    if echo "$RESPONSE" | grep -q "included content from backend"; then
        echo "FAIL: unlisted private host leaked content despite the bypass being on (bypass must only cover allowed_hosts)"
        echo "Response: $RESPONSE"
        exit 1
    fi
    echo "PASS: private host NOT in allowed_hosts stays blocked even with the directive on"
else
    echo "FAIL: include did not fall back (was it fetched?)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 35: Cache key template — Accept-Language variants get distinct cache entries ==="
# The ESI include fetch does not forward the parent request's headers
# (libgomesi fetches with its own HTTP client), so the dedicated
# /langcount backend endpoint returns a fresh number per fetch: each
# Accept-Language variant's FIRST request is a cache miss that pins that
# number, and every later request for the same language must replay it.
# The counter stands in for the language-specific variant content a real
# backend would serve (mirrors the Apache #177 hit-count approach).
PL1=$(curl -s -H "Accept-Language: pl" http://localhost:18080/cache-key-template/cache_key_template.html | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
PL2=$(curl -s -H "Accept-Language: pl" http://localhost:18080/cache-key-template/cache_key_template.html | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
EN1=$(curl -s -H "Accept-Language: en" http://localhost:18080/cache-key-template/cache_key_template.html | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
EN2=$(curl -s -H "Accept-Language: en" http://localhost:18080/cache-key-template/cache_key_template.html | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
if [ -z "$PL1" ] || [ -z "$PL2" ] || [ -z "$EN1" ] || [ -z "$EN2" ]; then
    echo "FAIL: could not extract counter values (PL1=$PL1 PL2=$PL2 EN1=$EN1 EN2=$EN2)"
    exit 1
fi
if [ "$PL1" = "$PL2" ]; then
    echo "PASS: same Accept-Language (pl) reuses its cache entry (both $PL1)"
else
    echo "FAIL: same Accept-Language (pl) should reuse the cache entry ($PL1 vs $PL2)"
    exit 1
fi
if [ "$EN1" != "$PL1" ]; then
    echo "PASS: different Accept-Language (en) is a distinct cache entry ($EN1 vs $PL1)"
else
    echo "FAIL: different Accept-Language (en) must NOT reuse the pl entry ($EN1 vs $PL1)"
    exit 1
fi
if [ "$EN1" = "$EN2" ]; then
    echo "PASS: same Accept-Language (en) reuses its cache entry (both $EN1)"
else
    echo "FAIL: same Accept-Language (en) should reuse the cache entry ($EN1 vs $EN2)"
    exit 1
fi

echo "=== Test 36: No template — URL-only cache key is header-agnostic ==="
# Control location: memory cache WITHOUT mesi_cache_key_template — the
# URL-only DefaultCacheKey must ignore Accept-Language entirely.
NT1=$(curl -s -H "Accept-Language: pl" http://localhost:18080/cache-key-notemplate/cache_key_template.html | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
NT2=$(curl -s -H "Accept-Language: en" http://localhost:18080/cache-key-notemplate/cache_key_template.html | grep -oE '^\s*[0-9]+\s*$' | tr -d ' ' | head -1)
if [ -n "$NT1" ] && [ -n "$NT2" ]; then
    if [ "$NT1" = "$NT2" ]; then
        echo "PASS: without a template both Accept-Language values share one cache entry (both $NT1)"
    else
        echo "FAIL: without a template the cache key must be header-agnostic ($NT1 vs $NT2)"
        exit 1
    fi
else
    echo "FAIL: could not extract counter values (NT1=$NT1 NT2=$NT2)"
    exit 1
fi

echo "=== Test 37: Config validation — mesi_cache_key_template control chars / oversize rejected ==="
# (a) a control character (tab) inside the template must fail nginx -t —
#     it would otherwise end up verbatim in cache keys and logs.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_cache_key_template "mesi:${url}\t${header:X}";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-cache-key-template.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-cache-key-template.conf' < /tmp/nginx-cache-key-template.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-cache-key-template.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "mesi_cache_key_template"; then
    echo "PASS: mesi_cache_key_template with a control character rejected by nginx -t"
else
    echo "FAIL: nginx did not reject the mesi_cache_key_template control character"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (b) a template longer than the module's 4096-byte cap (MESI_MAX_CACHE_KEY_
#     TEMPLATE, mirroring Apache) fails nginx -t at nginx's own parser guard:
#     "too long parameter" — nginx caps a single quoted argument at ~4090
#     bytes, tighter than the module cap, so an absurd template can never
#     load. The module-level cap is defense-in-depth for parser changes.
LONG_TEMPLATE=$(printf 'a%.0s' $(seq 1 4100))
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
      "mesi_cache_key_template \"${LONG_TEMPLATE}\";" \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-cache-key-template.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-cache-key-template.conf' < /tmp/nginx-cache-key-template.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-cache-key-template.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "too long parameter"; then
    echo "PASS: oversize mesi_cache_key_template (4100 bytes) rejected by nginx -t (parser guard)"
else
    echo "FAIL: nginx did not reject the oversize mesi_cache_key_template"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (c) a valid template (incl. the placeholder syntax) must still pass.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_cache_key_template "mesi:${url}:${header:Accept-Language}:${cookie:segment}";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-cache-key-template.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-cache-key-template.conf' < /tmp/nginx-cache-key-template.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-cache-key-template.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "syntax is ok"; then
    echo "PASS: valid mesi_cache_key_template accepted by nginx -t"
else
    echo "FAIL: nginx rejected a valid mesi_cache_key_template"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

echo "=== Test 38: mesi_max_depth 1 — inner nest not processed (#180) ==="
# /max-depth-1/nested_depth.html: depth 1 fetches the OUTER include
# (its visible body proves the fetch happened), then re-parses the
# fragment with MaxDepth=0 — the inner tag goes through the
# include-error path and is replaced with the empty default marker
# (never fetched, never left raw). Same contract as Apache Test 27 /
# Caddy, with a fixture that makes each level observable.
RESPONSE=$(curl -s http://localhost:18080/max-depth-1/nested_depth.html)
if echo "$RESPONSE" | grep -q "Nested Depth Test" \
    && echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: mesi_max_depth 1 fetched the outer include and stripped the inner tag"
else
    echo "FAIL: mesi_max_depth 1 did not match the depth-1 contract (outer body, no inner body, no leftover tag)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 39: mesi_max_depth 5 (explicit) — both nest levels processed (#180) ==="
RESPONSE=$(curl -s http://localhost:18080/max-depth-5/nested_depth.html)
if echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: explicit mesi_max_depth 5 processed both nest levels"
else
    echo "FAIL: explicit mesi_max_depth 5 did not process both nest levels"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 40: mesi_max_depth 0 — passthrough: nothing fetched, tags stripped (#180) ==="
# Depth 0 = the cross-platform "passthrough" contract (Caddy README:
# "Tags are stripped but includes are not fetched") — NOT raw tags in
# the output as the issue proposed: libgomesi's ParseOnly() takes the
# include-error path, and the default IncludeErrorMarker is empty.
# OUTER body absent proves no fetch happened; a raw tag would mean the
# filter never ran.
RESPONSE=$(curl -s http://localhost:18080/max-depth-0/nested_depth.html)
if echo "$RESPONSE" | grep -q "Nested Depth Test" \
    && ! echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: explicit mesi_max_depth 0 fetches nothing and strips the include tags"
else
    echo "FAIL: mesi_max_depth 0 did not match the passthrough contract (no fetch, stripped tags)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 41: mesi_max_depth unset → 5 (backward compat, #180) ==="
# The default location has no mesi_max_depth directive; it must keep
# behaving exactly like the historical hardcoded 5 (same as Test 7, now
# asserted explicitly as the #180 backward-compat case).
RESPONSE=$(curl -s http://localhost:18080/nested_depth.html)
if echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY"; then
    echo "PASS: unset mesi_max_depth defaults to 5 (both nest levels processed)"
else
    echo "FAIL: unset mesi_max_depth no longer behaves like depth 5"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 42: mesi_max_depth merge — child inherits the parent's value (#180) ==="
# Parent sets 1, the nested child location has no directive: the child
# must inherit 1 through ngx_conf_merge_value (depth-1 contract on both
# the parent URL and the nested child URL). A child that silently got 0
# would miss the OUTER body; a child without enable_mesi would show the
# raw tag — both fail the assertion.
RESPONSE=$(curl -s http://localhost:18080/max-depth-merge-inherit/nested_depth.html)
if echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: parent location with mesi_max_depth 1 behaves at depth 1"
else
    echo "FAIL: parent location (mesi_max_depth 1) did not match the depth-1 contract"
    echo "Response: $RESPONSE"
    exit 1
fi
RESPONSE=$(curl -s http://localhost:18080/max-depth-merge-inherit/child/nested_depth.html)
if echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: child location inherits the parent's mesi_max_depth 1"
else
    echo "FAIL: child location did not inherit the parent's mesi_max_depth"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 43: mesi_max_depth merge — child's own value overrides the parent (#180) ==="
# Parent sets 0 (passthrough), the nested child sets 5: the child must
# win and process both levels; the parent itself must stay at 0.
RESPONSE=$(curl -s http://localhost:18080/max-depth-merge-override/nested_depth.html)
if echo "$RESPONSE" | grep -q "Nested Depth Test" \
    && ! echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: parent location keeps its own mesi_max_depth 0 (nothing fetched, tags stripped)"
else
    echo "FAIL: parent location (mesi_max_depth 0) did not match the passthrough contract"
    echo "Response: $RESPONSE"
    exit 1
fi
RESPONSE=$(curl -s http://localhost:18080/max-depth-merge-override/child/nested_depth.html)
if echo "$RESPONSE" | grep -q "OUTER-DEPTH-BODY" \
    && echo "$RESPONSE" | grep -q "INNER-DEPTH-BODY"; then
    echo "PASS: child location's mesi_max_depth 5 overrides the parent's 0"
else
    echo "FAIL: child location did not override the parent's mesi_max_depth"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 44: Config validation — mesi_max_depth boundary values (#180) ==="
# (a) Boundary classes ACCEPTED: explicit 0 (passthrough) and the cap
#     MESI_MAX_MAX_DEPTH (10000).
for GOOD in 0 10000; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_depth ${GOOD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-max-depth.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-max-depth.conf' < /tmp/nginx-max-depth.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-max-depth.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "syntax is ok"; then
        echo "PASS: valid mesi_max_depth ${GOOD} accepted by nginx -t"
    else
        echo "FAIL: nginx rejected valid mesi_max_depth ${GOOD}"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (b) Format classes REJECTED: negative, non-integer, decimal, trailing
#     garbage, explicit plus sign — atoi would silently coerce all of
#     these ("-1" wraps when cast to uint, "1.5" truncates, "abc" → 0).
for BAD in '-1' 'abc' '1.5' '3foo' '+1'; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_depth ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-max-depth.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-max-depth.conf' < /tmp/nginx-max-depth.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-max-depth.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "must be a non-negative integer"; then
        echo "PASS: invalid mesi_max_depth ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject invalid mesi_max_depth ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (c) Range classes REJECTED: cap+1 and a 20-digit overflow input (the
#     setter's early exit bounds its own accumulator, so the value can
#     never overflow ngx_int_t regardless of argument length).
for BAD in 10001 99999999999999999999; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_depth ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-max-depth.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-max-depth.conf' < /tmp/nginx-max-depth.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-max-depth.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "out of range"; then
        echo "PASS: out-of-range mesi_max_depth ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject out-of-range mesi_max_depth ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (d) Empty value REJECTED: "" must not silently become a passthrough 0.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_depth "";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-max-depth.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-max-depth.conf' < /tmp/nginx-max-depth.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-max-depth.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "requires an argument"; then
    echo "PASS: empty mesi_max_depth rejected by nginx -t"
else
    echo "FAIL: nginx did not reject an empty mesi_max_depth"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (e) Missing argument REJECTED: a bare `mesi_max_depth;` (zero args) is
#     caught by NGX_CONF_TAKE1 before the setter runs.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_depth;' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-max-depth.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-max-depth.conf' < /tmp/nginx-max-depth.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-max-depth.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q 'invalid number of arguments in "mesi_max_depth"'; then
    echo "PASS: argument-less mesi_max_depth rejected by nginx -t"
else
    echo "FAIL: nginx did not reject a mesi_max_depth without an argument"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

echo "=== Test 45: mesi_timeout 2 — 5s include aborted at ~2s (#184) ==="
# The backend (tests/server.py) serves /sleep/<seconds>/<label>, which
# blocks for <seconds> and then returns "<label> Waited <seconds>".
# Wall-clock assertions use curl's %{time_total} (seconds, decimal) so
# they are portable (no GNU date +%N dependency). This is also the
# propagation proof that a configured mesi_timeout reaches libgomesi's
# ParseJson {"timeoutSeconds":N} key: if the key never arrived, the
# legacy 30s budget would let the full 5s sleep through and the
# fragment would render.
TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 20 http://localhost:18080/timeout-2/)
RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
rm -f /tmp/mesi-timeout-body.txt
if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 1.5 && t <= 4.0)}' \
    && echo "$RESPONSE" | grep -q "After timeout include" \
    && ! echo "$RESPONSE" | grep -q "timeout2 Waited" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: include failed within ~2s (elapsed ${TIME_TOTAL}s, fragment absent, tag stripped)"
else
    echo "FAIL: mesi_timeout 2 did not abort the 5s include (elapsed ${TIME_TOTAL}s)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 46: mesi_timeout 30 — 10s include succeeds (#184) ==="
# Floor 9.5s proves the complete backend sleep happened (cold URL —
# never fetched before; failures are never cached); the ceiling keeps
# the assertion well under the 30s budget's own bound.
TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 40 http://localhost:18080/timeout-30/)
RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
rm -f /tmp/mesi-timeout-body.txt
if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 9.5 && t <= 25.0)}' \
    && echo "$RESPONSE" | grep -q "timeout30 Waited 10" \
    && echo "$RESPONSE" | grep -q "After slow include"; then
    echo "PASS: 10s include succeeded under mesi_timeout 30 (elapsed ${TIME_TOTAL}s)"
else
    echo "FAIL: mesi_timeout 30 did not let the 10s include through (elapsed ${TIME_TOTAL}s)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 47: mesi_timeout unset — default 30s aborts a 31s include (#184) ==="
# The root location (/) has no mesi_timeout directive → the documented
# 30s default on the byte-identical legacy positional path. Backend
# sleeps 31s: the budget fires at ~30s (no fragment, chrome intact, no
# raw tag). Elapsed must be >= 28s (a smaller default would drop below
# the floor) and the fragment must be absent — the issue's proposed
# "0 = no timeout" default would render it at ~31s, so the content
# check pins the default to a finite 30s budget.
TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 55 http://localhost:18080/timeout-default.html)
RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
rm -f /tmp/mesi-timeout-body.txt
if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 28.0 && t <= 40.0)}' \
    && echo "$RESPONSE" | grep -q "After default include" \
    && ! echo "$RESPONSE" | grep -q "timeoutdefault Waited" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: unset mesi_timeout aborted the 31s include at ~30s (elapsed ${TIME_TOTAL}s)"
else
    echo "FAIL: unset mesi_timeout no longer behaves like the 30s default (elapsed ${TIME_TOTAL}s)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 48: mesi_timeout merge — child inherits the parent's value (#184) ==="
# Parent sets 2, the nested child location has no directive: the child
# must inherit 2 through ngx_conf_merge_value over the unset sentinel
# (5s include aborted on both the parent URL and the child URL). A
# child that wrongly kept the unset sentinel would let the 5s fragment
# through and fail the content check.
for TIMEOUT_URL in http://localhost:18080/timeout-merge-inherit/ \
                   http://localhost:18080/timeout-merge-inherit/child/; do
    TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 20 "$TIMEOUT_URL")
    RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
    if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 1.5 && t <= 4.0)}' \
        && echo "$RESPONSE" | grep -q "After timeout include" \
        && ! echo "$RESPONSE" | grep -q "timeout2 Waited" \
        && ! echo "$RESPONSE" | grep -q '<esi:include'; then
        echo "PASS: ${TIMEOUT_URL} aborted the 5s include at ~2s (elapsed ${TIME_TOTAL}s)"
    else
        echo "FAIL: ${TIMEOUT_URL} did not inherit/keep mesi_timeout 2 (elapsed ${TIME_TOTAL}s)"
        echo "Response: $RESPONSE"
        rm -f /tmp/mesi-timeout-body.txt
        exit 1
    fi
    rm -f /tmp/mesi-timeout-body.txt
done

echo "=== Test 49: mesi_timeout merge — child's own value overrides the parent (#184) ==="
# Parent sets 2, the nested child sets 30: the child must win (the 5s
# include arrives complete — floor 4.5s proves the full sleep ran) and
# the parent itself must keep aborting at 2s.
TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 25 http://localhost:18080/timeout-merge-override/)
RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
rm -f /tmp/mesi-timeout-body.txt
if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 1.5 && t <= 4.0)}' \
    && echo "$RESPONSE" | grep -q "After timeout include" \
    && ! echo "$RESPONSE" | grep -q "timeout2 Waited" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: parent location keeps its own mesi_timeout 2 (elapsed ${TIME_TOTAL}s)"
else
    echo "FAIL: parent location (mesi_timeout 2) did not abort the 5s include (elapsed ${TIME_TOTAL}s)"
    echo "Response: $RESPONSE"
    exit 1
fi
TIME_TOTAL=$(curl -s -o /tmp/mesi-timeout-body.txt -w "%{time_total}" --max-time 25 http://localhost:18080/timeout-merge-override/child/)
RESPONSE=$(cat /tmp/mesi-timeout-body.txt)
rm -f /tmp/mesi-timeout-body.txt
if awk -v t="$TIME_TOTAL" 'BEGIN {exit !(t >= 4.5 && t <= 20.0)}' \
    && echo "$RESPONSE" | grep -q "timeout2 Waited 5" \
    && echo "$RESPONSE" | grep -q "After timeout include"; then
    echo "PASS: child location's mesi_timeout 30 overrode the parent's 2 (elapsed ${TIME_TOTAL}s)"
else
    echo "FAIL: child location did not override the parent's mesi_timeout (elapsed ${TIME_TOTAL}s)"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 50: Config validation — mesi_timeout boundary values (#184) ==="
# (a) Boundary classes ACCEPTED: the range minimum 1 and the cap
#     MESI_MAX_TIMEOUT_SECONDS (86400).
for GOOD in 1 86400; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_timeout ${GOOD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-timeout.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-timeout.conf' < /tmp/nginx-mesi-timeout.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-timeout.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "syntax is ok"; then
        echo "PASS: valid mesi_timeout ${GOOD} accepted by nginx -t"
    else
        echo "FAIL: nginx rejected valid mesi_timeout ${GOOD}"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (b) Format classes REJECTED: negative, non-integer, decimal, trailing
#     garbage, explicit plus sign — atoi/ngx_atoi-style coercion must
#     never turn these into a plausible budget ("-1" wraps when cast,
#     "1.5" truncates, "abc" → 0 would fail every include).
for BAD in '-1' 'abc' '1.5' '3foo' '+1'; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_timeout ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-timeout.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-timeout.conf' < /tmp/nginx-mesi-timeout.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-timeout.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "must be a positive integer"; then
        echo "PASS: invalid mesi_timeout ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject invalid mesi_timeout ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (c) Range classes REJECTED: explicit 0 (NOT "no timeout" — the core
#     fails every include immediately when Timeout <= 0), cap+1 and a
#     20-digit overflow input (the setter's early exit bounds its own
#     accumulator, so the value can never overflow ngx_int_t regardless
#     of argument length).
for BAD in 0 86401 99999999999999999999; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_timeout ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-timeout.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-timeout.conf' < /tmp/nginx-mesi-timeout.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-timeout.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "out of range"; then
        echo "PASS: out-of-range mesi_timeout ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject out-of-range mesi_timeout ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (d) Empty value REJECTED: "" must not silently become a silent 0.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_timeout "";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-timeout.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-timeout.conf' < /tmp/nginx-mesi-timeout.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-timeout.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "requires an argument"; then
    echo "PASS: empty mesi_timeout rejected by nginx -t"
else
    echo "FAIL: nginx did not reject an empty mesi_timeout"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (e) Missing argument REJECTED: a bare `mesi_timeout;` (zero args) is
#     caught by NGX_CONF_TAKE1 before the setter runs.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_timeout;' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-timeout.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-timeout.conf' < /tmp/nginx-mesi-timeout.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-timeout.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q 'invalid number of arguments in "mesi_timeout"'; then
    echo "PASS: argument-less mesi_timeout rejected by nginx -t"
else
    echo "FAIL: nginx did not reject a mesi_timeout without an argument"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

rm -f /tmp/nginx-mesi-timeout.conf

# --- mesi_max_response_size tests (#208) ---
# The backend (tests/server.py) serves /bytes/<size>, a body of exactly
# <size> bytes prefixed with a "MesiBytesPayload <size>" marker line.
# Assertions combine the marker (fragment arrived / was rejected) with
# wc -c (the whole body was delivered, not just the marker).

echo "=== Test 51: mesi_max_response_size 100 — 200-byte include rejected (#208) ==="
RESPONSE=$(curl -s --max-time 10 http://localhost:18080/max-response-100/)
if echo "$RESPONSE" | grep -q "After reject include" \
    && ! echo "$RESPONSE" | grep -q "MesiBytesPayload" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: 200-byte include rejected by mesi_max_response_size 100 (marker absent, tag stripped — fail closed, not truncated)"
    # Control: the SAME page served from the unset root location must
    # deliver the payload — proves the rejection above comes from the
    # directive, not a broken endpoint or fixture.
    CONTROL=$(curl -s --max-time 10 http://localhost:18080/max_response_reject.html)
    if echo "$CONTROL" | grep -q "MesiBytesPayload 200" \
        && echo "$CONTROL" | grep -q "After reject include" \
        && ! echo "$CONTROL" | grep -q '<esi:include'; then
        echo "PASS: control — same page on the unset root location delivers the 200-byte payload"
    else
        echo "FAIL: control — same page on the unset root location did not deliver the payload"
        echo "Response: $CONTROL"
        exit 1
    fi
else
    echo "FAIL: mesi_max_response_size 100 did not reject the 200-byte include"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 52: mesi_max_response_size 1048576 — 500 KB include succeeds (#208) ==="
curl -s --max-time 60 -o /tmp/mesi-mrs-accept.html http://localhost:18080/max-response-1m/
SIZE=$(wc -c < /tmp/mesi-mrs-accept.html | tr -d ' ')
if [ "$SIZE" -gt 512000 ] \
    && grep -q "MesiBytesPayload 512000" /tmp/mesi-mrs-accept.html \
    && grep -q "After accept include" /tmp/mesi-mrs-accept.html \
    && ! grep -q '<esi:include' /tmp/mesi-mrs-accept.html; then
    echo "PASS: 500 KB include delivered in full under mesi_max_response_size 1048576 ($SIZE bytes)"
else
    echo "FAIL: mesi_max_response_size 1048576 did not deliver the 500 KB include (size $SIZE)"
    head -c 500 /tmp/mesi-mrs-accept.html
    rm -f /tmp/mesi-mrs-accept.html
    exit 1
fi
rm -f /tmp/mesi-mrs-accept.html

echo "=== Test 53: mesi_max_response_size 0 — unlimited, 50 MB include succeeds (#208) ==="
curl -s --max-time 120 -o /tmp/mesi-mrs-unlimited.html http://localhost:18080/max-response-0/
SIZE=$(wc -c < /tmp/mesi-mrs-unlimited.html | tr -d ' ')
if [ "$SIZE" -gt 52428800 ] \
    && grep -q "MesiBytesPayload 52428800" /tmp/mesi-mrs-unlimited.html \
    && grep -q "After unlimited include" /tmp/mesi-mrs-unlimited.html \
    && ! grep -q '<esi:include' /tmp/mesi-mrs-unlimited.html; then
    echo "PASS: 50 MB include delivered in full under mesi_max_response_size 0 (unlimited, $SIZE bytes)"
else
    echo "FAIL: mesi_max_response_size 0 did not behave as unlimited (size $SIZE)"
    head -c 500 /tmp/mesi-mrs-unlimited.html
    rm -f /tmp/mesi-mrs-unlimited.html
    exit 1
fi
rm -f /tmp/mesi-mrs-unlimited.html

echo "=== Test 54: mesi_max_response_size unset — backward compat, 10 MB + 1 include succeeds (#208) ==="
# The location never sets the directive → the legacy positional path,
# where libgomesi leaves MaxResponseSize at 0 (unlimited). The body is
# 10 MB + 1 byte: the issue's proposed implicit 10 MB default would
# reject it, so a passing test pins "unset → unlimited"
# byte-identical to pre-#208 behaviour.
curl -s --max-time 60 -o /tmp/mesi-mrs-unset.html http://localhost:18080/max-response-unset/
SIZE=$(wc -c < /tmp/mesi-mrs-unset.html | tr -d ' ')
if [ "$SIZE" -gt 10485761 ] \
    && grep -q "MesiBytesPayload 10485761" /tmp/mesi-mrs-unset.html \
    && grep -q "After unset include" /tmp/mesi-mrs-unset.html \
    && ! grep -q '<esi:include' /tmp/mesi-mrs-unset.html; then
    echo "PASS: unset mesi_max_response_size stayed unlimited — 10 MB + 1 include delivered ($SIZE bytes)"
else
    echo "FAIL: unset mesi_max_response_size did not behave as unlimited (size $SIZE)"
    head -c 500 /tmp/mesi-mrs-unset.html
    rm -f /tmp/mesi-mrs-unset.html
    exit 1
fi
rm -f /tmp/mesi-mrs-unset.html

echo "=== Test 55: mesi_max_response_size merge — inherit 100 / child 0 overrides (#208) ==="
# All four merge locations serve max_response_merge.html (a 301-byte
# /bytes/301 include — a size no other fixture uses). WHY a distinct
# size: the suite's /cache/ locations initialize libgomesi's
# process-wide shared cache, which every parse attaches to
# (libgomesi.go applySharedConfig) and which serves cache hits BEFORE
# the core's MaxResponseSize check (mesi/fetch.go:204 returns ahead of
# the fetch.go:288 size branch) — reusing a size whose fetch already
# SUCCEEDED under a different cap (e.g. the /bytes/200 control of
# Test 51) would be served from cache here and bypass the directive.
# Rejected fetches are never cached (fetch.go caches only on success),
# so the three reject assertions below stay deterministic.
# Inherit: parent sets 100, the nested child has no directive — the
# child must inherit 100 through ngx_conf_merge_off_value over the
# unset sentinel (the 301-byte include is rejected on BOTH URLs). A
# child that wrongly kept the sentinel would deliver the payload.
for MRS_URL in http://localhost:18080/max-response-merge-inherit/ \
               http://localhost:18080/max-response-merge-inherit/child/; do
    RESPONSE=$(curl -s --max-time 10 "$MRS_URL")
    if echo "$RESPONSE" | grep -q "After merge include" \
        && ! echo "$RESPONSE" | grep -q "MesiBytesPayload" \
        && ! echo "$RESPONSE" | grep -q '<esi:include'; then
        echo "PASS: ${MRS_URL} rejected the 301-byte include (inherited mesi_max_response_size 100)"
    else
        echo "FAIL: ${MRS_URL} did not inherit/keep mesi_max_response_size 100"
        echo "Response: $RESPONSE"
        exit 1
    fi
done
# Override: parent keeps rejecting, the child's explicit 0 must arrive
# — this is what proves 0 is STORED as a configured value (the
# unset sentinel is -1, not 0), not collapsed to unset or
# to the parent's 100 at merge time.
RESPONSE=$(curl -s --max-time 10 http://localhost:18080/max-response-merge-override/)
if echo "$RESPONSE" | grep -q "After merge include" \
    && ! echo "$RESPONSE" | grep -q "MesiBytesPayload" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: parent location keeps its own mesi_max_response_size 100 (301-byte include rejected)"
else
    echo "FAIL: parent location (mesi_max_response_size 100) did not reject the 301-byte include"
    echo "Response: $RESPONSE"
    exit 1
fi
RESPONSE=$(curl -s --max-time 10 http://localhost:18080/max-response-merge-override/child/)
if echo "$RESPONSE" | grep -q "MesiBytesPayload 301" \
    && echo "$RESPONSE" | grep -q "After merge include" \
    && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: child location's explicit mesi_max_response_size 0 overrode the parent's 100 (unlimited)"
else
    echo "FAIL: child location's mesi_max_response_size 0 did not override the parent's 100"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 56: Config validation — mesi_max_response_size boundary values (#208) ==="
# (a) Boundary classes ACCEPTED: explicit 0 (unlimited — the sentinel
#     is -1, so 0 IS storable), the range minimum is covered by 0/1,
#     a normal byte count, and the cap MESI_MAX_MAX_RESPONSE_SIZE
#     (9223372036854775806 = math.MaxInt64 - 1, the value keeping the
#     core's MaxResponseSize+1 LimitReader bound positive).
for GOOD in 0 1 1048576 9223372036854775806; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_response_size ${GOOD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-response-size.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-response-size.conf' < /tmp/nginx-mesi-max-response-size.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-response-size.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "syntax is ok"; then
        echo "PASS: valid mesi_max_response_size ${GOOD} accepted by nginx -t"
    else
        echo "FAIL: nginx rejected valid mesi_max_response_size ${GOOD}"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (b) Format classes REJECTED: negative, sign, decimal, non-integer,
#     trailing garbage, and a k/m/g size SUFFIX — the issue sketched
#     ngx_parse_size (suffix grammar), but every other platform landed
#     plain-integer bytes (Apache parse_nonneg_off / php-ext IS_LONG /
#     CLI int64), so the cross-platform contract wins and "10m" fails
#     config load instead of meaning 10485760 here but being invalid
#     on Apache/php/CLI.
for BAD in '-1' '+1' '1.5' 'abc' '3foo' '100abc' '10m'; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_response_size ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-response-size.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-response-size.conf' < /tmp/nginx-mesi-max-response-size.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-response-size.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "must be a non-negative integer"; then
        echo "PASS: invalid mesi_max_response_size ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject invalid mesi_max_response_size ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (c) Range classes REJECTED: cap+1 (= math.MaxInt64 — the core's
#     MaxResponseSize+1 LimitReader bound would wrap negative and
#     silently render an empty body, #448), cap+2 (overflows int64)
#     and a 20-digit overflow input — the setter's per-digit guard
#     checks against the cap BEFORE the multiply, so no intermediate
#     ever wraps off_t regardless of argument length.
for BAD in 9223372036854775807 9223372036854775808 99999999999999999999; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_response_size ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-response-size.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-response-size.conf' < /tmp/nginx-mesi-max-response-size.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-response-size.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "out of range"; then
        echo "PASS: out-of-range mesi_max_response_size ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject out-of-range mesi_max_response_size ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (d) Empty value REJECTED: "" must not silently become a silent 0
#     (= unlimited).
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_response_size "";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-max-response-size.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-response-size.conf' < /tmp/nginx-mesi-max-response-size.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-response-size.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "requires an argument"; then
    echo "PASS: empty mesi_max_response_size rejected by nginx -t"
else
    echo "FAIL: nginx did not reject an empty mesi_max_response_size"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (e) Missing argument REJECTED: a bare `mesi_max_response_size;`
#     (zero args) is caught by NGX_CONF_TAKE1 before the setter runs.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_response_size;' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-max-response-size.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-response-size.conf' < /tmp/nginx-mesi-max-response-size.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-response-size.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q 'invalid number of arguments in "mesi_max_response_size"'; then
    echo "PASS: argument-less mesi_max_response_size rejected by nginx -t"
else
    echo "FAIL: nginx did not reject a mesi_max_response_size without an argument"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

rm -f /tmp/nginx-mesi-max-response-size.conf

# --- mesi_max_concurrent_requests tests (#214) ---
# The backend (tests/server.py) serves /hold/<millis>/<label>: it
# records request concurrency in a peak counter (reset/read via
# /track/reset and /track/max, proxied through the nginx /track/
# location), holds each request for <millis>, then returns a
# "<label> Held <millis>" fragment. Each page fans out to 20 DISTINCT
# labels so every include reaches the backend (the process-wide
# shared libgomesi cache initialized by the suite's /cache/ locations
# would otherwise dedup repeat fetches and never touch the counter —
# hence every fixture owns its own label range). The peak counter is a
# deterministic observable — DEVIATION from the issue's wall-clock AC
# ("> 3*2s, < 20*2s"): mirrors Apache Test 36 (#170), which asserts
# the semaphore invariant directly instead of timing (the issue's
# "> 3*2s" floor also miscounts: 20 includes / 3 slots x 1.5s holds
# spans ~7 waves, so any fixed small wall-clock floor is both flaky
# and unfalsifiable against an uncapped parse on a fast backend).
#
# Fan-out bound for the "unlimited" cases: MESIParse drains includes
# through a worker pool of min(MaxWorkers=NumCPU*4, 20) goroutines
# (mesi/parser.go), i.e. at least 4 in any container — with 1500 ms
# holds, an uncapped parse must show peak >= 4, while a cap of 3 can
# never exceed 3 (hard semaphore invariant, mesi/fetch.go).

echo "=== Test 57: mesi_max_concurrent_requests 3 — 20 includes funneled through 3 slots (#214) ==="
# /mcr-3/ sets ONLY mesi_max_concurrent_requests 3 — an mcr-only
# config routed through ParseJson (the routing condition's mcr arm),
# consistent with the timeout/max-response-size keys being absent
# (that absence is code-verified via has_timeout/has_maxrs, not
# observable through the gauge). Assertions: peak <= 3 is
# the cap itself (an uncapped parse would reach >= 4 per the fan-out
# bound above, so this discriminates a broken route / a key that never
# arrived); peak >= 2 proves the cap is a multi-slot queue, not a
# serialisation to 1 (an exact peak == 3 would additionally require
# all three first-wave dials to overlap — scheduling-dependent,
# deliberately not asserted). All 20 fragments must arrive: includes
# beyond the cap are QUEUED, not dropped.
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mcr-cap.html http://localhost:18080/mcr-3/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-cap.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 2 ] && [ "$PEAK" -le 3 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mcr3 include" /tmp/mesi-mcr-cap.html \
    && ! grep -q '<esi:include' /tmp/mesi-mcr-cap.html; then
    echo "PASS: peak concurrent fetches $PEAK <= 3 (cap), >= 2 (parallel slots), all 20 fragments queued and delivered"
else
    echo "FAIL: mesi_max_concurrent_requests 3 did not funnel the 20 includes (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mcr-cap.html
    rm -f /tmp/mesi-mcr-cap.html
    exit 1
fi
rm -f /tmp/mesi-mcr-cap.html

echo "=== Test 58: mesi_max_concurrent_requests 0 — explicit unlimited, fan-out unthrottled (#214) ==="
# /mcr-0/ stores an explicit 0: the value must reach the core as
# "unlimited" (ParseJson "maxConcurrentRequests":0 — an explicit 0
# rejected Go-side would make ParseJson return NULL and the request
# would fail closed with an empty body). Peak >= 4 distinguishes this
# from the cap-3 location; the fan-out bound above explains the floor.
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mcr-zero.html http://localhost:18080/mcr-0/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-zero.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mcr0 include" /tmp/mesi-mcr-zero.html \
    && ! grep -q '<esi:include' /tmp/mesi-mcr-zero.html; then
    echo "PASS: peak concurrent fetches $PEAK >= 4 under explicit mesi_max_concurrent_requests 0 (unlimited), all 20 fragments delivered"
else
    echo "FAIL: mesi_max_concurrent_requests 0 did not behave as unlimited (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mcr-zero.html
    rm -f /tmp/mesi-mcr-zero.html
    exit 1
fi
rm -f /tmp/mesi-mcr-zero.html

echo "=== Test 59: mesi_max_concurrent_requests unset — backward compat, fan-out unthrottled (#214) ==="
# /mcr-unset/ never sets the directive → the legacy parse path (no
# ParseJson key rendered) with MaxConcurrentRequests left at 0 =
# unlimited, byte-identical to pre-#214 behaviour. Peak >= 4 pins that
# unset never throttles.
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mcr-unset.html http://localhost:18080/mcr-unset/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-unset.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mcr-unset include" /tmp/mesi-mcr-unset.html \
    && ! grep -q '<esi:include' /tmp/mesi-mcr-unset.html; then
    echo "PASS: unset mesi_max_concurrent_requests stayed unlimited — peak $PEAK >= 4, all 20 fragments delivered"
else
    echo "FAIL: unset mesi_max_concurrent_requests did not behave as unlimited (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mcr-unset.html
    rm -f /tmp/mesi-mcr-unset.html
    exit 1
fi
rm -f /tmp/mesi-mcr-unset.html

echo "=== Test 60: mesi_max_concurrent_requests merge — child inherits the parent's cap 3 (#214) ==="
# Parent sets 3, the nested child location has no directive: the child
# must inherit 3 through ngx_conf_merge_value over the unset sentinel
# (peak <= 3 on BOTH URLs; a child that wrongly kept the sentinel
# would fan out unthrottled and peak >= 4). Each location serves its
# OWN fixture (disjoint label ranges, distinct chrome text) so the
# second run cannot be served from the shared cache with a stale peak.
for MCR_URL in http://localhost:18080/mcr-merge-inherit/ \
               http://localhost:18080/mcr-merge-inherit/child/; do
    curl -s http://localhost:18080/track/reset > /dev/null
    curl -s --max-time 60 -o /tmp/mesi-mcr-inherit.html "$MCR_URL"
    PEAK=$(curl -s http://localhost:18080/track/max)
    FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-inherit.html | wc -l | tr -d ' ')
    if [ "$PEAK" -ge 2 ] && [ "$PEAK" -le 3 ] \
        && [ "$FRAGMENTS" -eq 20 ] \
        && grep -q "After mcr-inherit" /tmp/mesi-mcr-inherit.html \
        && ! grep -q '<esi:include' /tmp/mesi-mcr-inherit.html; then
        echo "PASS: ${MCR_URL} applied the cap of 3 (peak $PEAK, fragments $FRAGMENTS)"
    else
        echo "FAIL: ${MCR_URL} did not apply a cap of 3 (peak $PEAK, fragments $FRAGMENTS)"
        head -c 500 /tmp/mesi-mcr-inherit.html
        rm -f /tmp/mesi-mcr-inherit.html
        exit 1
    fi
    rm -f /tmp/mesi-mcr-inherit.html
done

echo "=== Test 61: mesi_max_concurrent_requests merge — child's explicit 0 overrides the parent's cap 3 (#214) ==="
# Parent keeps capping (peak <= 3), the nested child's explicit 0 must
# win (peak >= 4) — this is what proves 0 is STORED as a configured
# value (the unset sentinel is -1, not 0), not collapsed to unset or
# to the parent's 3 at merge time.
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mcr-override.html http://localhost:18080/mcr-merge-override/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-override.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 2 ] && [ "$PEAK" -le 3 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mcr-override-parent include" /tmp/mesi-mcr-override.html \
    && ! grep -q '<esi:include' /tmp/mesi-mcr-override.html; then
    echo "PASS: parent location keeps its own mesi_max_concurrent_requests 3 (peak $PEAK, fragments $FRAGMENTS)"
else
    echo "FAIL: parent location (mesi_max_concurrent_requests 3) did not cap the fan-out (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mcr-override.html
    rm -f /tmp/mesi-mcr-override.html
    exit 1
fi
rm -f /tmp/mesi-mcr-override.html
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mcr-override-child.html http://localhost:18080/mcr-merge-override/child/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-override-child.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mcr-override-child include" /tmp/mesi-mcr-override-child.html \
    && ! grep -q '<esi:include' /tmp/mesi-mcr-override-child.html; then
    echo "PASS: child location's explicit mesi_max_concurrent_requests 0 overrode the parent's 3 (peak $PEAK >= 4, unlimited)"
else
    echo "FAIL: child location's explicit mesi_max_concurrent_requests 0 did not override the parent's 3 (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mcr-override-child.html
    rm -f /tmp/mesi-mcr-override-child.html
    exit 1
fi
rm -f /tmp/mesi-mcr-override-child.html

echo "=== Test 62: Config validation — mesi_max_concurrent_requests boundary values (#214) ==="
# (a) Boundary classes ACCEPTED: explicit 0 (unlimited — the sentinel
#     is -1, so 0 IS storable), 1 (tightest possible cap), a typical
#     cap, and MESI_MAX_MAX_CONCURRENT_REQUESTS (999999999, the #170
#     transport-derived cap mirrored across every platform).
for GOOD in 0 1 3 999999999; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_concurrent_requests ${GOOD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-concurrent-requests.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-concurrent-requests.conf' < /tmp/nginx-mesi-max-concurrent-requests.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-concurrent-requests.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "syntax is ok"; then
        echo "PASS: valid mesi_max_concurrent_requests ${GOOD} accepted by nginx -t"
    else
        echo "FAIL: nginx rejected valid mesi_max_concurrent_requests ${GOOD}"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (b) Format classes REJECTED: negative, sign, decimal, non-integer,
#     trailing garbage — the issue sketched ngx_conf_set_num_slot
#     (ngx_atoi), which rejects "-" but silently truncates "1.5" and
#     has no upper bound; a negative must never reach the core, where
#     #329 warns and normalizes it to 0 = unlimited (a malformed
#     explicit value would silently pass as the documented one).
for BAD in '-1' '+1' '1.5' 'abc' '3foo'; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_concurrent_requests ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-concurrent-requests.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-concurrent-requests.conf' < /tmp/nginx-mesi-max-concurrent-requests.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-concurrent-requests.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "must be a non-negative integer"; then
        echo "PASS: invalid mesi_max_concurrent_requests ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject invalid mesi_max_concurrent_requests ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (c) Range classes REJECTED: cap+1 (1000000000 — the value ngx_atoi
#     would have accepted and every other platform rejects) and a
#     20-digit overflow input (the setter's per-digit guard checks
#     against the cap BEFORE the multiply, so no intermediate can
#     ever overflow ngx_int_t regardless of argument length).
for BAD in 1000000000 99999999999999999999; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_concurrent_requests ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-concurrent-requests.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-concurrent-requests.conf' < /tmp/nginx-mesi-max-concurrent-requests.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-concurrent-requests.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "out of range"; then
        echo "PASS: out-of-range mesi_max_concurrent_requests ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject out-of-range mesi_max_concurrent_requests ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (d) Empty value REJECTED: "" must not silently become a silent 0
#     (= unlimited).
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_concurrent_requests "";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-max-concurrent-requests.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-concurrent-requests.conf' < /tmp/nginx-mesi-max-concurrent-requests.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-concurrent-requests.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "requires an argument"; then
    echo "PASS: empty mesi_max_concurrent_requests rejected by nginx -t"
else
    echo "FAIL: nginx did not reject an empty mesi_max_concurrent_requests"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (e) Missing argument REJECTED: a bare `mesi_max_concurrent_requests;`
#     (zero args) is caught by NGX_CONF_TAKE1 before the setter runs.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_concurrent_requests;' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-max-concurrent-requests.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-concurrent-requests.conf' < /tmp/nginx-mesi-max-concurrent-requests.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-concurrent-requests.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q 'invalid number of arguments in "mesi_max_concurrent_requests"'; then
    echo "PASS: argument-less mesi_max_concurrent_requests rejected by nginx -t"
else
    echo "FAIL: nginx did not reject a mesi_max_concurrent_requests without an argument"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (f) Duplicate in the same scope REJECTED: matches the "is
#     duplicate" behaviour of every ngx_conf_set_*_slot directive in
#     this module (a silent last-wins would substitute the operator's
#     intent without a word).
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_concurrent_requests 3;' \
    '      mesi_max_concurrent_requests 5;' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-max-concurrent-requests.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-concurrent-requests.conf' < /tmp/nginx-mesi-max-concurrent-requests.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-concurrent-requests.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q 'is duplicate'; then
    echo "PASS: repeated mesi_max_concurrent_requests rejected by nginx -t (is duplicate)"
else
    echo "FAIL: nginx did not reject a repeated mesi_max_concurrent_requests"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

rm -f /tmp/nginx-mesi-max-concurrent-requests.conf

echo "=== Test 63: mesi_max_concurrent_requests stress — 10 parallel renders of a 20-include page (#214) ==="
# DEVIATION from the issue's AC "ab -n 100 -c 10": ab (apache2-utils)
# is not installed in the test image, and the suite's convention for
# concurrency proofs is curl + deterministic observables (Apache
# #170's thread-safety test uses 20 parallel curls). 10 concurrent
# renders of the explicit-0 page each fan out 20 includes (~200
# backend fetches within a few seconds): every render must complete
# with all 20 fragments, its chrome and no leftover tags — a hang
# (deadlocked semaphore), an fd-exhaustion failure or a failed
# ParseJson would drop fragments or stall past --max-time.
STRESS_OK=1
for i in $(seq 1 10); do
    curl -s --max-time 60 -o /tmp/mesi-mcr-stress-$i.html http://localhost:18080/mcr-stress/ &
done
wait
for i in $(seq 1 10); do
    STRESS_FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mcr-stress-$i.html 2>/dev/null | wc -l | tr -d ' ')
    if [ "$STRESS_FRAGMENTS" -ne 20 ] \
        || ! grep -q "After mcr-stress include" /tmp/mesi-mcr-stress-$i.html \
        || grep -q '<esi:include' /tmp/mesi-mcr-stress-$i.html; then
        echo "FAIL: parallel render $i incomplete (fragments ${STRESS_FRAGMENTS:-0})"
        head -c 500 /tmp/mesi-mcr-stress-$i.html 2>/dev/null
        STRESS_OK=0
    fi
    rm -f /tmp/mesi-mcr-stress-$i.html
done
if [ "$STRESS_OK" -eq 1 ]; then
    echo "PASS: 10 parallel 20-include renders all completed (~200 fetches, no fd exhaustion, no hang)"
else
    exit 1
fi

# --- mesi_max_workers tests (#219) ---
# Observable for this directive is the DRAIN POOL plus render
# CORRECTNESS, never a timing window: MESIParse spawns
# min(MaxWorkers, job count) goroutines (mesi/parser.go) and each
# processes one include at a time, so on a flat page the backend
# peak-concurrency counter can never exceed the pool size — with
# mesi_max_workers 2 that is a hard peak == 2 (a broken route that
# never renders the "maxWorkers" key would use the library default
# pool min(NumCPU*4, 20) >= 4 and show peak >= 4 instead).
# Fan-out floor for the "library default" cases (explicit 0 / unset /
# cap 100): the pool is min(cap or NumCPU*4, 20) goroutines, i.e. at
# least 4 in any container — with 1500 ms holds an unthrottled parse
# must show peak >= 4 (same deterministic bound Apache #171 Tests
# 41/42 and this suite's #214 tests rely on). Each page fans out to 20
# DISTINCT /hold labels (801-820, 901-920, 1001-1020, 1101-1120,
# 1201-1220, 1301-1320, 1401-1420, 1501-1520) so every include
# reaches the backend (duplicate URLs would be served from the
# process-wide shared cache, #484) and no label collides with any
# other fixture.

echo "=== Test 64: mesi_max_workers 2 vs 100 — deep-nesting stress completes correctly (#219) ==="
# The issue's AC: a four-level nested chain (workers_deep.html ->
# workers_lvl_1 -> workers_lvl_2 -> workers_lvl_3 -> workers_lvl_4)
# under max_workers 2 and under max_workers 100 must BOTH complete
# correctly. The ordered marker assertion (all seven markers — 3
# STARTs, the level-4 body and 3 ENDs — in nesting order after
# flattening newlines) proves every level was fetched AND re-parsed; a
# raw tag left behind would fail the tag check. The final byte-for-byte
# comparison proves cap 2 vs cap 100 changed NOTHING in the output:
# MaxWorkers bounds pool parallelism only, so the observable is
# identical fully-rendered bodies — deliberately NO timing assertions
# (the pool's scheduling is invisible in the output; mirrors Apache
# #171's Test 39 proof).
for MW_CAP in 2 100; do
    curl -s --max-time 60 -o /tmp/mesi-mw-deep-$MW_CAP.html "http://localhost:18080/mw-$MW_CAP/workers_deep.html"
    tr -d '\n' < /tmp/mesi-mw-deep-$MW_CAP.html > /tmp/mesi-mw-deep-$MW_CAP.flat
    if grep -q "MW-LVL-1-START.*MW-LVL-2-START.*MW-LVL-3-START.*MW-LVL-4-BODY.*MW-LVL-3-END.*MW-LVL-2-END.*MW-LVL-1-END" /tmp/mesi-mw-deep-$MW_CAP.flat \
        && grep -q "After deep include" /tmp/mesi-mw-deep-$MW_CAP.html \
        && ! grep -q '<esi:include' /tmp/mesi-mw-deep-$MW_CAP.html; then
        echo "PASS: mesi_max_workers $MW_CAP — four-level nested chain fully expanded (all level markers present, in order, no raw tags)"
    else
        echo "FAIL: mesi_max_workers $MW_CAP did not complete the deep-nesting stress correctly"
        head -c 500 /tmp/mesi-mw-deep-$MW_CAP.html
        rm -f /tmp/mesi-mw-deep-2.html /tmp/mesi-mw-deep-100.html /tmp/mesi-mw-deep-2.flat /tmp/mesi-mw-deep-100.flat
        exit 1
    fi
done
if cmp -s /tmp/mesi-mw-deep-2.html /tmp/mesi-mw-deep-100.html; then
    echo "PASS: max_workers 2 and max_workers 100 deliver byte-identical fully-rendered bodies"
else
    echo "FAIL: deep-nesting bodies differ between mesi_max_workers 2 and 100"
    diff /tmp/mesi-mw-deep-2.html /tmp/mesi-mw-deep-100.html | head -20 || true
    rm -f /tmp/mesi-mw-deep-2.html /tmp/mesi-mw-deep-100.html /tmp/mesi-mw-deep-2.flat /tmp/mesi-mw-deep-100.flat
    exit 1
fi
rm -f /tmp/mesi-mw-deep-2.html /tmp/mesi-mw-deep-100.html /tmp/mesi-mw-deep-2.flat /tmp/mesi-mw-deep-100.flat

echo "=== Test 65: mesi_max_workers 2 — 20 includes drain through a 2-goroutine pool (#219) ==="
# /mw-2/ sets ONLY mesi_max_workers 2 — an mw-only config routed
# through ParseJson (the routing condition's mw arm), consistent
# with the timeout / max-response-size / max-concurrent-requests
# keys being absent (code-verified via has_timeout/has_maxrs/
# has_maxcr, not observable through the gauge).
# Assertions: peak == 2 — the upper bound is the hard pool invariant
# (workerCount = min(2, 20), each goroutine fetches one include at a
# time, mesi/parser.go; an unrouted parse would reach >= 4 and fail
# this bound) and the lower bound proves the pool has both slots
# working in parallel rather than serializing to 1 (both goroutines
# grab their first buffered job within microseconds while each backend
# hold lasts 1500 ms — the deterministic bound Apache Test 40/#171
# pins with the same fixture shape). All 20 fragments must arrive:
# includes beyond the pool are queued in the jobs channel, not dropped.
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mw-pool2.html http://localhost:18080/mw-2/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mw-pool2.html | wc -l | tr -d ' ')
if [ "$PEAK" -eq 2 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mw2 include" /tmp/mesi-mw-pool2.html \
    && ! grep -q '<esi:include' /tmp/mesi-mw-pool2.html; then
    echo "PASS: peak concurrent fetches $PEAK == 2 (drain-pool bound), all 20 fragments queued and delivered"
else
    echo "FAIL: mesi_max_workers 2 did not bound the drain pool (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mw-pool2.html
    rm -f /tmp/mesi-mw-pool2.html
    exit 1
fi
rm -f /tmp/mesi-mw-pool2.html

echo "=== Test 66: mesi_max_workers 100 — pool clamps to job count, page renders completely (#219) ==="
# /mw-100/: workerCount = min(100, 20) = 20 — an over-cap pool is a
# documented no-op (the core additionally clamps to the job count, so
# an over-large value never over-spawns, #171's cap rationale). The 20
# includes drain unthrottled: peak in [4, 20] (floor = the library
# pool bound above, ceiling = fixture size) and, the issue's actual
# AC, the page must render COMPLETELY — all fragments, chrome, no raw
# tags. A value of 100 that never arrived would still pass a bare
# "renders" check, so the peak >= 4 floor additionally pins that the
# parse did not serialize to 1 (e.g. a mis-routed "1").
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mw-pool100.html http://localhost:18080/mw-100/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mw-pool100.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] && [ "$PEAK" -le 20 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mw100 include" /tmp/mesi-mw-pool100.html \
    && ! grep -q '<esi:include' /tmp/mesi-mw-pool100.html; then
    echo "PASS: mesi_max_workers 100 rendered completely — peak $PEAK in [4, 20] (job-count clamp), all 20 fragments delivered"
else
    echo "FAIL: mesi_max_workers 100 did not drain unthrottled and complete (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mw-pool100.html
    rm -f /tmp/mesi-mw-pool100.html
    exit 1
fi
rm -f /tmp/mesi-mw-pool100.html

echo "=== Test 67: mesi_max_workers 0 — explicit library default, pool unthrottled (#219) ==="
# /mw-0/ stores an explicit 0: the value must reach the core as
# "library default" (ParseJson "maxWorkers":0 — an explicit 0
# rejected Go-side would make ParseJson return NULL and the request
# would fail closed with an empty body). Peak >= 4 (pool
# min(NumCPU*4, 20)) distinguishes this from the pool-2 location; the
# fan-out floor above explains the bound.
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mw-zero.html http://localhost:18080/mw-0/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mw-zero.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mw0 include" /tmp/mesi-mw-zero.html \
    && ! grep -q '<esi:include' /tmp/mesi-mw-zero.html; then
    echo "PASS: peak concurrent fetches $PEAK >= 4 under explicit mesi_max_workers 0 (library default), all 20 fragments delivered"
else
    echo "FAIL: mesi_max_workers 0 did not behave as the library default (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mw-zero.html
    rm -f /tmp/mesi-mw-zero.html
    exit 1
fi
rm -f /tmp/mesi-mw-zero.html

echo "=== Test 68: mesi_max_workers unset — backward compat, pool unthrottled (#219) ==="
# /mw-unset/ never sets the directive → the legacy parse path (no
# ParseJson key rendered) with MaxWorkers left at 0 → library default
# NumCPU*4, byte-identical to pre-#219 behaviour. Peak >= 4 pins that
# unset never throttles the pool.
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mw-unset.html http://localhost:18080/mw-unset/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mw-unset.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mw-unset include" /tmp/mesi-mw-unset.html \
    && ! grep -q '<esi:include' /tmp/mesi-mw-unset.html; then
    echo "PASS: unset mesi_max_workers stayed at the library default — peak $PEAK >= 4, all 20 fragments delivered"
else
    echo "FAIL: unset mesi_max_workers did not behave as the library default (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mw-unset.html
    rm -f /tmp/mesi-mw-unset.html
    exit 1
fi
rm -f /tmp/mesi-mw-unset.html

echo "=== Test 69: mesi_max_workers merge — child inherits the parent's cap 2 (#219) ==="
# Parent sets 2, the nested child location has no directive: the child
# must inherit 2 through ngx_conf_merge_value over the unset sentinel
# (peak == 2 on BOTH URLs; a child that wrongly kept the sentinel
# would drain unthrottled and peak >= 4). Each location serves its
# OWN fixture (disjoint label ranges) so the second run cannot be
# served from the shared cache with a stale peak.
for MW_URL in http://localhost:18080/mw-merge-inherit/ \
              http://localhost:18080/mw-merge-inherit/child/; do
    curl -s http://localhost:18080/track/reset > /dev/null
    curl -s --max-time 60 -o /tmp/mesi-mw-inherit.html "$MW_URL"
    PEAK=$(curl -s http://localhost:18080/track/max)
    FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mw-inherit.html | wc -l | tr -d ' ')
    if [ "$PEAK" -eq 2 ] \
        && [ "$FRAGMENTS" -eq 20 ] \
        && grep -q "After mw-inherit" /tmp/mesi-mw-inherit.html \
        && ! grep -q '<esi:include' /tmp/mesi-mw-inherit.html; then
        echo "PASS: ${MW_URL} applied the cap of 2 (peak $PEAK, fragments $FRAGMENTS)"
    else
        echo "FAIL: ${MW_URL} did not apply a cap of 2 (peak $PEAK, fragments $FRAGMENTS)"
        head -c 500 /tmp/mesi-mw-inherit.html
        rm -f /tmp/mesi-mw-inherit.html
        exit 1
    fi
    rm -f /tmp/mesi-mw-inherit.html
done

echo "=== Test 70: mesi_max_workers merge — child's explicit 0 overrides the parent's cap 2 (#219) ==="
# Parent keeps capping (peak == 2), the nested child's explicit 0 must
# win (peak >= 4) — this is what proves 0 is STORED as a configured
# value (the unset sentinel is -1, not 0), not collapsed to unset or
# to the parent's 2 at merge time (a sentinel-collapsed child would
# inherit the parent's cap and show peak == 2).
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mw-override.html http://localhost:18080/mw-merge-override/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mw-override.html | wc -l | tr -d ' ')
if [ "$PEAK" -eq 2 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mw-override-parent include" /tmp/mesi-mw-override.html \
    && ! grep -q '<esi:include' /tmp/mesi-mw-override.html; then
    echo "PASS: parent location keeps its own mesi_max_workers 2 (peak $PEAK, fragments $FRAGMENTS)"
else
    echo "FAIL: parent location (mesi_max_workers 2) did not cap the drain pool (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mw-override.html
    rm -f /tmp/mesi-mw-override.html
    exit 1
fi
rm -f /tmp/mesi-mw-override.html
curl -s http://localhost:18080/track/reset > /dev/null
curl -s --max-time 60 -o /tmp/mesi-mw-override-child.html http://localhost:18080/mw-merge-override/child/
PEAK=$(curl -s http://localhost:18080/track/max)
FRAGMENTS=$(grep -o "Held 1500" /tmp/mesi-mw-override-child.html | wc -l | tr -d ' ')
if [ "$PEAK" -ge 4 ] \
    && [ "$FRAGMENTS" -eq 20 ] \
    && grep -q "After mw-override-child include" /tmp/mesi-mw-override-child.html \
    && ! grep -q '<esi:include' /tmp/mesi-mw-override-child.html; then
    echo "PASS: child location's explicit mesi_max_workers 0 overrode the parent's 2 (peak $PEAK >= 4, library default)"
else
    echo "FAIL: child location's explicit mesi_max_workers 0 did not override the parent's 2 (peak $PEAK, fragments $FRAGMENTS)"
    head -c 500 /tmp/mesi-mw-override-child.html
    rm -f /tmp/mesi-mw-override-child.html
    exit 1
fi
rm -f /tmp/mesi-mw-override-child.html

echo "=== Test 71: Config validation — mesi_max_workers boundary values (#219) ==="
# (a) Boundary classes ACCEPTED: explicit 0 (library default — the
#     sentinel is -1, so 0 IS storable), 1 (serializes token
#     processing — the documented debugging value), 4 (the shape of a
#     default NumCPU*4 value), 100 (a typical cap) and
#     MESI_MAX_MAX_WORKERS (999999999, the #171 transport-derived cap
#     mirrored across every platform).
for GOOD in 0 1 4 100 999999999; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_workers ${GOOD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-workers.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-workers.conf' < /tmp/nginx-mesi-max-workers.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-workers.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "syntax is ok"; then
        echo "PASS: valid mesi_max_workers ${GOOD} accepted by nginx -t"
    else
        echo "FAIL: nginx rejected valid mesi_max_workers ${GOOD}"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (b) Format classes REJECTED: negative, sign, decimal, non-integer,
#     trailing garbage — the issue sketched ngx_conf_set_num_slot
#     (ngx_atoi), which rejects "-" but silently truncates "1.5" and
#     has no upper bound; a negative must never reach the core, where
#     ANY value <= 0 is silently substituted with NumCPU*4 and NO
#     warning (#456 — no #329-style diagnostic exists for
#     MaxWorkers), i.e. a malformed explicit value would silently pass
#     as the documented "library default".
for BAD in '-1' '+1' '1.5' 'abc' '3foo'; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_workers ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-workers.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-workers.conf' < /tmp/nginx-mesi-max-workers.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-workers.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "must be a non-negative integer"; then
        echo "PASS: invalid mesi_max_workers ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject invalid mesi_max_workers ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (c) Range classes REJECTED: cap+1 (1000000000 — the value ngx_atoi
#     would have accepted and every other platform rejects) and a
#     20-digit overflow input (the setter's per-digit guard checks
#     against the cap BEFORE the multiply, so no intermediate can
#     ever overflow ngx_int_t regardless of argument length).
for BAD in 1000000000 99999999999999999999; do
    printf '%b\n' \
        'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
        'error_log stderr warn;' \
        'events {}' \
        'http {' \
        '  server {' \
        '    listen 18081;' \
        '    location / {' \
        '      enable_mesi on;' \
        "      mesi_max_workers ${BAD};" \
        '    }' \
        '  }' \
        '}' > /tmp/nginx-mesi-max-workers.conf
    docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-workers.conf' < /tmp/nginx-mesi-max-workers.conf
    NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-workers.conf 2>&1) || true
    if echo "$NGINX_T_OUT" | grep -q "out of range"; then
        echo "PASS: out-of-range mesi_max_workers ${BAD} rejected by nginx -t"
    else
        echo "FAIL: nginx did not reject out-of-range mesi_max_workers ${BAD} with the expected error"
        echo "nginx -t output: $NGINX_T_OUT"
        exit 1
    fi
done

# (d) Empty value REJECTED: "" must not silently become a silent 0
#     (= library default).
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_workers "";' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-max-workers.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-workers.conf' < /tmp/nginx-mesi-max-workers.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-workers.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q "requires an argument"; then
    echo "PASS: empty mesi_max_workers rejected by nginx -t"
else
    echo "FAIL: nginx did not reject an empty mesi_max_workers"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (e) Missing argument REJECTED: a bare `mesi_max_workers;` (zero
#     args) is caught by NGX_CONF_TAKE1 before the setter runs.
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_workers;' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-max-workers.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-workers.conf' < /tmp/nginx-mesi-max-workers.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-workers.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q 'invalid number of arguments in "mesi_max_workers"'; then
    echo "PASS: argument-less mesi_max_workers rejected by nginx -t"
else
    echo "FAIL: nginx did not reject a mesi_max_workers without an argument"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

# (f) Duplicate in the same scope REJECTED: matches the "is
#     duplicate" behaviour of every ngx_conf_set_*_slot directive in
#     this module (a silent last-wins would substitute the operator's
#     intent without a word).
printf '%b\n' \
    'load_module /usr/lib/nginx/modules/ngx_http_mesi_module.so;' \
    'error_log stderr warn;' \
    'events {}' \
    'http {' \
    '  server {' \
    '    listen 18081;' \
    '    location / {' \
    '      enable_mesi on;' \
    '      mesi_max_workers 2;' \
    '      mesi_max_workers 5;' \
    '    }' \
    '  }' \
    '}' > /tmp/nginx-mesi-max-workers.conf
docker compose exec -T nginx sh -c 'cat > /tmp/nginx-mesi-max-workers.conf' < /tmp/nginx-mesi-max-workers.conf
NGINX_T_OUT=$(docker compose exec -T nginx /usr/local/nginx/sbin/nginx -t -c /tmp/nginx-mesi-max-workers.conf 2>&1) || true
if echo "$NGINX_T_OUT" | grep -q 'is duplicate'; then
    echo "PASS: repeated mesi_max_workers rejected by nginx -t (is duplicate)"
else
    echo "FAIL: nginx did not reject a repeated mesi_max_workers"
    echo "nginx -t output: $NGINX_T_OUT"
    exit 1
fi

rm -f /tmp/nginx-mesi-max-workers.conf

docker compose down

echo ""
echo "=== All tests passed ==="
