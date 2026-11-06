--TEST--
parse_with_config() max_body_size / on_oversize: option parsing and validation
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// Strict validation contract (#538): an absent max_body_size keeps the
// documented default 0 = unlimited (byte-identical to the legacy path),
// and on_oversize defaults to "pass". Every malformed EXPLICIT value must
// produce E_WARNING naming the option and make parse_with_config() return
// false — never a silent coercion. These are input-handling options in the
// extension, not parser config: no ParseJson routing, so a warning here
// means validation failed (never a missing symbol).
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});
function run(array $cfg, string $input = 'plain-ok') {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse_with_config($input, 5, 'http://127.0.0.1/', $cfg);
    return [$r, array_slice($warnings, $before)];
}

// --- accepted: 0 (unlimited), exact boundary 1 with a 1-byte body,
//     typical, PHP_INT_MAX, absent ---
list($r,$w)=run(['max_body_size'=>0]);
echo "zero_result=".($r===false?'false':'string')." zero_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>1], 'x');
echo "min1_result=".($r===false?'false':'string')." min1_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>1048576]);
echo "mb1_result=".($r===false?'false':'string')." mb1_warnings=".count($w)." mb1_body=".($r===false?'':$r)."\n";
list($r,$w)=run(['max_body_size'=>PHP_INT_MAX]);
echo "intmax_result=".($r===false?'false':'string')." intmax_warnings=".count($w)."\n";
list($r,$w)=run([]);
echo "absent_result=".($r===false?'false':'string')." absent_warnings=".count($w)." absent_body=".($r===false?'':$r)."\n";

// --- accepted modes: pass/error at or below the limit; a mode without a
//     positive limit is a validated no-op ---
list($r,$w)=run(['max_body_size'=>1,'on_oversize'=>'pass'], 'x');
echo "mode_pass_result=".($r===false?'false':'string')." mode_pass_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>1,'on_oversize'=>'error'], 'x');
echo "mode_error_result=".($r===false?'false':'string')." mode_error_warnings=".count($w)."\n";
list($r,$w)=run(['on_oversize'=>'pass'], 'x');
echo "mode_nolimit_result=".($r===false?'false':'string')." mode_nolimit_warnings=".count($w)."\n";

// --- rejected max_body_size: negatives and non-integers ---
list($r,$w)=run(['max_body_size'=>-1]);
echo "neg1_result=".($r===false?'false':'string')." neg1_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>-100]);
echo "neg100_result=".($r===false?'false':'string')." neg100_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>'1048576']); // numeric string must NOT coerce
echo "str_result=".($r===false?'false':'string')." str_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>'abc']);
echo "abc_result=".($r===false?'false':'string')." abc_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>'']);
echo "emptystr_result=".($r===false?'false':'string')." emptystr_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>1.5]);
echo "float_result=".($r===false?'false':'string')." float_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>10.0]); // integral float still not IS_LONG
echo "float10_result=".($r===false?'false':'string')." float10_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>99999999999999999999]); // overflow literal -> float
echo "overflow_result=".($r===false?'false':'string')." overflow_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>true]);
echo "bool_result=".($r===false?'false':'string')." bool_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>null]);
echo "null_result=".($r===false?'false':'string')." null_warnings=".count($w)."\n";
list($r,$w)=run(['max_body_size'=>[100]]);
echo "array_result=".($r===false?'false':'string')." array_warnings=".count($w)."\n";

// --- rejected on_oversize: unknown strings and non-strings ---
list($r,$w)=run(['on_oversize'=>'drop']);
echo "mode_drop_result=".($r===false?'false':'string')." mode_drop_warnings=".count($w)."\n";
list($r,$w)=run(['on_oversize'=>'PASS']); // values are case-sensitive
echo "mode_upper_result=".($r===false?'false':'string')." mode_upper_warnings=".count($w)."\n";
list($r,$w)=run(['on_oversize'=>'']);
echo "mode_empty_result=".($r===false?'false':'string')." mode_empty_warnings=".count($w)."\n";
list($r,$w)=run(['on_oversize'=>true]);
echo "mode_bool_result=".($r===false?'false':'string')." mode_bool_warnings=".count($w)."\n";
list($r,$w)=run(['on_oversize'=>1]);
echo "mode_int_result=".($r===false?'false':'string')." mode_int_warnings=".count($w)."\n";
list($r,$w)=run(['on_oversize'=>null]);
echo "mode_null_result=".($r===false?'false':'string')." mode_null_warnings=".count($w)."\n";
list($r,$w)=run(['on_oversize'=>['pass']]);
echo "mode_array_result=".($r===false?'false':'string')." mode_array_warnings=".count($w)."\n";

// the warning text must name the option (fail-loud rule)
list($r,$w)=run(['max_body_size'=>-1]);
echo "warn_names_max_body_size=".(count($w)===1 && strpos($w[0], 'max_body_size') !== false ? 'yes':'no')."\n";
list($r,$w)=run(['on_oversize'=>'drop']);
echo "warn_names_on_oversize=".(count($w)===1 && strpos($w[0], 'on_oversize') !== false ? 'yes':'no')."\n";
?>
--EXPECT--
zero_result=string zero_warnings=0
min1_result=string min1_warnings=0
mb1_result=string mb1_warnings=0 mb1_body=plain-ok
intmax_result=string intmax_warnings=0
absent_result=string absent_warnings=0 absent_body=plain-ok
mode_pass_result=string mode_pass_warnings=0
mode_error_result=string mode_error_warnings=0
mode_nolimit_result=string mode_nolimit_warnings=0
neg1_result=false neg1_warnings=1
neg100_result=false neg100_warnings=1
str_result=false str_warnings=1
abc_result=false abc_warnings=1
emptystr_result=false emptystr_warnings=1
float_result=false float_warnings=1
float10_result=false float10_warnings=1
overflow_result=false overflow_warnings=1
bool_result=false bool_warnings=1
null_result=false null_warnings=1
array_result=false array_warnings=1
mode_drop_result=false mode_drop_warnings=1
mode_upper_result=false mode_upper_warnings=1
mode_empty_result=false mode_empty_warnings=1
mode_bool_result=false mode_bool_warnings=1
mode_int_result=false mode_int_warnings=1
mode_null_result=false mode_null_warnings=1
mode_array_result=false mode_array_warnings=1
warn_names_max_body_size=yes
warn_names_on_oversize=yes
