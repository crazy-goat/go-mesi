<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * Memcached cache backend. Duplicate <esi:include> URLs are deduped
 * cross-worker via the consistent-hashing ring of the configured servers.
 *
 * Run from CLI:
 *    php examples/php-ext-cache-memcached.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`) with the new
 *     InitCacheWithConfig entry point.
 *   - PHP extension built and loaded (`php -m | grep mesi`).
 *   - At least one reachable Memcached daemon.
 *
 * Expected output: three identical "Hurray: Esi included!" responses,
 * with the upstream seeing only one TCP request regardless of worker.
 *
 * Notes:
 *   - The servers array is non-empty and required; an empty array
 *     produces an E_WARNING so misconfiguration never silently demotes
 *     to "no cache".
 *   - Init succeeds even when no Memcached daemon is reachable (the
 *     underlying gomemcache client is lazy); <esi:include> traffic
 *     falls through to the origin server until a daemon comes online.
 *   - Each entry must be a "host:port" string with port in [1, 65535]
 *     and no whitespace, control chars, " or \\ — same restriction as
 *     the Apache MesiCacheMemcachedServers validator.
 */

declare(strict_types=1);

$servers = ['10.0.0.1:11211', '10.0.0.2:11211'];

$input = <<<ESI
<header>
  <esi:include src="http://test-server/esi"/>
</header>
<main>
  <esi:include src="http://test-server/esi"/>
</main>
<footer>
  <esi:include src="http://test-server/esi"/>
</footer>
ESI;

$result = \mesi\parse_with_config(
    $input,
    5,                       // max_depth (recommended: 5)
    'http://test-server/',   // default URL for relative includes
    [
        'cache_backend'           => 'memcached',
        'cache_size'              => 1000,
        'cache_ttl'               => 120,
        'cache_memcached_servers' => $servers,
    ]
);

if ($result === false) {
    $err = error_get_last();
    fwrite(STDERR, "parse_with_config failed: " . ($err['message'] ?? '(no message)') . "\n");
    exit(1);
}

echo $result;
