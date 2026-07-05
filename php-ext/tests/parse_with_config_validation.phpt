--TEST--
parse_with_config() validates cache_backend, cache_size, cache_ttl strictly
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// capture warnings via a custom handler so E_WARNING doesn't fail the test
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) {
        $warnings[] = $errstr;
        return true;
    }
    return false;
});

// Use plain string input (no ESI marker tags) so the output string equals
// the input verbatim — easier to compare in --EXPECT--.
$BASIC_INPUT = 'plain-ok';

// Helper: run parse_with_config and return [result, warnings_at_call]
function run(array $cfg, $input = null) {
    global $warnings;
    if ($input === null) {
        $input = 'plain-ok';
    }
    $before = count($warnings);
    $r = @\mesi\parse_with_config(
        $input, 5, 'http://127.0.0.1/', $cfg
    );
    return [$r, array_slice($warnings, $before)];
}

// 1. empty / no key: no cache (warning-free)
list($r1, $w1) = run([]);
echo "empty_backend_result=" . ($r1 === false ? 'false' : 'string') . "\n";
echo "empty_backend_warnings=" . count($w1) . "\n";

// 2. cache_backend => 'memory': accepted, returns string
list($r2, $w2) = run(['cache_backend' => 'memory']);
echo "memory_backend_result=" . ($r2 === false ? 'false' : 'string') . "\n";
echo "memory_backend_warnings=" . count($w2) . "\n";

// 3. cache_backend => 'redis' is now accepted (track #231), but
//    requires cache_redis_addr. Passing it without an address fires
//    the same E_WARNING path as a missing-key config would.
list($r3, $w3) = run(['cache_backend' => 'redis']);
echo "redis_no_addr_result=" . ($r3 === false ? 'false' : 'string') . "\n";
echo "redis_no_addr_warnings=" . count($w3) . "\n";

// 4. cache_size out of range (> 1_000_000)
list($r4, $w4) = run(['cache_backend' => 'memory', 'cache_size' => 9999999]);
echo "oversize_cache_size_result=" . ($r4 === false ? 'false' : 'string') . "\n";
echo "oversize_cache_size_warnings=" . count($w4) . "\n";

// 5. cache_ttl negative
list($r5, $w5) = run(['cache_backend' => 'memory', 'cache_ttl' => -1]);
echo "negative_ttl_result=" . ($r5 === false ? 'false' : 'string') . "\n";
echo "negative_ttl_warnings=" . count($w5) . "\n";

// 6. cache_ttl too high (> 86_400)
list($r6, $w6) = run(['cache_backend' => 'memory', 'cache_ttl' => 86401]);
echo "oversize_ttl_result=" . ($r6 === false ? 'false' : 'string') . "\n";
echo "oversize_ttl_warnings=" . count($w6) . "\n";

// 7. cache_backend not a string (e.g. int)
list($r7, $w7) = run(['cache_backend' => 42]);
echo "backend_wrong_type_result=" . ($r7 === false ? 'false' : 'string') . "\n";
echo "backend_wrong_type_warnings=" . count($w7) . "\n";

// 8. cache_size not an integer (string)
list($r8, $w8) = run(['cache_size' => '500']);
echo "size_wrong_type_result=" . ($r8 === false ? 'false' : 'string') . "\n";
echo "size_wrong_type_warnings=" . count($w8) . "\n";

// 9. Full valid config: backend=memory, size=100, ttl=60, input=plain-ok → returns 'plain-ok'
list($r9, $w9) = run(['cache_backend' => 'memory', 'cache_size' => 100, 'cache_ttl' => 60]);
echo "valid_full_config_result=" . ($r9 === false ? 'false' : 'string') . "\n";
echo "valid_full_config_body=" . ($r9 === false ? '' : $r9) . "\n";
echo "valid_full_config_warnings=" . count($w9) . "\n";
?>
--EXPECT--
empty_backend_result=string
empty_backend_warnings=0
memory_backend_result=string
memory_backend_warnings=0
redis_no_addr_result=false
redis_no_addr_warnings=1
oversize_cache_size_result=false
oversize_cache_size_warnings=1
negative_ttl_result=false
negative_ttl_warnings=1
oversize_ttl_result=false
oversize_ttl_warnings=1
backend_wrong_type_result=false
backend_wrong_type_warnings=1
size_wrong_type_result=false
size_wrong_type_warnings=1
valid_full_config_result=string
valid_full_config_body=plain-ok
valid_full_config_warnings=0
