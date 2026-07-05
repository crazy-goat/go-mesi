--TEST--
parse_with_config() validates cache_backend=redis and Redis-specific options strictly
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// Capture E_WARNING so they don't fail the test.
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

// 1. cache_backend=redis with a valid host:port: accepted, returns the input.
list($r1, $w1) = run([
    'cache_backend'    => 'redis',
    'cache_size'       => 100,
    'cache_ttl'        => 60,
    'cache_redis_addr' => '127.0.0.1:6379',
]);
echo "valid_full_result=" . ($r1 === false ? 'false' : 'string') . "\n";
echo "valid_full_body=" . ($r1 === false ? '' : $r1) . "\n";
echo "valid_full_warnings=" . count($w1) . "\n";

// 2. cache_backend=redis missing cache_redis_addr -> rejected.
list($r2, $w2) = run([
    'cache_backend' => 'redis',
    'cache_size'    => 100,
    'cache_ttl'     => 60,
]);
echo "missing_addr_result=" . ($r2 === false ? 'false' : 'string') . "\n";
echo "missing_addr_warnings=" . count($w2) . "\n";

// 3. cache_redis_addr without host:port (just a hostname) -> rejected.
list($r3, $w3) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => 'localhost',
]);
echo "addr_no_port_result=" . ($r3 === false ? 'false' : 'string') . "\n";
echo "addr_no_port_warnings=" . count($w3) . "\n";

// 4. cache_redis_addr with port = 0 -> rejected.
list($r4, $w4) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:0',
]);
echo "addr_zero_port_result=" . ($r4 === false ? 'false' : 'string') . "\n";
echo "addr_zero_port_warnings=" . count($w4) . "\n";

// 5. cache_redis_addr with port > 65535 -> rejected.
list($r5, $w5) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:65536',
]);
echo "addr_huge_port_result=" . ($r5 === false ? 'false' : 'string') . "\n";
echo "addr_huge_port_warnings=" . count($w5) . "\n";

// 6. cache_redis_addr with whitespace -> rejected.
list($r6, $w6) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1 :6379',
]);
echo "addr_with_space_result=" . ($r6 === false ? 'false' : 'string') . "\n";
echo "addr_with_space_warnings=" . count($w6) . "\n";

// 7. cache_redis_addr with embedded quote -> rejected (would break JSON blob).
list($r7, $w7) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:"6379"',
]);
echo "addr_with_quote_result=" . ($r7 === false ? 'false' : 'string') . "\n";
echo "addr_with_quote_warnings=" . count($w7) . "\n";

// 8. cache_redis_password with control char -> rejected.
list($r8, $w8) = run([
    'cache_backend'        => 'redis',
    'cache_redis_addr'     => '127.0.0.1:6379',
    'cache_redis_password' => "hi\x01there",
]);
echo "pwd_control_char_result=" . ($r8 === false ? 'false' : 'string') . "\n";
echo "pwd_control_char_warnings=" . count($w8) . "\n";

// 9. cache_redis_password with embedded quote -> rejected.
list($r9, $w9) = run([
    'cache_backend'        => 'redis',
    'cache_redis_addr'     => '127.0.0.1:6379',
    'cache_redis_password' => 'a"b',
]);
echo "pwd_quote_result=" . ($r9 === false ? 'false' : 'string') . "\n";
echo "pwd_quote_warnings=" . count($w9) . "\n";

// 10. cache_redis_password with embedded backslash -> rejected.
list($r10, $w10) = run([
    'cache_backend'        => 'redis',
    'cache_redis_addr'     => '127.0.0.1:6379',
    'cache_redis_password' => 'a\\b',
]);
echo "pwd_backslash_result=" . ($r10 === false ? 'false' : 'string') . "\n";
echo "pwd_backslash_warnings=" . count($w10) . "\n";

// 11. cache_redis_password "normal" -> accepted.
list($r11, $w11) = run([
    'cache_backend'        => 'redis',
    'cache_redis_addr'     => '127.0.0.1:6379',
    'cache_redis_password' => 's3cret',
]);
echo "pwd_ok_result=" . ($r11 === false ? 'false' : 'string') . "\n";
echo "pwd_ok_warnings=" . count($w11) . "\n";

