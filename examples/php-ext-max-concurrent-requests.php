<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * `max_concurrent_requests` option — the per-call cap on concurrent
 * <esi:include> fetches.
 *
 * Run from CLI:
 *    php examples/php-ext-max-concurrent-requests.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *   - servers/test-server reachable (`cd servers/test-server && make
 *     build && MESI_TEST_SERVER_PORT=80 ./test-server`)
 *
 * Notes:
 *   - Unit: CONCURRENT include fetches within ONE parse_with_config()
 *     call (per call, not global across PHP calls/requests).
 *   - Range [0, 999999999]; absent -> 0 -> UNLIMITED (backward
 *     compatible: the value every positional path leaves).
 *   - `0` is the documented "unlimited" value (accepted, not rejected).
 *   - Includes queued beyond the cap are still delivered — funneled,
 *     never dropped.
 *   - Malformed values (string "10", 1.5, -1, 1000000000, ...) are
 *     rejected with E_WARNING and the function returns false — never
 *     silently coerced.
 *   - Requires a libgomesi build exporting ParseJson (#167); older
 *     libgomesi.so keep working with a warning and unlimited fetches.
 */

declare(strict_types=1);

$input = <<<ESI
<header>
  <esi:include src="http://test-server/esi"/>
  <esi:include src="http://test-server/esi"/>
  <esi:include src="http://test-server/esi"/>
</header>
ESI;

// Fetch at most 3 includes at a time within this one parse:
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['max_concurrent_requests' => 3]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;

// Explicit 0 = the documented "unlimited" value:
$unlimited = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['max_concurrent_requests' => 0]
);
var_export($unlimited !== false);   // true
echo "\n";

// Strict validation — this prints a warning and yields false:
$bad = \mesi\parse_with_config($input, 5, 'http://test-server/', ['max_concurrent_requests' => -1]);
var_export($bad);            // false
echo "\n";
