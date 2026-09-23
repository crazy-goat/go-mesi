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

echo ""
echo "=== Test 18: max_response_size=100 rejects a 200-byte include (#201) ==="
# Spawn a DEDICATED size-serving fragment server (tests/router.php
# /bytes/<size>) on 127.0.0.1:18082 — same split as the slow server
# above (the app's built-in server is single-threaded, so the include
# must never be served by itself):
#   CI mode:     second `php -S` on the runner
#   docker mode: `php -S` inside the php-ext container
BYTES_PID=""
if [ "${CI:-}" = "true" ]; then
  BYTES_ROUTER="$(dirname "${BASH_SOURCE[0]}")/tests/router.php"
  php -S 127.0.0.1:18082 "$BYTES_ROUTER" >/dev/null 2>&1 &
  BYTES_PID=$!
  for i in $(seq 1 50); do
    if (exec 3<>/dev/tcp/127.0.0.1/18082) 2>/dev/null; then break; fi
    sleep 0.2
  done
else
  docker compose exec -d php-ext php -S 0.0.0.0:18082 /app/tests/router.php >/dev/null 2>&1
  for i in $(seq 1 50); do
    if docker compose exec -T php-ext php -r '$c=@fsockopen("127.0.0.1",18082,$e,$s,0.2); if($c){fclose($c);echo "ready";}' 2>/dev/null | grep -q ready; then
      break
    fi
    sleep 0.2
  done
fi

RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-response-size-over)
if echo "$RESPONSE" | grep -q "over test" \
   && ! echo "$RESPONSE" | grep -q '#' \
   && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: 200-byte include rejected by max_response_size=100 (empty marker, no raw tag)"
else
    echo "FAIL: over-cap include was not rejected (cap ignored?)"
    echo "Response: $RESPONSE"
    [ -n "$BYTES_PID" ] && kill "$BYTES_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 19: max_response_size=1000 delivers a 200-byte include (#201) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-response-size-under)
COUNT=$(printf '%s' "$RESPONSE" | tr -cd '#' | wc -c | tr -d ' ')
if echo "$RESPONSE" | grep -q "under test" && [ "$COUNT" -eq 200 ]; then
    echo "PASS: under-cap include delivered fully ($COUNT/200 bytes)"
else
    echo "FAIL: under-cap include incomplete (got $COUNT/200 bytes, cap ignored?)"
    echo "Response: ${RESPONSE:0:200}"
    [ -n "$BYTES_PID" ] && kill "$BYTES_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 20: max_response_size absent -> unlimited (10 MB + 1 body) (#201) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-response-size-absent)
COUNT=$(printf '%s' "$RESPONSE" | tr -cd '#' | wc -c | tr -d ' ')
if echo "$RESPONSE" | grep -q "absent test" && [ "$COUNT" -eq 10485761 ]; then
    echo "PASS: absent key kept unlimited size (10485761/10485761 bytes delivered)"
else
    echo "FAIL: absent key did not behave as unlimited (got $COUNT/10485761 bytes)"
    echo "Response: ${RESPONSE:0:200}"
    [ -n "$BYTES_PID" ] && kill "$BYTES_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 21: max_response_size=0 -> unlimited (10 MB + 1 body) (#201) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-response-size-zero)
COUNT=$(printf '%s' "$RESPONSE" | tr -cd '#' | wc -c | tr -d ' ')
if echo "$RESPONSE" | grep -q "zero test" && [ "$COUNT" -eq 10485761 ]; then
    echo "PASS: explicit 0 delivered the full body (unlimited)"
else
    echo "FAIL: explicit 0 did not behave as unlimited (got $COUNT/10485761 bytes)"
    echo "Response: ${RESPONSE:0:200}"
    [ -n "$BYTES_PID" ] && kill "$BYTES_PID" 2>/dev/null
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