// 12. cache_redis_db = 0 -> accepted (boundary).
list($r12, $w12) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:6379',
    'cache_redis_db'   => 0,
]);
echo "db_zero_result=" . ($r12 === false ? 'false' : 'string') . "\n";
echo "db_zero_warnings=" . count($w12) . "\n";

// 13. cache_redis_db = 15 -> accepted (boundary).
list($r13, $w13) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:6379',
    'cache_redis_db'   => 15,
]);
echo "db_max_result=" . ($r13 === false ? 'false' : 'string') . "\n";
echo "db_max_warnings=" . count($w13) . "\n";

// 14. cache_redis_db = 16 -> rejected (out of range).
list($r14, $w14) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:6379',
    'cache_redis_db'   => 16,
]);
echo "db_overflow_result=" . ($r14 === false ? 'false' : 'string') . "\n";
echo "db_overflow_warnings=" . count($w14) . "\n";

// 15. cache_redis_db = -1 -> rejected.
list($r15, $w15) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:6379',
    'cache_redis_db'   => -1,
]);
echo "db_negative_result=" . ($r15 === false ? 'false' : 'string') . "\n";
echo "db_negative_warnings=" . count($w15) . "\n";

// 16. cache_redis_addr passed with backend=memory -> rejected (mismatched).
list($r16, $w16) = run([
    'cache_backend'    => 'memory',
    'cache_redis_addr' => '127.0.0.1:6379',
]);
echo "addr_with_memory_result=" . ($r16 === false ? 'false' : 'string') . "\n";
echo "addr_with_memory_warnings=" . count($w16) . "\n";

// 17. cache_redis_password wrong type (int) -> rejected.
list($r17, $w17) = run([
    'cache_backend'        => 'redis',
    'cache_redis_addr'     => '127.0.0.1:6379',
    'cache_redis_password' => 42,
]);
echo "pwd_wrong_type_result=" . ($r17 === false ? 'false' : 'string') . "\n";
echo "pwd_wrong_type_warnings=" . count($w17) . "\n";

// 18. cache_redis_addr integer (treated as int) -> rejected (type mismatch).
list($r18, $w18) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => 1234,
]);
echo "addr_wrong_type_result=" . ($r18 === false ? 'false' : 'string') . "\n";
echo "addr_wrong_type_warnings=" . count($w18) . "\n";

// 19. accepted-max port (= 65535): the upper boundary of the [1, 65535]
//     range must be admitted without warning. If this regresses (e.g. the
//     validator's "65535" check turns into "<65535"), the silent
//     substitution would be a workflow anti-rule violation.
list($r19, $w19) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:65535',
]);
echo "addr_max_port_result=" . ($r19 === false ? 'false' : 'string') . "\n";
echo "addr_max_port_warnings=" . count($w19) . "\n";

// 20. accepted-min port (= 1): the lower boundary is admitted.
list($r20, $w20) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:1',
]);
echo "addr_min_port_result=" . ($r20 === false ? 'false' : 'string') . "\n";
echo "addr_min_port_warnings=" . count($w20) . "\n";

// 21. cache_redis_addr with backend="" (no backend at all) -> rejected.
list($r21, $w21) = run([
    'cache_redis_addr' => '127.0.0.1:6379',
]);
echo "addr_with_empty_result=" . ($r21 === false ? 'false' : 'string') . "\n";
echo "addr_with_empty_warnings=" . count($w21) . "\n";

// 22. cache_redis_addr with backend=memcached -> rejected.
list($r22, $w22) = run([
    'cache_backend'    => 'memcached',
    'cache_memcached_servers' => ['127.0.0.1:11211'],
    'cache_redis_addr' => '127.0.0.1:6379',
]);
echo "addr_with_memcached_result=" . ($r22 === false ? 'false' : 'string') . "\n";
echo "addr_with_memcached_warnings=" . count($w22) . "\n";

// 23. cache_redis_password with backend=memory -> rejected.
list($r23, $w23) = run([
    'cache_backend'        => 'memory',
    'cache_redis_password' => 's3cret',
]);
echo "pwd_with_memory_result=" . ($r23 === false ? 'false' : 'string') . "\n";
echo "pwd_with_memory_warnings=" . count($w23) . "\n";

