<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * `max_body_size` and `on_oversize` options — the parent (input) body cap
 * for ESI processing.
 *
 * Run from CLI:
 *    php examples/php-ext-max-body-size.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Notes:
 *   - Unit is BYTES. Absent or `0` means unlimited (backward compatible).
 *   - PHP has already allocated the body as a string, so the limit cannot
 *     reduce PHP-side memory; it avoids ESI parsing of huge bodies and
 *     gives the caller a deterministic outcome.
 *   - `pass` (default) returns an oversized body unchanged without ESI
 *     processing and emits an E_WARNING.
 *   - `error` emits an E_WARNING (no internal details) and returns false.
 *   - The boundary is inclusive: a body of exactly max_body_size bytes is
 *     processed; only a longer body is oversized.
 *   - These are input-handling options in the extension, not parser
 *     config: no ParseJson key, so the positional path is unchanged.
 */

declare(strict_types=1);

$input = <<<ESI
<header>
  <esi:include src="http://test-server/esi"/>
</header>
ESI;

// Pass pages bigger than 1 MiB through unchanged (no ESI processing):
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['max_body_size' => 1048576, 'on_oversize' => 'pass']
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;

// Fail deterministically instead of passing the body through:
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://test-server/',
    ['max_body_size' => 1048576, 'on_oversize' => 'error']
);
var_export($result !== false);   // true when the body is within the limit

// Strict validation — this prints a warning and yields false:
$bad = \mesi\parse_with_config($input, 5, 'http://test-server/', ['max_body_size' => -1]);
var_export($bad);            // false
echo "\n";
