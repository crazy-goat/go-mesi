--TEST--
parse_with_config() debug: strict boolean validation
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// #216: `debug` is a STRICT boolean option. Only true/false are accepted.
// Unlike block_private_ips (bool or int 0/1) every other type — integer
// 0/1 included — emits E_WARNING naming the option and makes the call
// return false, so a typo can never silently flip parser diagnostics on
// or off. `debug => true` is exercised by parse_with_config_debug_true.phpt
// (its DEBUG lines would otherwise pollute this test's exact output).
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

// --- accepted: explicit false (true covered by the diagnostics test) ---
list($r,$w)=run(['debug' => false]);
echo "false_result=".($r===false?'false':'string')." false_warnings=".count($w)." false_body=".($r===false?'':$r)."\n";

// --- absent: legacy positional path, no warnings ---
list($r,$w)=run([]);
echo "absent_result=".($r===false?'false':'string')." absent_warnings=".count($w)." absent_body=".($r===false?'':$r)."\n";

// --- rejected: integers never coerce (strict, unlike block_private_ips) ---
list($r,$w)=run(['debug' => 1]);
echo "int1_result=".($r===false?'false':'string')." int1_warnings=".count($w)."\n";
list($r,$w)=run(['debug' => 0]);
echo "int0_result=".($r===false?'false':'string')." int0_warnings=".count($w)."\n";

// --- rejected: strings, including PHP's usual truthy/falsy spellings ---
list($r,$w)=run(['debug' => 'true']);
echo "strtrue_result=".($r===false?'false':'string')." strtrue_warnings=".count($w)."\n";
list($r,$w)=run(['debug' => 'false']);
echo "strfalse_result=".($r===false?'false':'string')." strfalse_warnings=".count($w)."\n";
list($r,$w)=run(['debug' => '1']);
echo "str1_result=".($r===false?'false':'string')." str1_warnings=".count($w)."\n";
list($r,$w)=run(['debug' => '']);
echo "emptystr_result=".($r===false?'false':'string')." emptystr_warnings=".count($w)."\n";

// --- rejected: floats, null, arrays, objects ---
list($r,$w)=run(['debug' => 1.0]);
echo "float_result=".($r===false?'false':'string')." float_warnings=".count($w)."\n";
list($r,$w)=run(['debug' => null]);
echo "null_result=".($r===false?'false':'string')." null_warnings=".count($w)."\n";
list($r,$w)=run(['debug' => [true]]);
echo "array_result=".($r===false?'false':'string')." array_warnings=".count($w)."\n";
list($r,$w)=run(['debug' => new stdClass()]);
echo "object_result=".($r===false?'false':'string')." object_warnings=".count($w)."\n";

// the warning text must name the option and say it expects a boolean
list($r,$w)=run(['debug' => 1]);
echo "warn_names_option=".(count($w)===1 && strpos($w[0], 'debug') !== false ? 'yes':'no')."\n";
echo "warn_explains_bool=".(count($w)===1 && strpos($w[0], 'boolean') !== false ? 'yes':'no')."\n";
?>
--EXPECT--
false_result=string false_warnings=0 false_body=plain-ok
absent_result=string absent_warnings=0 absent_body=plain-ok
int1_result=false int1_warnings=1
int0_result=false int0_warnings=1
strtrue_result=false strtrue_warnings=1
strfalse_result=false strfalse_warnings=1
str1_result=false str1_warnings=1
emptystr_result=false emptystr_warnings=1
float_result=false float_warnings=1
null_result=false null_warnings=1
array_result=false array_warnings=1
object_result=false object_warnings=1
warn_names_option=yes
warn_explains_bool=yes
