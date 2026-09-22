<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * `max_response_size` option — the per-include ESI fetch body cap in bytes.
 *
 * Run from CLI:
 *    php examples/php-ext-max-response-size.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Notes:
 *   - Unit is BYTES; range [0, 9223372036854775806] (MaxInt64 - 1).
 *   - Absent key -> 0 -> UNLIMITED (backward compatible): there is NO
 *     implicit 10 MB default on this path (that value only exists in
 *     mesi.CreateDefaultConfig, which this extension never uses).
 *   - `0` is the documented "unlimited" value (accepted, not rejected).
 *   - Over-limit includes fail their fetch and render the empty
 *     IncludeErrorMarker / fallback body — per include, not per page.
 *   - Malformed values (string "10", 1.5, -1, MaxInt64, ...) are rejected
 *     with E_WARNING and the function returns false — never silently
 *     coerced.
 *   - Requires a libgomesi build exporting ParseJson (#167); older
 *     libgomesi.so keep working with a warning and unlimited size.
 */

declare(strict_types=1);

$input = <<<ESI
<header>
  <esi:include src="http://test-server/esi"/>
</header>
ESI;

// Reject includes whose body exceeds 1 MB:
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['max_response_size' => 1048576]
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
    ['max_response_size' => 0]
);
var_export($unlimited !== false);   // true
echo "\n";

// Strict validation — this prints a warning and yields false:
$bad = \mesi\parse_with_config($input, 5, 'http://test-server/', ['max_response_size' => -1]);
var_export($bad);            // false
echo "\n";
