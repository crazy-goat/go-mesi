<?php
/**
 * Example: use the PHP mESI extension's parse_with_config() with the
 * Redis cache backend so duplicate <esi:include> URLs are deduped across
 * PHP-FPM workers (and across hosts that point at the same Redis).
 *
 * Run from CLI:
 *    php examples/php-ext-cache-redis.php
 *
 * Requires:
 *   - libgomesi built (`cd libgomesi && make`) with the new
 *     InitCacheWithConfig entry point.
 *   - PHP extension built and loaded (`php -m | grep mesi`).
 *   - A reachable Redis server (the example targets 127.0.0.1:6379 —
 *     override via the $addr / $password / $db variables below).
 *
 * Expected output: the same "Hurray: Esi included!" content appears three
 * times (one per <esi:include>) but the upstream sees a single TCP request
 * regardless of which PHP-FPM worker handles the call.
 *
 * Notes:
 *   - The Redis cache is shared across PHP workers and across hosts.
 *     For a per-worker-process cache that does not require a Redis
 *     daemon, use the memory backend (see examples/php-ext-cache-memory.php).
 *   - For the Memcached variant, see examples/php-ext-cache-memcached.php.
 *   - Validation is strict: a non-`host:port` redis address, an out-of-range
 *     port, a non-integer db, or a password containing control chars / " / \
 *     produces an E_WARNING and parse_with_config() returns false. The
 *     misconfigured backend never silently demotes to "no cache".
 *   - Init succeeds even when no Redis is reachable (the underlying go-redis
 *     client is lazy), so this example can run without a live Redis for the
 *     parse-only path. Cache hits won't be observed until a real Redis is
 *     present, and <esi:include> traffic will fall through to the origin.
 */

declare(strict_types=1);

$addr     = '127.0.0.1:6379';
$password = '';     // optional
$db       = 0;      // 0..15

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
        'cache_backend'        => 'redis',
        'cache_size'           => 1000,        // entries
        'cache_ttl'            => 60,          // seconds
        'cache_redis_addr'     => $addr,
        'cache_redis_password' => $password,
        'cache_redis_db'       => $db,
    ]
);

if ($result === false) {
    // Surface the last E_WARNING so operators see what went wrong.
    $err = error_get_last();
    fwrite(STDERR, "parse_with_config failed: " . ($err['message'] ?? '(no message)') . "\n");
    exit(1);
}

echo $result;
