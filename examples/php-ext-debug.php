<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * `debug` option — the core parser's verbose diagnostics.
 *
 * Run from CLI:
 *    php examples/php-ext-debug.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *   - servers/test-server reachable (`cd servers/test-server && make
 *     build && MESI_TEST_SERVER_PORT=80 ./test-server`)
 *
 * Notes:
 *   - `debug => true` enables mesi.EsiParserConfig.Debug. The core's
 *     default logger writes DEBUG parse/include/fetch events to STDERR
 *     (captured by PHP's error log / CLI stderr); output is never sent
 *     to the client.
 *   - Absent or `false` keeps the parser silent. Absent also keeps the
 *     legacy positional path byte-identical.
 *   - Only true/false are accepted. Integer 0/1 (unlike
 *     block_private_ips), strings ("true", "1", ""), floats, null,
 *     arrays and objects are rejected with E_WARNING naming `debug`
 *     and the function returns false — never silently coerced.
 *   - Requires a libgomesi build exporting ParseJson (#167); an older
 *     libgomesi.so warns and keeps debug off.
 *   - SECURITY: each debug line includes the include URL, so enable
 *     this only while diagnosing and turn it off for production
 *     traffic when URLs may carry tokens or other secrets.
 */

declare(strict_types=1);

$input = <<<ESI
<header>
  <esi:include src="http://test-server/esi"/>
</header>
ESI;

// Enable core parser diagnostics (written to stderr):
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['debug' => true]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;

// Explicit false (or an absent key) stays silent:
$quiet = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['debug' => false]
);
var_export($quiet !== false);   // true
echo "\n";

// Strict validation — this prints a warning and yields false:
$bad = \mesi\parse_with_config($input, 5, 'http://test-server/', ['debug' => 1]);
var_export($bad);               // false
echo "\n";
