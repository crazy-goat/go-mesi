<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * `max_workers` option — the per-call size cap of the include drain
 * pool (the goroutines that process ESI tokens and fetch includes).
 *
 * Run from CLI:
 *    php examples/php-ext-max-workers.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *   - servers/test-server reachable (`cd servers/test-server && make
 *     build && MESI_TEST_SERVER_PORT=80 ./test-server`)
 *
 * Notes:
 *   - Unit: drain-pool GOROUTINES within ONE parse_with_config() call
 *     (per call, not global across PHP calls/requests; each nested
 *     parse spawns its own pool and inherits the cap).
 *   - Range [0, 999999999]; absent -> 0 -> the LIBRARY DEFAULT
 *     runtime.NumCPU()*4 (backward compatible: the value every
 *     positional path leaves).
 *   - `0` is the documented "library default" value (accepted, not
 *     rejected) — with a current libgomesi.so it behaves exactly like
 *     an absent key.
 *   - Includes queued beyond the pool are still delivered — drained
 *     later, never dropped.
 *   - Malformed values (string "8", 1.5, -1, 1000000000, ...) are
 *     rejected with E_WARNING and the function returns false — never
 *     silently coerced (the core would substitute NumCPU*4 for a
 *     negative SILENTLY, so the PHP-side check is the only guard).
 *   - Requires a libgomesi build exporting ParseJson (#167); older
 *     libgomesi.so keep working with a warning and the library-default
 *     pool.
 */

declare(strict_types=1);

$input = <<<ESI
<header>
  <esi:include src="http://test-server/esi"/>
  <esi:include src="http://test-server/esi"/>
  <esi:include src="http://test-server/esi"/>
</header>
ESI;

// Drain this parse's ESI jobs with at most 2 goroutines:
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['max_workers' => 2]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;

// Explicit 0 = the documented "library default" (runtime.NumCPU()*4):
$defaultPool = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['max_workers' => 0]
);
var_export($defaultPool !== false);   // true
echo "\n";

// Strict validation — this prints a warning and yields false:
$bad = \mesi\parse_with_config($input, 5, 'http://test-server/', ['max_workers' => -1]);
var_export($bad);            // false
echo "\n";
