--TEST--
parse_with_config() timeout: option parsing and range validation
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// Strict validation contract (#181): an absent key keeps the documented
// default 30s, but every malformed EXPLICIT value must produce E_WARNING
// naming the option and make parse_with_config() return false — never a
// silent coercion. Accepted values route the call through libgomesi's
// ParseJson entry point; a warning here would also mean the symbol lookup
// failed, so warnings=0 doubles as proof the ParseJson path is live.
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

// --- accepted boundary classes: 1 (min), 10 (normal), 86400 (cap) ---
list($r,$w)=run(['timeout'=>1]);
echo "min1_result=".($r===false?'false':'string')." min1_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>10]);
echo "t10_result=".($r===false?'false':'string')." t10_warnings=".count($w)." t10_body=".($r===false?'':$r)."\n";
list($r,$w)=run(['timeout'=>86400]);
echo "max86400_result=".($r===false?'false':'string')." max86400_warnings=".count($w)."\n";

// explicit 30 == the documented default, passed through ParseJson
list($r,$w)=run(['timeout'=>30]);
echo "explicit30_result=".($r===false?'false':'string')." explicit30_warnings=".count($w)."\n";

// --- absent key: documented default, positional path, no warnings ---
list($r,$w)=run([]);
echo "absent_result=".($r===false?'false':'string')." absent_warnings=".count($w)." absent_body=".($r===false?'':$r)."\n";

// --- rejected: 0 (NOT "no timeout"), negatives, above cap ---
list($r,$w)=run(['timeout'=>0]);
echo "zero_result=".($r===false?'false':'string')." zero_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>-1]);
echo "neg1_result=".($r===false?'false':'string')." neg1_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>86401]);
echo "over_result=".($r===false?'false':'string')." over_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>PHP_INT_MAX]);
echo "phpintmax_result=".($r===false?'false':'string')." phpintmax_warnings=".count($w)."\n";

// --- rejected: non-integer types (same strict contract as cache_ttl) ---
list($r,$w)=run(['timeout'=>'abc']);
echo "abc_result=".($r===false?'false':'string')." abc_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>'10']);   // numeric string must NOT coerce
echo "str10_result=".($r===false?'false':'string')." str10_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>'3foo']);
echo "str3foo_result=".($r===false?'false':'string')." str3foo_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>'']);     // empty string
echo "emptystr_result=".($r===false?'false':'string')." emptystr_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>1.5]);    // float, whole or not
echo "float_result=".($r===false?'false':'string')." float_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>10.0]);   // float that happens to be integral
echo "float10_result=".($r===false?'false':'string')." float10_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>99999999999999999999]); // overflow literal -> float
echo "overflow_result=".($r===false?'false':'string')." overflow_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>true]);
echo "bool_result=".($r===false?'false':'string')." bool_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>null]);
echo "null_result=".($r===false?'false':'string')." null_warnings=".count($w)."\n";
list($r,$w)=run(['timeout'=>[10]]);
echo "array_result=".($r===false?'false':'string')." array_warnings=".count($w)."\n";

// the warning text must name the option (fail-loud rule)
list($r,$w)=run(['timeout'=>0]);
echo "warn_names_option=".(count($w)===1 && strpos($w[0], 'timeout') !== false ? 'yes':'no')."\n";
?>
--EXPECT--
min1_result=string min1_warnings=0
t10_result=string t10_warnings=0 t10_body=plain-ok
max86400_result=string max86400_warnings=0
explicit30_result=string explicit30_warnings=0
absent_result=string absent_warnings=0 absent_body=plain-ok
zero_result=false zero_warnings=1
neg1_result=false neg1_warnings=1
over_result=false over_warnings=1
phpintmax_result=false phpintmax_warnings=1
abc_result=false abc_warnings=1
str10_result=false str10_warnings=1
str3foo_result=false str3foo_warnings=1
emptystr_result=false emptystr_warnings=1
float_result=false float_warnings=1
float10_result=false float10_warnings=1
overflow_result=false overflow_warnings=1
bool_result=false bool_warnings=1
null_result=false null_warnings=1
array_result=false array_warnings=1
warn_names_option=yes
