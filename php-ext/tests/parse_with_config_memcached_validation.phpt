--TEST--
parse_with_config() validates cache_backend=memcached and the servers list strictly
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) {
        $warnings[] = $errstr;
        return true;
    }
    return false;
});

function run(array $cfg) {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse_with_config(
        'plain-ok', 5, 'http://127.0.0.1/', $cfg
    );
    return [$r, array_slice($warnings, $before)];
}

// 1. memcached with one server: accepted (InitCacheWithConfig succeeds
//    even when no real memcached is running because the client is lazy).
list($r1, $w1) = run([
    'cache_backend'           => 'memcached',
    'cache_size'              => 100,
    'cache_ttl'               => 60,
    'cache_memcached_servers' => ['127.0.0.1:11211'],
]);
echo "single_server_result=" . ($r1 === false ? 'false' : 'string') . "\n";
echo "single_server_warnings=" . count($w1) . "\n";

// 2. memcached with two servers: accepted.
list($r2, $w2) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1:11211', '10.0.0.2:11211'],
]);
echo "two_servers_result=" . ($r2 === false ? 'false' : 'string') . "\n";
echo "two_servers_warnings=" . count($w2) . "\n";

// 3. memcached without servers key: rejected.
list($r3, $w3) = run([
    'cache_backend' => 'memcached',
    'cache_size'    => 100,
    'cache_ttl'     => 60,
]);
echo "missing_servers_result=" . ($r3 === false ? 'false' : 'string') . "\n";
echo "missing_servers_warnings=" . count($w3) . "\n";

// 4. memcached with empty servers list: rejected.
list($r4, $w4) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => [],
]);
echo "empty_servers_result=" . ($r4 === false ? 'false' : 'string') . "\n";
echo "empty_servers_warnings=" . count($w4) . "\n";

// 5. memcached with a non-array for servers: rejected.
list($r5, $w5) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => '127.0.0.1:11211',
]);
echo "non_array_servers_result=" . ($r5 === false ? 'false' : 'string') . "\n";
echo "non_array_servers_warnings=" . count($w5) . "\n";

// 6. memcached with a server entry that has no port: rejected.
list($r6, $w6) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1'],
]);
echo "srv_no_port_result=" . ($r6 === false ? 'false' : 'string') . "\n";
echo "srv_no_port_warnings=" . count($w6) . "\n";

// 7. memcached with port = 0: rejected.
list($r7, $w7) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1:0'],
]);
echo "srv_zero_port_result=" . ($r7 === false ? 'false' : 'string') . "\n";
echo "srv_zero_port_warnings=" . count($w7) . "\n";

// 8. memcached with port > 65535: rejected.
list($r8, $w8) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1:99999'],
]);
echo "srv_huge_port_result=" . ($r8 === false ? 'false' : 'string') . "\n";
echo "srv_huge_port_warnings=" . count($w8) . "\n";

// 9. memcached with whitespace in a server entry: rejected.
list($r9, $w9) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1 :11211'],
]);
echo "srv_with_space_result=" . ($r9 === false ? 'false' : 'string') . "\n";
echo "srv_with_space_warnings=" . count($w9) . "\n";

// 10. memcached with embedded quote in a server entry: rejected.
list($r10, $w10) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1:"11211"'],
]);
echo "srv_with_quote_result=" . ($r10 === false ? 'false' : 'string') . "\n";
echo "srv_with_quote_warnings=" . count($w10) . "\n";

// 11. memcached with one bad entry mixed with a good one: rejected (atomic).
list($r11, $w11) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1:11211', '10.0.0.2'],
]);
echo "mixed_good_bad_result=" . ($r11 === false ? 'false' : 'string') . "\n";
echo "mixed_good_bad_warnings=" . count($w11) . "\n";

// 12. memcached with a non-string entry: rejected.
list($r12, $w12) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => [11211],
]);
echo "srv_non_string_result=" . ($r12 === false ? 'false' : 'string') . "\n";
echo "srv_non_string_warnings=" . count($w12) . "\n";

// 13. memcached with backend=memory but servers supplied: rejected.
list($r13, $w13) = run([
    'cache_backend'           => 'memory',
    'cache_memcached_servers' => ['10.0.0.1:11211'],
]);
echo "servers_with_memory_result=" . ($r13 === false ? 'false' : 'string') . "\n";
echo "servers_with_memory_warnings=" . count($w13) . "\n";

// 14. accepted-max port (= 65535) for a memcached server entry: the
//     upper boundary of the [1, 65535] range must be admitted.
list($r14, $w14) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1:65535'],
]);
echo "srv_max_port_result=" . ($r14 === false ? 'false' : 'string') . "\n";
echo "srv_max_port_warnings=" . count($w14) . "\n";

// 15. accepted-min port (= 1) for a memcached server entry.
list($r15, $w15) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1:1'],
]);
echo "srv_min_port_result=" . ($r15 === false ? 'false' : 'string') . "\n";
echo "srv_min_port_warnings=" . count($w15) . "\n";

// 16. memcached with backend="" -> rejected (servers required).
list($r16, $w16) = run([
    'cache_memcached_servers' => ['10.0.0.1:11211'],
]);
echo "servers_with_empty_result=" . ($r16 === false ? 'false' : 'string') . "\n";
echo "servers_with_empty_warnings=" . count($w16) . "\n";

// 17. memcached with backend=redis but servers supplied -> rejected.
list($r17, $w17) = run([
    'cache_backend'           => 'redis',
    'cache_redis_addr'        => '127.0.0.1:6379',
    'cache_memcached_servers' => ['10.0.0.1:11211'],
]);
echo "servers_with_redis_result=" . ($r17 === false ? 'false' : 'string') . "\n";
echo "servers_with_redis_warnings=" . count($w17) . "\n";

// 18. memcached with a decimal port "3.14" -> rejected at validation.
list($r18, $w18) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['10.0.0.1:3.14'],
]);
echo "srv_decimal_port_result=" . ($r18 === false ? 'false' : 'string') . "\n";
echo "srv_decimal_port_warnings=" . count($w18) . "\n";
?>
--EXPECT--
single_server_result=string
single_server_warnings=0
two_servers_result=string
two_servers_warnings=0
missing_servers_result=false
missing_servers_warnings=1
empty_servers_result=false
empty_servers_warnings=1
non_array_servers_result=false
non_array_servers_warnings=1
srv_no_port_result=false
srv_no_port_warnings=1
srv_zero_port_result=false
srv_zero_port_warnings=1
srv_huge_port_result=false
srv_huge_port_warnings=1
srv_with_space_result=false
srv_with_space_warnings=1
srv_with_quote_result=false
srv_with_quote_warnings=1
mixed_good_bad_result=false
mixed_good_bad_warnings=1
srv_non_string_result=false
srv_non_string_warnings=1
servers_with_memory_result=false
servers_with_memory_warnings=1
srv_max_port_result=string
srv_max_port_warnings=0
srv_min_port_result=string
srv_min_port_warnings=0
servers_with_empty_result=false
servers_with_empty_warnings=1
servers_with_redis_result=false
servers_with_redis_warnings=1
srv_decimal_port_result=false
srv_decimal_port_warnings=1
