--TEST--
parse_with_config() max_workers: 20 includes drained through a 2-goroutine pool, 0/absent library default
--SKIPIF--
<?php
// The pool observable is a peak-concurrency tracker on a CONCURRENT
// backend — a single-threaded `php -S` would serialize every hold into a
// peak of 1 and prove nothing. The repo's own test-server (Go, the same
// binary test.sh/CI use, with /hold + /track from #206 — the
// #170/#192/#171 tracker pattern) is built here into the temp dir. Go is
// a documented build requirement of this extension (README "Requirements"),
// but a missing toolchain or failed build skips instead of failing.
if (!extension_loaded('mesi')) die('skip');
$src = __DIR__ . '/../../servers/test-server';
if (!is_dir($src)) die('skip servers/test-server source not found');
$bin = rtrim(sys_get_temp_dir(), '/') . '/mesi_test_server_211';
$out = [];
$rc = 0;
exec('cd ' . escapeshellarg($src) . ' && go build -o ' . escapeshellarg($bin) . ' . 2>&1', $out, $rc);
if ($rc !== 0 || !is_executable($bin)) {
    die('skip test-server build failed: ' . implode(' ', $out));
}
?>
--FILE--
<?php
// Functional coverage for #211: the issue's ACs —
//   - max_workers 2 + 20 x 1500 ms /hold includes -> HARD pool bound:
//     peak <= 2 AND peak >= 2, i.e. exactly 2. Upper bound: the pool is
//     workerCount = min(MaxWorkers, len(esiJobs)) = 2 goroutines
//     (mesi/parser.go:118-129) and each processes one include at a
//     time (the fetch is synchronous inside the goroutine), so on this
//     flat page the backend counter can never exceed 2 — a broken
//     route (key not rendered / not resolved) would fall back to the
//     default pool min(NumCPU*4, 20) >= 4 and show peak >= 4, failing
//     the ceiling, so this assertion also proves the maxWorkers key
//     reached the core (the issue's "config JSON includes it" AC).
//     Lower bound: both goroutines grab their first job from the
//     buffered jobs channel (mesi/parser.go:132,175-178) within
//     microseconds while each backend hold lasts 1500 ms, so the two
//     holds must overlap -> peak >= 2 (the same exact-cap invariant
//     Apache Test 40 (#171) and CLI Test 33 (#197) shipped peak == 2
//     against for the identical mechanism).
//   - explicit 0 -> library default: peak >= 4 (pool min(NumCPU*4, 20)
//     >= 4 goroutines, NumCPU >= 1) — same bound as the absent case,
//     pinning "0 and absent behave identically".
//   - absent key -> library default NumCPU*4: peak >= 4,
//     byte-identical legacy path.
// Budget math (the absent timeout key): the call keeps the documented
// 30 s default (positional path / ParseJson timeoutSeconds absent ->
// config.ResolveTimeout(nil) = 30 s). The budget ERODES as the parse
// runs (WithElapsedTime, mesi/parser.go:158): an include picked up at
// time t gets 30 s - t. Workers 2 -> 10 waves x 1500 ms = ~15 s
// nominal; the last wave starts ~13.5 s, leaving ~16.5 s >> 1.5 s —
// only waves averaging >= 2.85 s (~1.9x nominal) could push it past
// the 28.5 s erosion floor. 0/absent -> <= 5 waves (pool >= 4) =
// <= ~7.5 s. Any warning would also mean the ParseJson symbol lookup
// failed, so warnings=0 doubles as proof the routed path is live.
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});

$bin = rtrim(sys_get_temp_dir(), '/') . '/mesi_test_server_211';
$sock = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
$name = stream_socket_get_name($sock, false);
fclose($sock);
$port = (int)substr($name, strrpos($name, ':') + 1);

$cmd = 'MESI_TEST_SERVER_PORT=' . $port . ' ' . escapeshellarg($bin)
     . ' >/dev/null 2>&1 & echo $!';
$pid = (int)trim(shell_exec($cmd));
register_shutdown_function(function () use ($pid) {
    if ($pid <= 0) return;
    // #469 lesson: kill a specific PID, then WAIT for it (bounded poll)
    // so the child is actually reaped instead of lingering past the
    // test run.
    exec('kill ' . $pid . ' 2>/dev/null');
    for ($i = 0; $i < 50; $i++) {
        $probe = [];
        exec('ps -p ' . $pid . ' -o pid= 2>/dev/null', $probe);
        if (count($probe) === 0) return;
        usleep(20000);
    }
    exec('kill -9 ' . $pid . ' 2>/dev/null');
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
    $input = '<p>MW-TEST</p>';
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

// 1. max_workers 2: hard pool bound — peak == 2, 20/20 fragments
//    delivered (queued behind the pool, never dropped).
list($r, $w, $peak) = run_parse($port, 'mw-pool', ['max_workers' => 2]);
echo "pool_result=".($r===false?'false':'string')."\n";
echo "pool_fragments=".(is_string($r) ? substr_count($r, 'Held 1500') : -1)."\n";
echo "pool_peak_le2=".($peak <= 2 ? 'yes':'no')."\n";
echo "pool_peak_ge2=".($peak >= 2 ? 'yes':'no')."\n";
echo "pool_rawtag=".(is_string($r) && strpos($r, '<esi:include') !== false ? 'yes':'no')."\n";
echo "pool_pagetext=".(is_string($r) && strpos($r, 'MW-TEST') !== false ? 'yes':'no')."\n";
echo "pool_warnings=".count($w)."\n";

// 2. explicit 0: the documented "library default" value — same
//    unthrottled fan-out as absent (peak >= 4).
list($r, $w, $peak) = run_parse($port, 'mw-zero', ['max_workers' => 0]);
echo "zero_result=".($r===false?'false':'string')."\n";
echo "zero_fragments=".(is_string($r) ? substr_count($r, 'Held 1500') : -1)."\n";
echo "zero_peak_ge4=".($peak >= 4 ? 'yes':'no')."\n";
echo "zero_warnings=".count($w)."\n";

// 3. absent key: documented default 0 = library default NumCPU*4,
//    backward-compatible positional-equivalent behaviour —
//    unthrottled fan-out.
list($r, $w, $peak) = run_parse($port, 'mw-absent', []);
echo "absent_result=".($r===false?'false':'string')."\n";
echo "absent_fragments=".(is_string($r) ? substr_count($r, 'Held 1500') : -1)."\n";
echo "absent_peak_ge4=".($peak >= 4 ? 'yes':'no')."\n";
echo "absent_warnings=".count($w)."\n";
?>
--EXPECT--
pool_result=string
pool_fragments=20
pool_peak_le2=yes
pool_peak_ge2=yes
pool_rawtag=no
pool_pagetext=yes
pool_warnings=0
zero_result=string
zero_fragments=20
zero_peak_ge4=yes
zero_warnings=0
absent_result=string
absent_fragments=20
absent_peak_ge4=yes
absent_warnings=0
