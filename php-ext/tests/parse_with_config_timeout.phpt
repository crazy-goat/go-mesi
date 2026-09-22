--TEST--
parse_with_config() timeout: slow include respects the configured budget
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// Functional coverage for #181: a slow backend (sleeps 3s per request) vs
// timeout=2 (include must be cut at ~2s, fragment absent), timeout=30 and
// an absent key (documented default 30s — include succeeds after ~3s).
// timeout=2 against a fast file proves the ParseJson path still fetches
// normally within budget. If the timeout were silently ignored (positional
// path / missing ParseJson symbol), the timeout2_* assertions fail: the
// fragment would appear after ~3s.
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});

$dir = sys_get_temp_dir();
$slow = tempnam($dir, 'mesi_slow') . '.php';
file_put_contents($slow, '<?php sleep(3); echo "SLOW-FRAGMENT";');
$fast = tempnam($dir, 'mesi_fast') . '.php';
file_put_contents($fast, '<?php echo "FAST-FRAGMENT";');

$sock = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
$name = stream_socket_get_name($sock, false);
fclose($sock);
$port = (int)substr($name, strrpos($name, ':') + 1);

$cmd = 'php -S 127.0.0.1:' . $port . ' -t ' . escapeshellarg($dir) . ' >/dev/null 2>&1 & echo $!';
$pid = (int)trim(shell_exec($cmd));
register_shutdown_function(function () use (&$pid, $slow, $fast) {
    if ($pid) { exec('kill ' . (int)$pid . ' 2>/dev/null'); }
    @unlink($slow);
    @unlink($fast);
});
for ($i = 0; $i < 50; $i++) {
    $c = @fsockopen('127.0.0.1', $port, $e, $es, 0.1);
    if ($c !== false) { fclose($c); break; }
    usleep(20000);
}
// The readiness probes above ran under @ but still reached the error
// handler — drop them so every count below covers parse calls only.
$warnings = [];

function fetch_to(int $port, string $file, array $cfg) {
    $src = 'http://127.0.0.1:' . $port . '/' . basename($file);
    $t0 = microtime(true);
    $r = @\mesi\parse_with_config(
        '<esi:include src="' . $src . '" />',
        5, 'http://127.0.0.1/', $cfg);
    $el = microtime(true) - $t0;
    return [$r, $el];
}

// 1. slow backend + timeout=2: cut at ~2s, fragment must be ABSENT.
//    block_private_ips=false so the loopback dial is allowed and only the
//    timeout can fail the include.
list($r, $el) = fetch_to($port, $slow, ['timeout' => 2, 'block_private_ips' => false]);
echo "timeout2_result=".($r===false?'false':'string')."\n";
echo "timeout2_fragment=".(is_string($r) && strpos($r, 'SLOW-FRAGMENT') !== false ? 'yes':'no')."\n";
echo "timeout2_over1s=".($el >= 1.0 ? 'yes':'no')."\n";   // dial really waited (no instant fail)
echo "timeout2_under29s=".($el < 2.9 ? 'yes':'no')."\n";  // budget enforced (~2s)
echo "timeout2_warnings=".count($warnings)."\n";
$warnings = [];

// 2. fast backend + timeout=2: within budget -> fetched normally.
list($r, $el) = fetch_to($port, $fast, ['timeout' => 2, 'block_private_ips' => false]);
echo "fast2_result=".($r===false?'false':'string')."\n";
echo "fast2_fragment=".(is_string($r) && strpos($r, 'FAST-FRAGMENT') !== false ? 'yes':'no')."\n";
echo "fast2_warnings=".count($warnings)."\n";
$warnings = [];

// 3. slow backend + timeout=30: well above the 3s sleep -> succeeds (issue AC).
list($r, $el) = fetch_to($port, $slow, ['timeout' => 30, 'block_private_ips' => false]);
echo "timeout30_result=".($r===false?'false':'string')."\n";
echo "timeout30_fragment=".(is_string($r) && strpos($r, 'SLOW-FRAGMENT') !== false ? 'yes':'no')."\n";
echo "timeout30_warnings=".count($warnings)."\n";
$warnings = [];

// 4. key absent: documented default 30s applies -> succeeds, no warnings,
//    positional ParseWithConfigCtx path (backward compatible).
list($r, $el) = fetch_to($port, $slow, ['block_private_ips' => false]);
echo "absent_result=".($r===false?'false':'string')."\n";
echo "absent_fragment=".(is_string($r) && strpos($r, 'SLOW-FRAGMENT') !== false ? 'yes':'no')."\n";
echo "absent_warnings=".count($warnings)."\n";
?>
--EXPECT--
timeout2_result=string
timeout2_fragment=no
timeout2_over1s=yes
timeout2_under29s=yes
timeout2_warnings=0
fast2_result=string
fast2_fragment=yes
fast2_warnings=0
timeout30_result=string
timeout30_fragment=yes
timeout30_warnings=0
absent_result=string
absent_fragment=yes
absent_warnings=0
