--TEST--
parse_with_config() max_response_size: over-cap rejected, under-cap/absent/0 deliver
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// Functional coverage for #201: a local static backend serves bodies of a
// given size; max_response_size caps a SINGLE include fetch in bytes.
//   - 200-byte body over a 100-byte cap  -> include fails, renders the
//     empty IncludeErrorMarker (no marker, no raw <esi:include tag).
//   - same body under a 1000-byte cap    -> delivered fully (marker +
//     exact byte count).
//   - cap == body size (200)             -> boundary: the core rejects
//     only when len > cap, so the exact cap delivers.
//   - absent key + 10 MB + 1-byte body   -> delivered: pins absent =
//     UNLIMITED against the issue's proposed "absent -> 10 MB" default
//     (that default only exists in mesi.CreateDefaultConfig, which the
//     php-ext positional/ParseJson paths never use — #169).
//   - explicit 0 + same large body       -> 0 is the documented
//     "unlimited" value (mesi/fetch.go only limits when > 0).
// Any warning would also mean the ParseJson symbol lookup failed, so
// warnings=0 doubles as proof the routed path is live.
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});

$dir = sys_get_temp_dir();
$frag = tempnam($dir, 'mesi_frag');
file_put_contents($frag, 'FRAGMARK' . str_repeat('#', 192));   // exactly 200 bytes
$big = tempnam($dir, 'mesi_big');
file_put_contents($big, 'BIGMARK' . str_repeat('#', 10485761 - 7)); // exactly 10 MB + 1

$sock = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
$name = stream_socket_get_name($sock, false);
fclose($sock);
$port = (int)substr($name, strrpos($name, ':') + 1);

$cmd = 'php -S 127.0.0.1:' . $port . ' -t ' . escapeshellarg($dir) . ' >/dev/null 2>&1 & echo $!';
$pid = (int)trim(shell_exec($cmd));
register_shutdown_function(function () use ($pid, $frag, $big) {
    if ($pid) { exec('kill ' . (int)$pid . ' 2>/dev/null'); }
    @unlink($frag);
    @unlink($big);
});
for ($i = 0; $i < 50; $i++) {
    $c = @fsockopen('127.0.0.1', $port, $e, $es, 0.1);
    if ($c !== false) { fclose($c); break; }
    usleep(20000);
}
// The readiness probes above ran under @ but still reached the error
// handler — drop them so every count below covers parse calls only.
$warnings = [];

function fetch_body(string $port, string $file, array $cfg) {
    global $warnings;
    $src = 'http://127.0.0.1:' . $port . '/' . basename($file);
    $before = count($warnings);
    $r = @\mesi\parse_with_config(
        '<p>SIZE-TEST</p><esi:include src="' . $src . '" />',
        5, 'http://127.0.0.1/', $cfg);
    return [$r, array_slice($warnings, $before)];
}

// 1. 200-byte body over a 100-byte cap: include must render empty.
list($r, $w) = fetch_body($port, $frag, ['max_response_size' => 100, 'block_private_ips' => false]);
echo "over_result=".($r===false?'false':'string')."\n";
echo "over_marker=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "over_hashtags=".(is_string($r) ? substr_count($r, '#') : -1)."\n";
echo "over_rawtag=".(is_string($r) && strpos($r, '<esi:include') !== false ? 'yes':'no')."\n";
echo "over_pagetext=".(is_string($r) && strpos($r, 'SIZE-TEST') !== false ? 'yes':'no')."\n";
echo "over_warnings=".count($w)."\n";

// 2. same body under a 1000-byte cap: delivered fully (marker + count).
list($r, $w) = fetch_body($port, $frag, ['max_response_size' => 1000, 'block_private_ips' => false]);
echo "under_result=".($r===false?'false':'string')."\n";
echo "under_marker=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "under_hashtags=".(is_string($r) ? substr_count($r, '#') : -1)."\n";
echo "under_warnings=".count($w)."\n";

// 3. exact boundary: cap == body size -> delivered (core rejects only len > cap).
list($r, $w) = fetch_body($port, $frag, ['max_response_size' => 200, 'block_private_ips' => false]);
echo "exact_marker=".(is_string($r) && strpos($r, 'FRAGMARK') !== false ? 'yes':'no')."\n";
echo "exact_hashtags=".(is_string($r) ? substr_count($r, '#') : -1)."\n";
echo "exact_warnings=".count($w)."\n";

// 4. absent key + 10 MB + 1 body: delivered -> absent = unlimited
//    (pins byte-identical legacy behaviour, refutes the issue's
//    "absent -> default 10 MB" AC). 16 = strlen('<p>SIZE-TEST</p>').
list($r, $w) = fetch_body($port, $big, ['block_private_ips' => false]);
echo "absent_result=".($r===false?'false':'string')."\n";
echo "absent_full=".(is_string($r) && strlen($r) === 16 + 10485761 ? 'yes':'no')."\n";
echo "absent_marker=".(is_string($r) && strpos($r, 'BIGMARK') !== false ? 'yes':'no')."\n";
echo "absent_warnings=".count($w)."\n";

// 5. explicit 0 (documented "unlimited") + same large body: delivered.
list($r, $w) = fetch_body($port, $big, ['max_response_size' => 0, 'block_private_ips' => false]);
echo "zero2_result=".($r===false?'false':'string')."\n";
echo "zero2_full=".(is_string($r) && strlen($r) === 16 + 10485761 ? 'yes':'no')."\n";
echo "zero2_marker=".(is_string($r) && strpos($r, 'BIGMARK') !== false ? 'yes':'no')."\n";
echo "zero2_warnings=".count($w)."\n";
?>
--EXPECT--
over_result=string
over_marker=no
over_hashtags=0
over_rawtag=no
over_pagetext=yes
over_warnings=0
under_result=string
under_marker=yes
under_hashtags=192
under_warnings=0
exact_marker=yes
exact_hashtags=192
exact_warnings=0
absent_result=string
absent_full=yes
absent_marker=yes
absent_warnings=0
zero2_result=string
zero2_full=yes
zero2_marker=yes
zero2_warnings=0
