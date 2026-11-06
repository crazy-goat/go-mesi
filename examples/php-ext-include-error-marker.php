<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * `include_error_marker` option — raw HTML shown in place of a failed
 * <esi:include>.
 *
 * Run from CLI:
 *    php examples/php-ext-include-error-marker.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *   - servers/test-server reachable (`cd servers/test-server && make
 *     build && MESI_TEST_SERVER_PORT=80 ./test-server`)
 *
 * Notes:
 *   - The marker is rendered only when an include FAILS and the tag has
 *     neither onerror="continue" nor a fallback body. Both always take
 *     priority.
 *   - An empty string is a valid "configured but silent" value; an absent
 *     key keeps the legacy positional path byte-identical (failed includes
 *     render nothing).
 *   - Only strings are accepted. Integers, floats, bools, null, arrays,
 *     objects and strings containing NUL bytes are rejected with E_WARNING
 *     naming `include_error_marker` and the function returns false.
 *   - Requires a libgomesi build exporting ParseJson (#167); an older
 *     libgomesi.so warns and stays silent.
 *   - SECURITY: the marker is inserted into the public response VERBATIM
 *     as raw HTML (it is not HTML-escaped). Never include internal error
 *     details, upstream URLs, host names or IP addresses — a generic
 *     placeholder is enough for users and cannot leak infrastructure.
 */

declare(strict_types=1);

$input = <<<ESI
<main>
  <esi:include src="http://test-server/esi"/>
  <esi:include src="http://test-server/does-not-exist"/>
</main>
ESI;

$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['include_error_marker' => '<!-- ESI include failed -->']
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;

// onerror="continue" and fallback bodies suppress the marker:
$suppressed = \mesi\parse_with_config(
    '<esi:include src="http://test-server/does-not-exist" onerror="continue" />'
    . '<esi:include src="http://test-server/does-not-exist">fallback</esi:include>',
    5,
    'http://test-server/',
    ['include_error_marker' => '<!-- ESI include failed -->']
);
var_export($suppressed !== false);   // true
echo "\n";
