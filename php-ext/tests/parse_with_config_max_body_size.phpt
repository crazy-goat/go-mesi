--TEST--
parse_with_config() max_body_size / on_oversize: pass, error, boundary and unlimited
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// Functional coverage for #538. The limit applies to the PARENT body that
// parse_with_config() receives, before any ESI processing:
//   - under the limit and exactly at it -> the include expands.
//   - over the limit with on_oversize=pass (the default) -> the input is
//     returned byte-for-byte unchanged, raw <esi:include> tag included,
//     and the include is never fetched.
//   - over the limit with on_oversize=error -> false + one E_WARNING that
//     names the option and leaks no internal details (no include URL).
//   - absent max_body_size and explicit 0 -> unlimited (legacy path).
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});

$dir = sys_get_temp_dir();
$frag = tempnam($dir, 'mesi_frag');
file_put_contents($frag, 'FRAGMARK');

$sock = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
$name = stream_socket_get_name($sock, false);
fclose($sock);
$port = (int)substr($name, strrpos($name, ':') + 1);

$cmd = 'php -S 127.0.0.1:' . $port . ' -t ' . escapeshellarg($dir) . ' >/dev/null 2>&1 & echo $!';
$pid = (int)trim(shell_exec($cmd));
register_shutdown_function(function () use ($pid, $frag) {
    if ($pid) { exec('kill ' . (int)$pid . ' 2>/dev/null'); }
    @unlink($frag);
});
for ($i = 0; $i < 50; $i++) {
    $c = @fsockopen('127.0.0.1', $port, $e, $es, 0.1);
    if ($c !== false) { fclose($c); break; }
    usleep(20000);
}
// Drop the readiness probes so every count below covers parse calls only.
$warnings = [];

$src = 'http://127.0.0.1:' . $port . '/' . basename($frag);
$input = '<p>BODY-TEST</p><esi:include src="' . $src . '" />';
$len = strlen($input);

function run_body(string $input, array $cfg) {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse_with_config($input, 5, 'http://127.0.0.1/', $cfg);
    return [$r, array_slice($warnings, $before)];
}

// 1. under the limit: processed normally.
list($r, $w) = run_body($input, ['max_body_size' => $len + 1, 'block_private_ips' => false]);
echo "under_result=".($r===false?'false':'string')."\n";
echo "under_expanded=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "under_rawtag=".(is_string($r) && strpos($r, '<esi:include') !== false ? 'yes':'no')."\n";
echo "under_warnings=".count($w)."\n";

// 2. exactly at the limit: processed (the limit rejects only len > cap).
list($r, $w) = run_body($input, ['max_body_size' => $len, 'block_private_ips' => false]);
echo "exact_expanded=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "exact_rawtag=".(is_string($r) && strpos($r, '<esi:include') !== false ? 'yes':'no')."\n";
echo "exact_warnings=".count($w)."\n";

// 3. limit+1 over, default on_oversize ("pass"): unchanged, no processing.
list($r, $w) = run_body($input, ['max_body_size' => $len - 1, 'block_private_ips' => false]);
echo "pass_result=".($r===false?'false':'string')."\n";
echo "pass_unchanged=".(is_string($r) && $r === $input ? 'yes':'no')."\n";
echo "pass_rawtag=".(is_string($r) && strpos($r, '<esi:include') !== false ? 'yes':'no')."\n";
echo "pass_fetched=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "pass_warnings=".count($w)."\n";
echo "pass_warn_names=".(count($w)===1 && strpos($w[0], 'max_body_size') !== false ? 'yes':'no')."\n";
echo "pass_warn_leaks_url=".(count($w)===1 && strpos($w[0], '127.0.0.1') !== false ? 'yes':'no')."\n";

// 4. over the limit with on_oversize=error: false + warning.
list($r, $w) = run_body($input, ['max_body_size' => $len - 1, 'on_oversize' => 'error', 'block_private_ips' => false]);
echo "error_result=".($r===false?'false':'string')."\n";
echo "error_warnings=".count($w)."\n";
echo "error_warn_names=".(count($w)===1 && strpos($w[0], 'max_body_size') !== false ? 'yes':'no')."\n";
echo "error_warn_leaks_url=".(count($w)===1 && strpos($w[0], '127.0.0.1') !== false ? 'yes':'no')."\n";

// 5. explicit on_oversize=pass over the limit: same unchanged result.
list($r, $w) = run_body($input, ['max_body_size' => $len - 1, 'on_oversize' => 'pass', 'block_private_ips' => false]);
echo "explicit_pass_unchanged=".(is_string($r) && $r === $input ? 'yes':'no')."\n";
echo "explicit_pass_warnings=".count($w)."\n";

// 6. absent limit: unlimited (legacy behaviour).
list($r, $w) = run_body($input, ['block_private_ips' => false]);
echo "absent_expanded=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "absent_warnings=".count($w)."\n";

// 7. explicit 0: the documented "unlimited" value.
list($r, $w) = run_body($input, ['max_body_size' => 0, 'block_private_ips' => false]);
echo "zero_expanded=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "zero_warnings=".count($w)."\n";

// 8. on_oversize=error below the limit is a no-op.
list($r, $w) = run_body($input, ['max_body_size' => $len + 1, 'on_oversize' => 'error', 'block_private_ips' => false]);
echo "below_error_expanded=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "below_error_warnings=".count($w)."\n";
?>
--EXPECT--
under_result=string
under_expanded=yes
under_rawtag=no
under_warnings=0
exact_expanded=yes
exact_rawtag=no
exact_warnings=0
pass_result=string
pass_unchanged=yes
pass_rawtag=yes
pass_fetched=no
pass_warnings=1
pass_warn_names=yes
pass_warn_leaks_url=no
error_result=false
error_warnings=1
error_warn_names=yes
error_warn_leaks_url=no
explicit_pass_unchanged=yes
explicit_pass_warnings=1
absent_expanded=yes
absent_warnings=0
zero_expanded=yes
zero_warnings=0
below_error_expanded=yes
below_error_warnings=0
