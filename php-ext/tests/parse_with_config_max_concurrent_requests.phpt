--TEST--
parse_with_config() max_concurrent_requests: 20 includes funneled through cap 3, 0/absent unthrottled
--SKIPIF--
<?php
// The funnel observable is a peak-concurrency tracker on a CONCURRENT
// backend — a single-threaded `php -S` would serialize every hold into a
// peak of 1 and prove nothing. The repo's own test-server (Go, the same
// binary test.sh/CI use, extended with /hold + /track for #206 — the
// #170/#192 tracker pattern) is built here into the temp dir. Go is a
// documented build requirement of this extension (README "Requirements"),
// but a missing toolchain or failed build skips instead of failing.
if (!extension_loaded('mesi')) die('skip');
$src = __DIR__ . '/../../servers/test-server';
if (!is_dir($src)) die('skip servers/test-server source not found');
$bin = rtrim(sys_get_temp_dir(), '/') . '/mesi_test_server_206';
$out = [];
$rc = 0;
exec('cd ' . escapeshellarg($src) . ' && go build -o ' . escapeshellarg($bin) . ' . 2>&1', $out, $rc);
if ($rc !== 0 || !is_executable($bin)) {
    die('skip test-server build failed: ' . implode(' ', $out));
}
?>
--FILE--
<?php
// Functional coverage for #206: the issue's ACs —
//   - cap 3 + 20 x 1500 ms /hold includes -> FUNNELED: peak in [2,3]
//     (<= 3 is the hard semaphore invariant, mesi/fetch.go; >= 2 proves
//     a multi-slot queue rather than a serialisation to 1 — an exact
//     peak == 3 needs all three first-wave dials to overlap and is
//     scheduling-dependent, deliberately not asserted, same as Apache
//     Test 36 / CLI Test 28) and all 20 fragments delivered (queued
//     beyond the cap, never dropped),
//   - explicit 0 -> UNLIMITED: peak >= 4 (the worker pool is
//     min(NumCPU*4, 20) >= 4 goroutines, mesi/parser.go),
//   - absent key -> UNLIMITED: peak >= 4, byte-identical legacy path.
// The absent timeout key keeps the documented 30s default, well above
// the ~10.5 s worst case of 20 x 1500 ms queued 3 at a time. Any
// warning would also mean the ParseJson symbol lookup failed, so
// warnings=0 doubles as proof the routed path is live.
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});

$bin = rtrim(sys_get_temp_dir(), '/') . '/mesi_test_server_206';
$sock = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
$name = stream_socket_get_name($sock, false);
fclose($sock);
$port = (int)substr($name, strrpos($name, ':') + 1);

$cmd = 'MESI_TEST_SERVER_PORT=' . $port . ' ' . escapeshellarg($bin)
     . ' >/dev/null 2>&1 & echo $!';
$pid = (int)trim(shell_exec($cmd));
register_shutdown_function(function () use ($pid) {
    if ($pid) { exec('kill ' . (int)$pid . ' 2>/dev/null'); }
});
for ($i = 0; $i < 100; $i++) {
    $c = @fsockopen('127.0.0.1', $port, $e, $es, 0.1);
    if ($c !== false) { fclose($c); break; }
    usleep(20000);
}

function track_reset(int $port) {
    @file_get_contents('http://127.0.0.1:' . $port . '/track/reset');
}
function track_max(int $port): int {
    $v = @file_get_contents('http://127.0.0.1:' . $port . '/track/max');
    return $v === false ? -1 : (int)trim($v);
}
// 20 DISTINCT hold labels per parse, so every include reaches the
// backend's peak counter (and no dedup could swallow fetches).
function hold_input(int $port, string $label): string {
    $base = 'http://127.0.0.1:' . $port . '/hold/1500/' . $label;
    $input = '<p>MCR-TEST</p>';
    for ($i = 1; $i <= 20; $i++) {
        $input .= '<esi:include src="' . $base . '-' . $i . '" />';
    }
    return $input;
}
function run_parse(int $port, string $label, array $cfg): array {
    global $warnings;
    track_reset($port);
    $before = count($warnings);
    $r = @\mesi\parse_with_config(
        hold_input($port, $label), 5, 'http://127.0.0.1/',
        $cfg + ['block_private_ips' => false]);
    return [$r, array_slice($warnings, $before), track_max($port)];
}

// 1. cap 3: funneled — peak in [2,3], 20/20 fragments delivered.
list($r, $w, $peak) = run_parse($port, 'mcr-cap', ['max_concurrent_requests' => 3]);
echo "cap_result=".($r===false?'false':'string')."\n";
echo "cap_fragments=".(is_string($r) ? substr_count($r, 'Held 1500') : -1)."\n";
echo "cap_peak_le3=".($peak <= 3 ? 'yes':'no')."\n";
echo "cap_peak_ge2=".($peak >= 2 ? 'yes':'no')."\n";
echo "cap_rawtag=".(is_string($r) && strpos($r, '<esi:include') !== false ? 'yes':'no')."\n";
echo "cap_pagetext=".(is_string($r) && strpos($r, 'MCR-TEST') !== false ? 'yes':'no')."\n";
echo "cap_warnings=".count($w)."\n";

// 2. explicit 0: the documented "unlimited" value — unthrottled fan-out.
list($r, $w, $peak) = run_parse($port, 'mcr-zero', ['max_concurrent_requests' => 0]);
echo "zero_result=".($r===false?'false':'string')."\n";
echo "zero_fragments=".(is_string($r) ? substr_count($r, 'Held 1500') : -1)."\n";
echo "zero_peak_ge4=".($peak >= 4 ? 'yes':'no')."\n";
echo "zero_warnings=".count($w)."\n";

// 3. absent key: documented default 0 = unlimited, backward-compatible
//    positional-equivalent behaviour — unthrottled fan-out.
list($r, $w, $peak) = run_parse($port, 'mcr-absent', []);
echo "absent_result=".($r===false?'false':'string')."\n";
echo "absent_fragments=".(is_string($r) ? substr_count($r, 'Held 1500') : -1)."\n";
echo "absent_peak_ge4=".($peak >= 4 ? 'yes':'no')."\n";
echo "absent_warnings=".count($w)."\n";
?>
--EXPECT--
cap_result=string
cap_fragments=20
cap_peak_le3=yes
cap_peak_ge2=yes
cap_rawtag=no
cap_pagetext=yes
cap_warnings=0
zero_result=string
zero_fragments=20
zero_peak_ge4=yes
zero_warnings=0
absent_result=string
absent_fragments=20
absent_peak_ge4=yes
absent_warnings=0