// 24. cache_redis_password with backend="" -> rejected.
list($r24, $w24) = run([
    'cache_redis_password' => 's3cret',
]);
echo "pwd_with_empty_result=" . ($r24 === false ? 'false' : 'string') . "\n";
echo "pwd_with_empty_warnings=" . count($w24) . "\n";

// 25. cache_redis_password with backend=memcached -> rejected.
list($r25, $w25) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['127.0.0.1:11211'],
    'cache_redis_password'    => 's3cret',
]);
echo "pwd_with_memcached_result=" . ($r25 === false ? 'false' : 'string') . "\n";
echo "pwd_with_memcached_warnings=" . count($w25) . "\n";

// 26. cache_redis_db with backend=memory -> rejected.
list($r26, $w26) = run([
    'cache_backend'    => 'memory',
    'cache_redis_db'   => 3,
]);
echo "db_with_memory_result=" . ($r26 === false ? 'false' : 'string') . "\n";
echo "db_with_memory_warnings=" . count($w26) . "\n";

// 27. cache_redis_db with backend="" -> rejected.
list($r27, $w27) = run([
    'cache_redis_db' => 3,
]);
echo "db_with_empty_result=" . ($r27 === false ? 'false' : 'string') . "\n";
echo "db_with_empty_warnings=" . count($w27) . "\n";

// 28. cache_redis_db with backend=memcached -> rejected.
list($r28, $w28) = run([
    'cache_backend'           => 'memcached',
    'cache_memcached_servers' => ['127.0.0.1:11211'],
    'cache_redis_db'          => 3,
]);
echo "db_with_memcached_result=" . ($r28 === false ? 'false' : 'string') . "\n";
echo "db_with_memcached_warnings=" . count($w28) . "\n";

// 29. cache_redis_addr with port spelled as a decimal string "3.14"
//     (i.e. the user passed the wrong type by mistake) -> rejected
//     because parse_host_port rejects mid-string non-digits.
list($r29, $w29) = run([
    'cache_backend'    => 'redis',
    'cache_redis_addr' => '127.0.0.1:3.14',
]);
echo "addr_decimal_port_result=" . ($r29 === false ? 'false' : 'string') . "\n";
echo "addr_decimal_port_warnings=" . count($w29) . "\n";
?>
--EXPECT--
valid_full_result=string
valid_full_body=plain-ok
valid_full_warnings=0
missing_addr_result=false
missing_addr_warnings=1
addr_no_port_result=false
addr_no_port_warnings=1
addr_zero_port_result=false
addr_zero_port_warnings=1
addr_huge_port_result=false
addr_huge_port_warnings=1
addr_with_space_result=false
addr_with_space_warnings=1
addr_with_quote_result=false
addr_with_quote_warnings=1
pwd_control_char_result=false
pwd_control_char_warnings=1
pwd_quote_result=false
pwd_quote_warnings=1
pwd_backslash_result=false
pwd_backslash_warnings=1
pwd_ok_result=string
pwd_ok_warnings=0
db_zero_result=string
db_zero_warnings=0
db_max_result=string
db_max_warnings=0
db_overflow_result=false
db_overflow_warnings=1
db_negative_result=false
db_negative_warnings=1
addr_with_memory_result=false
addr_with_memory_warnings=1
pwd_wrong_type_result=false
pwd_wrong_type_warnings=1
addr_wrong_type_result=false
addr_wrong_type_warnings=1
addr_max_port_result=string
addr_max_port_warnings=0
addr_min_port_result=string
addr_min_port_warnings=0
addr_with_empty_result=false
addr_with_empty_warnings=1
addr_with_memcached_result=false
addr_with_memcached_warnings=1
pwd_with_memory_result=false
pwd_with_memory_warnings=1
pwd_with_empty_result=false
pwd_with_empty_warnings=1
pwd_with_memcached_result=false
pwd_with_memcached_warnings=1
db_with_memory_result=false
db_with_memory_warnings=1
db_with_empty_result=false
db_with_empty_warnings=1
db_with_memcached_result=false
db_with_memcached_warnings=1
addr_decimal_port_result=false
addr_decimal_port_warnings=1