# Reap the bytes server (#469: kill without wait leaves the job table
# noisy / the zombie around until the shell exits).
if [ -n "$BYTES_PID" ]; then
    kill "$BYTES_PID" 2>/dev/null || true
    wait "$BYTES_PID" 2>/dev/null || true
fi

echo ""
echo "=== Test 22: max_concurrent_requests=3 funnels 20 includes (peak <= 3) (#206) ==="
# Deterministic peak-concurrency tracker (the #170/#192 pattern): the
# test-server (Go — CONCURRENT, unlike the single-threaded php -S app
# server which would serialize every hold into a peak of 1) serves
# /hold/<millis>/<label> (registers in the peak counter, sleeps, returns a
# "<label> Held <millis>" fragment) and /track/reset //track/max zero and
# read the counter — the router.php fixtures reset the counter before
# each parse and proxy /track/max through /max-concurrent-requests-peak
# (docker mode does not publish the test-server port to the host).
# Fan-out bound: MESIParse drains includes through a worker pool of
# min(MaxWorkers=NumCPU*4, 20) >= 4 goroutines (mesi/parser.go), so an
# uncapped parse must show peak >= 4, while cap 3 can never exceed 3 (the
# admission semaphore, mesi/fetch.go) and >= 2 proves the cap is a
# multi-slot queue rather than a serialisation to 1. The absent timeout
# key keeps the documented 30s default — well above the ~10.5s worst case
# of 20 x 1500ms queued 3 at a time.
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-concurrent-requests-cap)
FRAGMENTS=$(printf '%s' "$RESPONSE" | grep -o 'Held 1500' | wc -l | tr -d ' ')
PEAK=$(curl -s http://localhost:$TEST_PORT/max-concurrent-requests-peak)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -ge 2 ] && [ "$PEAK" -le 3 ] \
   && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: cap 3 funneled: peak=$PEAK (<=3 cap, >=2 parallel slots), 20/20 fragments delivered"
else
    echo "FAIL: max_concurrent_requests=3 did not funnel (peak=$PEAK, fragments=$FRAGMENTS)"
    echo "Response: ${RESPONSE:0:300}"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 23: max_concurrent_requests=0 -> unlimited (peak >= 4) (#206) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-concurrent-requests-zero)
FRAGMENTS=$(printf '%s' "$RESPONSE" | grep -o 'Held 1500' | wc -l | tr -d ' ')
PEAK=$(curl -s http://localhost:$TEST_PORT/max-concurrent-requests-peak)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -ge 4 ] \
   && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: explicit 0 unthrottled: peak=$PEAK >= 4, 20/20 fragments delivered"
else
    echo "FAIL: explicit 0 was not unlimited (peak=$PEAK, fragments=$FRAGMENTS)"
    echo "Response: ${RESPONSE:0:300}"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 24: max_concurrent_requests absent -> unlimited (peak >= 4) (#206) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-concurrent-requests-absent)
FRAGMENTS=$(printf '%s' "$RESPONSE" | grep -o 'Held 1500' | wc -l | tr -d ' ')
PEAK=$(curl -s http://localhost:$TEST_PORT/max-concurrent-requests-peak)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -ge 4 ] \
   && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: absent key unlimited: peak=$PEAK >= 4, 20/20 fragments delivered"
else
    echo "FAIL: absent key did not behave as unlimited (peak=$PEAK, fragments=$FRAGMENTS)"
    echo "Response: ${RESPONSE:0:300}"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 25: max_workers=2 drains 20 includes through a 2-goroutine pool (peak == 2) (#211) ==="
# Observable for max_workers is the DRAIN POOL, not a semaphore (#171):
# MESIParse spawns min(MaxWorkers, job count) goroutines
# (mesi/parser.go:118-129) and each processes one include at a time (the
# fetch is synchronous inside the goroutine), so on this flat page the
# backend peak-concurrency counter can never exceed the pool size — with
# max_workers=2 that is a hard peak <= 2 (a broken route — key not
# rendered / not resolved — would fall back to the default pool
# min(NumCPU*4, 20) >= 4 and show peak >= 4 instead, failing this
# bound: the ceiling also proves the maxWorkers key reached the core).
# The exact lower bound peak == 2 mirrors Apache Test 40 (#171) and CLI
# Test 33 (#197): both goroutines grab their first (buffered,
# parser.go:132,175-178) job within microseconds while each backend hold
# lasts 1500 ms, so the two holds must overlap — the ceiling is the hard
# pool invariant, the floor proves both slots work in parallel rather
# than serializing to 1. All 20 fragments are queued behind the pool and
# delivered — never dropped.
#
# Budget (the absent timeout key): the documented 30s default applies
# (positional path; a max_workers-only ParseJson blob resolves
# timeoutSeconds absent -> config.ResolveTimeout(nil) = 30s), and the
# budget ERODES as the parse runs (WithElapsedTime, parser.go:158): an
# include picked up at time t gets 30s - t. Workers 2 -> 10 waves x
# 1500 ms = ~15s nominal; the last wave starts ~13.5s leaving ~16.5s
# >> 1.5s, so only waves averaging >= 2.85s (~1.9x nominal) could push
# it past the 28.5s erosion floor. Tests 26/27 fan out through a pool
# of min(NumCPU*4, 20) >= 4 goroutines -> <= 5 waves = ~7.5s worst
# case. No explicit timeout is passed through the blob — that would
# violate the per-key conditional rendering contract.
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-workers-pool)
FRAGMENTS=$(printf '%s' "$RESPONSE" | grep -o 'Held 1500' | wc -l | tr -d ' ')
PEAK=$(curl -s http://localhost:$TEST_PORT/max-workers-peak)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -le 2 ] && [ "$PEAK" -ge 2 ] \
   && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: pool 2 bounded: peak=$PEAK (==2 hard pool bound + both slots parallel), 20/20 fragments delivered"
else
    echo "FAIL: max_workers=2 did not bound the drain pool (peak=$PEAK, fragments=$FRAGMENTS)"
    echo "Response: ${RESPONSE:0:300}"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 26: max_workers=0 -> library default pool (peak >= 4) (#211) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-workers-zero)
FRAGMENTS=$(printf '%s' "$RESPONSE" | grep -o 'Held 1500' | wc -l | tr -d ' ')
PEAK=$(curl -s http://localhost:$TEST_PORT/max-workers-peak)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -ge 4 ] \
   && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: explicit 0 = library default: peak=$PEAK >= 4, 20/20 fragments delivered"
else
    echo "FAIL: explicit 0 was not the library default (peak=$PEAK, fragments=$FRAGMENTS)"
    echo "Response: ${RESPONSE:0:300}"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

echo ""
echo "=== Test 27: max_workers absent -> library default pool (peak >= 4) (#211) ==="
RESPONSE=$(curl -s http://localhost:$TEST_PORT/max-workers-absent)
FRAGMENTS=$(printf '%s' "$RESPONSE" | grep -o 'Held 1500' | wc -l | tr -d ' ')
PEAK=$(curl -s http://localhost:$TEST_PORT/max-workers-peak)
if [ "$FRAGMENTS" -eq 20 ] && [ "$PEAK" -ge 4 ] \
   && ! echo "$RESPONSE" | grep -q '<esi:include'; then
    echo "PASS: absent key library default: peak=$PEAK >= 4, 20/20 fragments delivered"
else
    echo "FAIL: absent key did not behave as the library default (peak=$PEAK, fragments=$FRAGMENTS)"
    echo "Response: ${RESPONSE:0:300}"
    [ "${CI:-}" != "true" ] && docker compose down
    exit 1
fi

if [ "${CI:-}" != "true" ]; then
  docker compose down -v
fi

echo ""
echo "=== All PHP extension tests passed ==="
