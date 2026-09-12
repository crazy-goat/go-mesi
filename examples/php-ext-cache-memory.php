<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * in-memory cache backend to dedup repeated <esi:include> URLs within a
 * single PHP worker process.
 *
 * Run from CLI:
 *    php examples/php-ext-cache-memory.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`)
 *   - PHP extension built and loaded (`php -m | grep mesi`)
 *
 * Expected output: the same "Hurray: Esi included!" content appears three
 * times (one per <esi:include>) but the upstream sees a single TCP request
 * (logged by the test-server; not part of this script).
 *
 * Notes:
 *   - The cache is per-PHP-worker-process. To share between workers,
 *     track the upcoming Redis (`#231`) and Memcached (`#235`) backends.
 *   - The legacy \mesi\parse() entrypoint is unchanged. Switch to
 *     \mesi\parse_with_config() only when you want caching.
 */

declare(strict_types=1);

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
    5,                              // max_depth (recommended: 5)
    'http://test-server/',          // default URL for relative includes
    [
        'cache_backend' => 'memory',
        'cache_size'    => 1000,    // entries; default 10000
        'cache_ttl'     => 60,      // seconds; 0 = no expiry
    ]
);

if ($result === false) {
    fwrite(STDERR, "parse_with_config failed\n");
    exit(1);
}

echo $result;
