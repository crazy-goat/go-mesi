#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

docker compose up -d

echo "Waiting for services to be ready..."
for i in $(seq 1 30); do
    if curl -sf -H "Host: domain.com" http://localhost:18080/ >/dev/null 2>&1; then
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
RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" -H "Host: domain.com" http://localhost:18080/)
if [ "$RESPONSE" = "200" ]; then
    echo "PASS: Traefik responds with 200"
else
    echo "FAIL: Traefik returned $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 2: ESI remove ==="
RESPONSE=$(curl -s -H "Host: domain.com" http://localhost:18080/)
if echo "$RESPONSE" | grep -q "Failed to include ESI"; then
    echo "FAIL: ESI remove content still present"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
else
    echo "PASS: ESI remove processed correctly"
fi

echo "=== Test 3: HTML content served through mesi plugin ==="
RESPONSE=$(curl -s -H "Host: domain.com" http://localhost:18080/)
if echo "$RESPONSE" | grep -q "Welcome to ESI Test"; then
    echo "PASS: HTML content served through mesi plugin"
else
    echo "FAIL: Expected HTML content missing"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
fi

echo "=== Test 4: Content-Length correctness ==="
HEADERS=$(curl -s -D - -H "Host: domain.com" http://localhost:18080/ -o /tmp/mesi-traefik-body.txt 2>/dev/null)
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
RESPONSE=$(curl -s -H "Host: domain.com" http://localhost:18080/)
if echo "$RESPONSE" | grep -q "<esi:include"; then
    echo "FAIL: Raw <esi:include> tag still present in response"
    echo "Response: $RESPONSE"
    docker compose down
    exit 1
else
    echo "PASS: Raw ESI include tag removed from response"
fi

echo "=== Test 6: Non-HTML content passthrough ==="
HEADERS=$(curl -sI -H "Host: domain.com" http://localhost:18080/esi)
CT=$(echo "$HEADERS" | grep -i "Content-Type" || true)
if echo "$CT" | grep -qi "text/html"; then
    echo "PASS: /esi endpoint returns text/html (processed by mesi)"
else
    echo "INFO: /esi Content-Type: $CT"
fi

echo "=== Test 7: timeout 2s aborts 5s include ==="
TIME_TOTAL=$(curl -s --max-time 20 -H "Host: timeout2.domain.com" http://localhost:18080/slow/5000 -o /tmp/mesi-traefik-timeout-body.txt -w "%{time_total}")
if awk -v t="$TIME_TOTAL" 'BEGIN{exit !(t >= 1.5 && t <= 4.0)}'; then
    echo "PASS: include aborted at ${TIME_TOTAL}s (window [1.5, 4.0])"
else
    echo "FAIL: expected abort at ~2s, got ${TIME_TOTAL}s"
    docker compose down
    exit 1
fi
if grep -q "SLOW-FRAG" /tmp/mesi-traefik-timeout-body.txt; then
    echo "FAIL: 5s fragment delivered despite 2s timeout"
    echo "Response: $(cat /tmp/mesi-traefik-timeout-body.txt)"
    docker compose down
    exit 1
elif ! grep -q "SLOW-PAGE" /tmp/mesi-traefik-timeout-body.txt; then
    echo "FAIL: page marker missing (page itself did not render)"
    echo "Response: $(cat /tmp/mesi-traefik-timeout-body.txt)"
    docker compose down
    exit 1
elif grep -q "<esi:include" /tmp/mesi-traefik-timeout-body.txt; then
    echo "FAIL: raw <esi:include> tag left in response"
    echo "Response: $(cat /tmp/mesi-traefik-timeout-body.txt)"
    docker compose down
    exit 1
else
    echo "PASS: 5s fragment aborted at the 2s budget, tag stripped"
fi

echo "=== Test 8: timeout 30s lets 10s include through ==="
TIME_TOTAL=$(curl -s --max-time 40 -H "Host: timeout30.domain.com" http://localhost:18080/slow/10000 -o /tmp/mesi-traefik-timeout-body.txt -w "%{time_total}")
if awk -v t="$TIME_TOTAL" 'BEGIN{exit !(t >= 9.5 && t <= 25.0)}'; then
    echo "PASS: 10s backend ran to completion in ${TIME_TOTAL}s (window [9.5, 25.0])"
else
    echo "FAIL: expected the 10s include in window [9.5, 25.0], got ${TIME_TOTAL}s"
    docker compose down
    exit 1
fi
if grep -q "SLOW-FRAG Held 10000" /tmp/mesi-traefik-timeout-body.txt; then
    echo "PASS: fragment delivered under the 30s budget"
else
    echo "FAIL: fragment missing under the 30s budget"
    echo "Response: $(cat /tmp/mesi-traefik-timeout-body.txt)"
    docker compose down
    exit 1
fi

echo "=== Test 9: absent timeout keeps the 10s default (11s include aborted) ==="
TIME_TOTAL=$(curl -s --max-time 55 -H "Host: domain.com" http://localhost:18080/slow/11000 -o /tmp/mesi-traefik-timeout-body.txt -w "%{time_total}")
if awk -v t="$TIME_TOTAL" 'BEGIN{exit !(t >= 9.5 && t <= 14.0)}'; then
    echo "PASS: default budget aborted the 11s include at ${TIME_TOTAL}s (window [9.5, 14.0])"
else
    echo "FAIL: expected ~10s default budget, got ${TIME_TOTAL}s"
    docker compose down
    exit 1
fi
if grep -q "SLOW-FRAG" /tmp/mesi-traefik-timeout-body.txt; then
    echo "FAIL: 11s fragment delivered — default timeout is not 10s"
    echo "Response: $(cat /tmp/mesi-traefik-timeout-body.txt)"
    docker compose down
    exit 1
fi
if ! grep -q "SLOW-PAGE" /tmp/mesi-traefik-timeout-body.txt; then
    echo "FAIL: parent page marker missing — the render itself broke"
    echo "Response: $(cat /tmp/mesi-traefik-timeout-body.txt)"
    docker compose down
    exit 1
fi
if grep -q "<esi:include" /tmp/mesi-traefik-timeout-body.txt; then
    echo "FAIL: raw <esi:include> tag left in response"
    docker compose down
    exit 1
fi
echo "PASS: absent timeout stays byte-compatible with the historical 10s (page rendered, tag stripped)"
rm -f /tmp/mesi-traefik-timeout-body.txt

# --- maxResponseSize tests (#210) ---
# The backend (servers/test-server) serves /bytes/<size>, a body of
# exactly <size> bytes prefixed with a "MesiBytesPayload <size>" marker
# line, and /bytespage/<size>, a page including it. Assertions combine
# the marker (fragment arrived / was rejected) with wc -c (the whole
# body was delivered, not just the marker).

echo "=== Test 10: maxResponseSize 100 — 200-byte include rejected (#210) ==="
RESPONSE=$(curl -s --max-time 15 -H "Host: maxrs100.domain.com" http://localhost:18080/bytespage/200)
if echo "$RESPONSE" | grep -q "MesiBytesPayload" \
    || ! echo "$RESPONSE" | grep -q "BYTES-PAGE" \
    || ! echo "$RESPONSE" | grep -q "After bytes include" \
    || echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "FAIL: maxResponseSize 100 did not reject the 200-byte include fail-closed"
    echo "Response: $(echo "$RESPONSE" | head -c 500)"
    docker compose down
    exit 1
fi
echo "PASS: 200-byte include rejected by maxResponseSize 100 (marker absent, tag stripped — fail closed, not truncated)"
# Control: the SAME page served through the default middleware
# (maxResponseSize absent) must deliver the payload — proves the
# rejection above comes from the option, not a broken fixture.
CONTROL=$(curl -s --max-time 15 -H "Host: domain.com" http://localhost:18080/bytespage/200)
if echo "$CONTROL" | grep -q "MesiBytesPayload 200" \
    && echo "$CONTROL" | grep -q "After bytes include" \
    && ! echo "$CONTROL" | grep -q '<esi:include'; then
    echo "PASS: control — same page on the default middleware (option absent) delivers the 200-byte payload"
else
    echo "FAIL: control — default middleware did not deliver the payload"
    echo "Response: $(echo "$CONTROL" | head -c 500)"
    docker compose down
    exit 1
fi

echo "=== Test 11: maxResponseSize 1048576 — 500 KB include delivered (#210) ==="
curl -s --max-time 60 -o /tmp/mesi-traefik-maxrs-accept.txt -H "Host: maxrs1m.domain.com" http://localhost:18080/bytespage/500000
SIZE=$(wc -c < /tmp/mesi-traefik-maxrs-accept.txt | tr -d ' ')
if [ "$SIZE" -gt 500000 ] \
    && grep -q "MesiBytesPayload 500000" /tmp/mesi-traefik-maxrs-accept.txt \
    && grep -q "After bytes include" /tmp/mesi-traefik-maxrs-accept.txt \
    && ! grep -q '<esi:include' /tmp/mesi-traefik-maxrs-accept.txt; then
    echo "PASS: 500 KB include delivered in full under maxResponseSize 1048576 ($SIZE bytes)"
else
    echo "FAIL: maxResponseSize 1048576 did not deliver the 500 KB include (size $SIZE)"
    head -c 500 /tmp/mesi-traefik-maxrs-accept.txt
    rm -f /tmp/mesi-traefik-maxrs-accept.txt
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-traefik-maxrs-accept.txt

echo "=== Test 12: absent maxResponseSize — unlimited, 10 MB + 1 include delivered (#210) ==="
# The default middleware never sets the option → ServeHTTP's
# EsiParserConfig literal leaves MaxResponseSize at 0 (unlimited,
# mesi/fetch.go). The body is 10 MB + 1 byte: the issue's proposed
# implicit 10 MB default would reject it, so a passing test pins
# "absent → unlimited" byte-identical to pre-#210 behaviour.
curl -s --max-time 60 -o /tmp/mesi-traefik-maxrs-absent.txt -H "Host: domain.com" http://localhost:18080/bytespage/10485761
SIZE=$(wc -c < /tmp/mesi-traefik-maxrs-absent.txt | tr -d ' ')
if [ "$SIZE" -gt 10485761 ] \
    && grep -q "MesiBytesPayload 10485761" /tmp/mesi-traefik-maxrs-absent.txt \
    && grep -q "After bytes include" /tmp/mesi-traefik-maxrs-absent.txt \
    && ! grep -q '<esi:include' /tmp/mesi-traefik-maxrs-absent.txt; then
    echo "PASS: absent maxResponseSize stayed unlimited — 10 MB + 1 include delivered ($SIZE bytes)"
else
    echo "FAIL: absent maxResponseSize did not behave as unlimited (size $SIZE)"
    head -c 500 /tmp/mesi-traefik-maxrs-absent.txt
    rm -f /tmp/mesi-traefik-maxrs-absent.txt
    docker compose down
    exit 1
fi
rm -f /tmp/mesi-traefik-maxrs-absent.txt

docker compose down

echo ""
echo "=== All tests passed ==="
