--TEST--
parse_with_config() include_error_marker: strict string validation
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// #221: `include_error_marker` is a STRICT string option. Every other type
// (integer, float, bool, null, array, object) emits E_WARNING naming the
// option and makes the call return false, so a typo can never silently
// produce a page without (or with a wrong) error marker. An embedded NUL
// byte is rejected too: the value crosses to libgomesi as a NUL-terminated
// C string, so accepting it would silently truncate the marker.
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});
function run($value) {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', [
        'include_error_marker' => $value,
    ]);
    return [$r, array_slice($warnings, $before)];
}

// --- accepted: any string, including empty ---
list($r, $w) = run('<!-- ok -->');
echo "string_result=" . ($r === false ? 'false' : 'string') . " string_warnings=" . count($w) . " string_body=" . ($r === false ? '' : $r) . "\n";
list($r, $w) = run('');
echo "empty_result=" . ($r === false ? 'false' : 'string') . " empty_warnings=" . count($w) . "\n";

// --- rejected: every non-string type ---
$rejected = [
    'int' => 1,
    'float' => 1.5,
    'true' => true,
    'false' => false,
    'null' => null,
    'array' => ['<!-- x -->'],
    'object' => new stdClass(),
];
foreach ($rejected as $name => $value) {
    list($r, $w) = run($value);
    echo $name . "_result=" . ($r === false ? 'false' : 'string') . " " . $name . "_warnings=" . count($w) . "\n";
}

// --- rejected: embedded NUL would silently truncate the marker ---
list($r, $w) = run("before\0after");
echo "nul_result=" . ($r === false ? 'false' : 'string') . " nul_warnings=" . count($w) . "\n";

// the warning text must name the option and say it expects a string
list($r, $w) = run(42);
echo "warn_names_option=" . (count($w) === 1 && strpos($w[0], 'include_error_marker') !== false ? 'yes' : 'no') . "\n";
echo "warn_explains_string=" . (count($w) === 1 && strpos($w[0], 'string') !== false ? 'yes' : 'no') . "\n";
list($r, $w) = run("before\0after");
echo "nul_warn_explains_nul=" . (count($w) === 1 && strpos($w[0], 'NUL') !== false ? 'yes' : 'no') . "\n";
?>
--EXPECT--
string_result=string string_warnings=0 string_body=plain-ok
empty_result=string empty_warnings=0
int_result=false int_warnings=1
float_result=false float_warnings=1
true_result=false true_warnings=1
false_result=false false_warnings=1
null_result=false null_warnings=1
array_result=false array_warnings=1
object_result=false object_warnings=1
nul_result=false nul_warnings=1
warn_names_option=yes
warn_explains_string=yes
nul_warn_explains_nul=yes
