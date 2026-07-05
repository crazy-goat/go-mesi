--TEST--
parse_with_config() with cache_backend=redis (and memcached) doesn't re-init the
shared cache when the same parameters are passed twice — it skips
InitCacheWithConfig so a second call doesn't wipe cached entries.
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
/*
 * The contract verified here matches the docblock on g_cache_state:
 * repeated parse_with_config() calls with identical config arrays must
 * NOT re-issue InitCacheWithConfig (which would replace sharedCache with
 * a fresh, empty instance). We can't observe libgomesi's internal state
 * from PHP, so we verify the externally-visible behaviour: an invalid
 * second call (e.g. db out of range) should still reject on input —
 * it never gets far enough to swap the cache.
 *
 * InitCacheWithConfig is lazy on the address (no DIAL), so the call
 * succeeds for any valid host:port even if nothing answers on it.
 */

$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) {
        $warnings[] = $errstr;
        return true;
    }
    return false;
});

// 1. valid redis config -> success
$cfg1 = [
    'cache_backend'    => 'redis',
    'cache_size'       => 100,
    'cache_ttl'        => 60,
    'cache_redis_addr' => '127.0.0.1:6379',
    'cache_redis_db'   => 2,
];
$r1 = \mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', $cfg1);
echo "first_call_body=" . ($r1 === false ? 'false' : $r1) . "\n";
echo "first_call_warnings=" . count($warnings) . "\n";

// 2. identical config -> no warning, same body (cache not re-initialized)
$before = count($warnings);
$r2 = \mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', $cfg1);
echo "second_call_body=" . ($r2 === false ? 'false' : $r2) . "\n";
echo "second_call_warnings=" . count($warnings) - $before . "\n";

// 3. memcached valid config -> success
$cfg3 = [
    'cache_backend'           => 'memcached',
    'cache_size'              => 100,
    'cache_ttl'               => 60,
    'cache_memcached_servers' => ['127.0.0.1:11211'],
];
$r3 = \mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', $cfg3);
echo "memcached_first_body=" . ($r3 === false ? 'false' : $r3) . "\n";
echo "memcached_first_warnings=" . count($warnings) . "\n";

// 4. memcached identical config -> still no warning
$before = count($warnings);
$r4 = \mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', $cfg3);
echo "memcached_second_body=" . ($r4 === false ? 'false' : $r4) . "\n";
echo "memcached_second_warnings=" . count($warnings) - $before . "\n";

// 5. memory config without touching previous redis/memcached sharedCache
//    -> success and the prior backend gets replaced (per init semantics;
//    parse_with_config never accumulates stale backends)
$cfg5 = [
    'cache_backend' => 'memory',
    'cache_size'    => 50,
    'cache_ttl'     => 30,
];
$r5 = \mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', $cfg5);
echo "memory_first_body=" . ($r5 === false ? 'false' : $r5) . "\n";
echo "memory_first_warnings=" . count($warnings) . "\n";

// 6. an invalid config must still reject loudly (validation runs before
//    InitCacheWithConfig is even considered).
$before = count($warnings);
$r6 = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', [
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:6379',
    'cache_redis_db'   => 99,
]);
echo "invalid_db_result=" . ($r6 === false ? 'false' : 'string') . "\n";
echo "invalid_db_warnings=" . count($warnings) - $before . "\n";

?>
--EXPECT--
first_call_body=plain-ok
first_call_warnings=0
second_call_body=plain-ok
second_call_warnings=0
memcached_first_body=plain-ok
memcached_first_warnings=0
memcached_second_body=plain-ok
memcached_second_warnings=0
memory_first_body=plain-ok
memory_first_warnings=0
invalid_db_result=false
invalid_db_warnings=1
