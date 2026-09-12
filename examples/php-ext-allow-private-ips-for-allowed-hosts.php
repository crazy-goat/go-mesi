<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * per-host private-IP bypass (allow_private_ips_for_allowed_hosts).
 *
 * Run from CLI:
 *    php examples/php-ext-allow-private-ips-for-allowed-hosts.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make build`) — must export
 *     ParseWithConfigEx (shipped since #168)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Notes:
 *   - allow_private_ips_for_allowed_hosts lets the HOSTS LISTED IN
 *     allowed_hosts resolve to private/reserved IPs even when
 *     block_private_ips is on — everything else stays blocked.
 *   - Only effective when BOTH block_private_ips=true AND allowed_hosts
 *     is a non-empty whitelist; otherwise a no-op.
 *   - SECURITY: this trusts DNS. An attacker able to influence what an
 *     allowed_hosts entry resolves to can reach internal/private
 *     addresses. Use only with internal DNS (Consul, Kubernetes DNS,
 *     /etc/hosts).
 *   - A non-boolean value is rejected with E_WARNING and
 *     parse_with_config() returns false.
 */

declare(strict_types=1);

$input = <<<ESI
<esi:include src="http://backend.internal/esi"/>
<esi:include src="http://public-cdn.example.com/assets/1.js"/>
ESI;

// backend.internal is on a private IP and IS whitelisted -> fetched with
// the bypass. public-cdn.example.com is whitelisted but public -> also
// fetched. Anything NOT in allowed_hosts (e.g. an attacker-controlled
// host inside a customer-supplied template) is rejected by hostname before
// any dial.
$result = \mesi\parse_with_config(
    $input,
    5,
    'http://edge.example.com/',
    [
        'allowed_hosts'                       => 'backend.internal public-cdn.example.com',
        'block_private_ips'                   => true,
        'allow_private_ips_for_allowed_hosts' => true,
    ]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;
