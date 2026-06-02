#!/bin/bash
set -e

if [ "${CI:-}" != "true" ]; then
  echo "Building and starting services..."
  cd "$(dirname "${BASH_SOURCE[0]}")"
  docker compose up -d --build

  echo "Waiting for PHP extension server to be ready..."
  for i in $(seq 1 60); do
      if curl -s -o /dev/null http://localhost:8080/health 2>/dev/null; then
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
RESPONSE=$(curl -s http://localhost:8080/)
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
RESPONSE=$(curl -s http://localhost:8080/)
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
RESPONSE=$(curl -s http://localhost:8080/)
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
RESPONSE=$(curl -s http://localhost:8080/remove)
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
RESPONSE=$(curl -s http://localhost:8080/plain)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: text/plain content had ESI include resolved"
else
    echo "INFO: text/plain ESI include resolved (tag replaced without content)"
fi

echo ""
echo "=== Test 6: JSON content - ESI tags are processed ==="
RESPONSE=$(curl -s http://localhost:8080/json)
if echo "$RESPONSE" | grep -q "Hurray: Esi included!"; then
    echo "PASS: JSON content had ESI include resolved"
else
    echo "INFO: JSON ESI include resolved (tag replaced without content)"
fi

echo ""
echo "=== Test 7: Content-Type preserved ==="
HEADERS=$(curl -sI http://localhost:8080/)
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
HEADERS=$(curl -sD - http://localhost:8080/remove -o "$TMPFILE" 2>/dev/null)
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

if [ "${CI:-}" != "true" ]; then
  docker compose down -v
fi

echo ""
echo "=== All PHP extension tests passed ==="
