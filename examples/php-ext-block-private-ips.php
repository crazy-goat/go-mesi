<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with SSRF
 * protection (block_private_ips).
 *
 * Run from CLI:
 *    php examples/php-ext-block-private-ips.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Notes:
 *   - block_private_ips defaults to true (secure by default): includes that
 *     resolve to private/reserved IP ranges (loopback, RFC1918, CGNAT,
 *     link-local, …) are blocked at dial time, preventing SSRF via DNS
 *     rebinding.
 *   - Pass false only when your origin legitimately lives on an internal
 *     address you trust.
 *   - A non-boolean value is rejected with E_WARNING and parse_with_config()
 *     returns false.
 */

declare(strict_types=1);

$input = <<<ESI
<esi:include src="http://internal-origin/esi"/>
ESI;

// Secure default — private IPs are blocked.
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://internal-origin/',
    [
        'block_private_ips' => true,
    ]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;
