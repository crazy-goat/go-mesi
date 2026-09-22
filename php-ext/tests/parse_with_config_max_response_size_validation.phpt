--TEST--
parse_with_config() max_response_size: option parsing and range validation
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// Strict validation contract (#201): an absent key keeps the documented
// default 0 = unlimited (byte-identical to previous behaviour — there is
// NO implicit 10 MB default on this path, see the #169 CHANGELOG entry),
// but every malformed EXPLICIT value must produce E_WARNING naming the
// option and make parse_with_config() return false — never a silent
// coercion. Accepted values route the call through libgomesi's ParseJson
// entry point; a warning here would also mean the symbol lookup failed,
// so warnings=0 doubles as proof the ParseJson path is live.
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});
function run(array $cfg) {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', $cfg);
    return [$r, array_slice($warnings, $before)];
}

// --- accepted boundary classes: 0 (unlimited), 1 (min cap), 1048576
//     (typical), 9223372036854775806 (MaxInt64-1, the documented cap) ---
list($r,$w)=run(['max_response_size'=>0]);
echo "zero_result=".($r===false?'false':'string')." zero_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>1]);
echo "min1_result=".($r===false?'false':'string')." min1_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>1048576]);
echo "mb10_result=".($r===false?'false':'string')." mb10_warnings=".count($w)." mb10_body=".($r===false?'':$r)."\n";
list($r,$w)=run(['max_response_size'=>9223372036854775806]);
echo "cap_result=".($r===false?'false':'string')." cap_warnings=".count($w)."\n";

// --- absent key: documented default 0 = unlimited, positional path, no warnings ---
list($r,$w)=run([]);
echo "absent_result=".($r===false?'false':'string')." absent_warnings=".count($w)." absent_body=".($r===false?'':$r)."\n";

// --- rejected: negatives and the MaxInt64 overflow cap ---
list($r,$w)=run(['max_response_size'=>-1]);
echo "neg1_result=".($r===false?'false':'string')." neg1_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>-100]);
echo "neg100_result=".($r===false?'false':'string')." neg100_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>9223372036854775807]); // MaxInt64: size+1 wraps
echo "maxint64_result=".($r===false?'false':'string')." maxint64_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>PHP_INT_MAX]);          // same value on 64-bit
echo "phpintmax_result=".($r===false?'false':'string')." phpintmax_warnings=".count($w)."\n";

// --- rejected: non-integer types (same strict contract as timeout/cache_ttl) ---
list($r,$w)=run(['max_response_size'=>'10485760']);  // numeric string must NOT coerce
echo "str_result=".($r===false?'false':'string')." str_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>'abc']);
echo "abc_result=".($r===false?'false':'string')." abc_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>'']);          // empty string
echo "emptystr_result=".($r===false?'false':'string')." emptystr_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>1.5]);         // float, fractional
echo "float_result=".($r===false?'false':'string')." float_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>10.0]);        // float that happens to be integral
echo "float10_result=".($r===false?'false':'string')." float10_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>99999999999999999999]); // overflow literal -> float
echo "overflow_result=".($r===false?'false':'string')." overflow_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>true]);
echo "bool_result=".($r===false?'false':'string')." bool_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>null]);
echo "null_result=".($r===false?'false':'string')." null_warnings=".count($w)."\n";
list($r,$w)=run(['max_response_size'=>[100]]);
echo "array_result=".($r===false?'false':'string')." array_warnings=".count($w)."\n";

// the warning text must name the option (fail-loud rule)
list($r,$w)=run(['max_response_size'=>-1]);
echo "warn_names_option=".(count($w)===1 && strpos($w[0], 'max_response_size') !== false ? 'yes':'no')."\n";
?>
--EXPECT--
zero_result=string zero_warnings=0
min1_result=string min1_warnings=0
mb10_result=string mb10_warnings=0 mb10_body=plain-ok
cap_result=string cap_warnings=0
absent_result=string absent_warnings=0 absent_body=plain-ok
neg1_result=false neg1_warnings=1
neg100_result=false neg100_warnings=1
maxint64_result=false maxint64_warnings=1
phpintmax_result=false phpintmax_warnings=1
str_result=false str_warnings=1
abc_result=false abc_warnings=1
emptystr_result=false emptystr_warnings=1
float_result=false float_warnings=1
float10_result=false float10_warnings=1
overflow_result=false overflow_warnings=1
bool_result=false bool_warnings=1
null_result=false null_warnings=1
array_result=false array_warnings=1
warn_names_option=yes
