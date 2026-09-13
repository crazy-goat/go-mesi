--TEST--
parse() / parse_with_config() reject max_depth outside [0, 10000]
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

function run_parse($depth) {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse('plain-ok', $depth, 'http://127.0.0.1/');
    return [$r, array_slice($warnings, $before)];
}

function run_pwc($depth) {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse_with_config('plain-ok', $depth, 'http://127.0.0.1/', []);
    return [$r, array_slice($warnings, $before)];
}

list($r, $w) = run_parse(0);
echo "parse_0_result=" . ($r === false ? 'false' : $r) . "\n";
echo "parse_0_warnings=" . count($w) . "\n";

list($r, $w) = run_parse(10000);
echo "parse_10000_result=" . ($r === false ? 'false' : $r) . "\n";
echo "parse_10000_warnings=" . count($w) . "\n";

list($r, $w) = run_parse(10001);
echo "parse_10001_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "parse_10001_warnings=" . count($w) . "\n";

list($r, $w) = run_parse(-1);
echo "parse_-1_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "parse_-1_warnings=" . count($w) . "\n";

list($r, $w) = run_pwc(0);
echo "pwc_0_result=" . ($r === false ? 'false' : $r) . "\n";
echo "pwc_0_warnings=" . count($w) . "\n";

list($r, $w) = run_pwc(10000);
echo "pwc_10000_result=" . ($r === false ? 'false' : $r) . "\n";
echo "pwc_10000_warnings=" . count($w) . "\n";

list($r, $w) = run_pwc(10001);
echo "pwc_10001_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "pwc_10001_warnings=" . count($w) . "\n";

list($r, $w) = run_pwc(-1);
echo "pwc_-1_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "pwc_-1_warnings=" . count($w) . "\n";
?>
--EXPECT--
parse_0_result=plain-ok
parse_0_warnings=0
parse_10000_result=plain-ok
parse_10000_warnings=0
parse_10001_result=false
parse_10001_warnings=1
parse_-1_result=false
parse_-1_warnings=1
pwc_0_result=plain-ok
pwc_0_warnings=0
pwc_10000_result=plain-ok
pwc_10000_warnings=0
pwc_10001_result=false
pwc_10001_warnings=1
pwc_-1_result=false
pwc_-1_warnings=1
