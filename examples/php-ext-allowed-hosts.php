<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with a host
 * whitelist (allowed_hosts).
 *
 * Run from CLI:
 *    php examples/php-ext-allowed-hosts.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Notes:
 *   - allowed_hosts is a space-separated hostname whitelist: only
 *     <esi:include> destinations whose host equals an entry or is a
 *     subdomain of one are fetched. Anything else is rejected before a
 *     connection is made.
 *   - Empty or absent allowed_hosts = all hosts allowed (backward
 *     compatible).
 *   - allowed_hosts does NOT bypass block_private_ips: if the whitelisted
 *     backend lives on a private/reserved IP, pass block_private_ips=false.
 *   - A non-string or whitespace-only value is rejected with E_WARNING and
 *     parse_with_config() returns false.
 */

declare(strict_types=1);

$input = <<<ESI
<esi:include src="http://backend.internal/esi"/>
<esi:include src="http://public-cdn.example.com/assets/1.js"/>
ESI;

// Only backend.internal and public-cdn.example.com are fetched — anything
// else (e.g. an attacker-controlled host inside a customer-supplied
// template) is rejected by hostname before any dial.
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://edge.example.com/',
    [
        'allowed_hosts' => 'backend.internal public-cdn.example.com',
        // backend.internal resolves to a private IP, so the dial-time
        // private-IP block must be off for it (trusted internal DNS):
        'block_private_ips' => false,
    ]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;
