<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * `timeout` option — the global per-include ESI fetch budget in seconds.
 *
 * Run from CLI:
 *    php examples/php-ext-timeout.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Notes:
 *   - Range [1, 86400] seconds; absent key → 30 (libgomesi's historical
 *     default, backward compatible).
 *   - `0` is REJECTED (E_WARNING + false): it does not mean "no timeout" —
 *     the core fails every include immediately when Timeout <= 0.
 *   - Malformed values (string "10", 1.5, "abc", ...) are rejected with
 *     E_WARNING and the function returns false — never silently coerced.
 *   - Requires a libgomesi build exporting ParseJson (#167); older
 *     libgomesi.so keep working with a warning and the 30s default.
 */

declare(strict_types=1);

$input = <<<ESI
<header>
  <esi:include src="http://test-server/esi"/>
</header>
ESI;

// Abort includes that take longer than 10 seconds:
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['timeout' => 10]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;

// Strict validation — this prints a warning and yields false:
$bad = \mesi\parse_with_config($input, 5, 'http://test-server/', ['timeout' => 0]);
var_export($bad);            // false
echo "\n";
