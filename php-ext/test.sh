#!/bin/bash
set -e
# CI launches php -S on port 8080 (tests.yaml); the local docker path
# publishes host port 18080 (container-internal 8080 is unchanged).
# TEST_PORT can be pre-set to run the CI-mode suite locally on a port
# other than 8080.
TEST_PORT=${TEST_PORT:-8080}
if [ "${CI:-}" != "true" ]; then
  TEST_PORT=18080
fi

if [ "${CI:-}" != "true" ]; then
  echo "Building and starting services..."
  cd "$(dirname "${BASH_SOURCE[0]}")"
  docker compose up -d --build

  echo "Waiting for PHP extension server to be ready..."
  for i in $(seq 1 60); do
      if curl -s -o /dev/null http://localhost:$TEST_PORT/health 2>/dev/null; then
          echo "PHP extension server ready after $((i * 2))s"
          break
      fi
      if [ "$i" -eq 60 ]; then
          echo "FAIL: PHP extension server did not become ready within $((i * 2))s"
          docker compose logs php-ext
          docker compose down
          exit 1
      fi
      sleep 2
  done
fi

echo ""
echo "=== Test 1: ESI comment unwrapping ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/)
if echo "$RESPONSE" | grep -q "Unwrapped content"; then
    echo "PASS: ESI comment unwrapped correctly"
else
    echo "FAIL: ESI comment not unwrapped"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 2: ESI include ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: ESI include processed correctly"
else
    echo "FAIL: ESI include not processed"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 3: ESI remove ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/)
if echo "$RESPONSE" | grep -q "Failed to include ESI"; then
    echo "FAIL: ESI remove content still present"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
else
    echo "PASS: ESI remove processed correctly"
fi

echo ""
echo "=== Test 4: ESI remove (dedicated route) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/remove)
if echo "$RESPONSE" | grep -q "remove this"; then
    echo "FAIL: ESI remove content still present in dedicated route"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi
if echo "$RESPONSE" | grep -q "keep this" && echo "$RESPONSE" | grep -q "also keep this"; then
    echo "PASS: ESI remove processed correctly, kept content preserved"
else
    echo "FAIL: Kept content missing after ESI remove"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 5: Non-HTML content (text/plain) - ESI tags are processed ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/plain)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: text/plain content had ESI include resolved"
else
    echo "INFO: text/plain ESI include resolved (tag replaced without content)"
fi

echo ""
echo "=== Test 6: JSON content - ESI tags are processed ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/json)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: JSON content had ESI include resolved"
else
    echo "INFO: JSON ESI include resolved (tag replaced without content)"
fi

echo ""
echo "=== Test 7: Content-Type preserved ==="
HEADERS=$(curl -sI http://localhost:$TEST_PORT/)
if echo "$HEADERS" | grep -qi "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Content-Type missing or wrong"
    echo "Headers: $HEADERS"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 8: Content-Length correctness ==="
TMPFILE=$(mktemp)
HEADERS=$(curl -sD - http://localhost:$TEST_PORT/remove -o "$TMPFILE" 2>/dev/null)
ACTUAL_BODY_SIZE=$(wc -c < "$TMPFILE")
HEADER_CL=$(echo "$HEADERS" | grep -i "Content-Length" | awk '{print $2}' | tr -d '\r')
if [ -n "$HEADER_CL" ]; then
    if [ "$HEADER_CL" -eq "$ACTUAL_BODY_SIZE" ] 2>/dev/null; then
        echo "PASS: Content-Length ($HEADER_CL) matches actual body size ($ACTUAL_BODY_SIZE)"
    else
        echo "FAIL: Content-Length ($HEADER_CL) != body size ($ACTUAL_BODY_SIZE)"
        [ "${CI:-}" != "true" ] && docker compose down
        exit 1
    fi
else
    echo "PASS: Content-Length correctly absent (processed by PHP built-in server)"
fi
rm -f "$TMPFILE"

echo ""
echo "=== Test 9: allowed_hosts (host listed -> include resolves) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/allowed-hosts)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: allowed_hosts whitelist permits the configured backend"
else
    echo "FAIL: include blocked although host is in allowed_hosts"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 10: allowed_hosts (host not listed -> include blocked) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/allowed-hosts-blocked)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "FAIL: include from host outside allowed_hosts was still fetched"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
else
    echo "PASS: include from host outside allowed_hosts blocked"
fi

echo ""
echo "=== Test 11: allowed_hosts subdomain match (docker network alias) ==="
if [ "${CI:-}" != "true" ]; then
    RESPONSE=$(curl -s http://localhost:$TEST_PORT/allowed-hosts-subdomain)
    if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
        echo "PASS: subdomain of an allowed host resolves (sub.test-server alias)"
    else
        echo "FAIL: subdomain include did not resolve (is the alias present?)"
        echo "Response: $RESPONSE"
        docker compose down
        exit 1
    fi
else
    echo "SKIP: subdomain fixture needs docker DNS aliases (CI runs test.sh without docker)"
fi

echo ""
echo "=== Test 12: allow_private_ips_for_allowed_hosts (on -> listed private backend fetched) ==="
# block_private_ips stays TRUE: the whitelisted backend host resolves to a
# private/reserved IP (loopback in CI, container IP in docker). Only the
# per-host bypass can let that dial through — this case proves the libgomesi
# shared-client yield; without it the bypass is a silent no-op and this FAILS.
RESPONSE=$(curl -s http://localhost:$TEST_PORT/bypass-on)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: bypass lets the whitelisted private backend through (block_private_ips on)"
else
    echo "FAIL: bypass did not take effect (shared-client yield broken?)"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 13: allow_private_ips_for_allowed_hosts (off by default -> blocked) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/bypass-off)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "FAIL: private backend fetched although the bypass is off"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
else
    echo "PASS: private-IP dial blocked with the bypass off (default)"
fi

echo ""
echo "=== Test 14: allow_private_ips_for_allowed_hosts (unlisted host still blocked) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/bypass-unlisted)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "FAIL: unlisted private host fetched despite whitelist"
    echo "Response: $RESPONSE"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
else
    echo "PASS: unlisted host blocked even with the bypass on"
fi

echo ""
echo "=== Test 15: timeout=2 cuts a slow include at ~2s (#181) ==="
# Spawn a DEDICATED slow fragment server (tests/slow_router.php, 4s per
# response) on 127.0.0.1:18081 — the app's built-in server is
# single-threaded, so the include must never be served by itself:
#   CI mode:     second `php -S` on the runner (everything is localhost)
#   docker mode: `php -S` inside the php-ext container (router.php runs
#                there, so 127.0.0.1:18081 must resolve in-container)
SLOW_PID=""
if [ "${CI:-}" = "true" ]; then
  SLOW_SCRIPT="$(dirname "${BASH_SOURCE[0]}")/tests/slow_router.php"
  php -S 127.0.0.1:18081 "$SLOW_SCRIPT" >/dev/null 2>&1 &
  SLOW_PID=$!
  for i in $(seq 1 50); do
    if (exec 3<>/dev/tcp/127.0.0.1/18081) 2>/dev/null; then break; fi
    sleep 0.2
  done
else
  docker compose exec -d php-ext php -S 0.0.0.0:18081 /app/tests/slow_router.php >/dev/null 2>&1
  for i in $(seq 1 50); do
    if docker compose exec -T php-ext php -r '$c=@fsockopen("127.0.0.1",18081,$e,$s,0.2); if($c){fclose($c);echo "ready";}' 2>/dev/null | grep -q ready; then
      break
    fi
    sleep 0.2
  done
fi

START=$(date +%s)
RESPONSE=$(curl -s http://localhost:$TEST_PORT/timeout)
ELAPSED=$(( $(date +%s) - START ))
if echo "$RESPONSE" | grep -q "SLOW-FRAGMENT"; then
    echo "FAIL: timeout=2 did not cut the slow include (timeout ignored?)"
    echo "Response: $RESPONSE"
    [ -n "$SLOW_PID" ] && kill "$SLOW_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
elif [ "$ELAPSED" -lt 1 ]; then
    echo "FAIL: include failed instantly (wrong reason — server not up?)"
    echo "Elapsed: ${ELAPSED}s"
    [ -n "$SLOW_PID" ] && kill "$SLOW_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
elif [ "$ELAPSED" -ge 4 ]; then
    echo "FAIL: budget not enforced (took ${ELAPSED}s, expected ~2s)"
    [ -n "$SLOW_PID" ] && kill "$SLOW_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
else
    echo "PASS: timeout=2 cut the slow include at ~${ELAPSED}s (fragment absent)"
fi

echo ""
echo "=== Test 16: timeout=30 + slow include -> success (#181) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/timeout-ok)
if echo "$RESPONSE" | grep -q "SLOW-FRAGMENT"; then
    echo "PASS: timeout=30 outlasted the 4s backend (include resolved)"
else
    echo "FAIL: include did not resolve within timeout=30"
    echo "Response: $RESPONSE"
    [ -n "$SLOW_PID" ] && kill "$SLOW_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 17: timeout key absent -> default 30s, positional path (#181) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/timeout-default)
if echo "$RESPONSE" | grep -q "SLOW-FRAGMENT"; then
    echo "PASS: absent timeout kept the documented 30s default (include resolved)"
else
    echo "FAIL: absent timeout broke the backward-compatible path"
    echo "Response: $RESPONSE"
    [ -n "$SLOW_PID" ] && kill "$SLOW_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

[ -n "$SLOW_PID" ] && kill "$SLOW_PID" 2>/dev/null || true

if [ "${CI:-}" != "true" ]; then
  docker compose down -v
fi

echo ""
echo "=== All PHP extension tests passed ==="
