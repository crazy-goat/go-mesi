--TEST--
parse_with_config() shared_http_client: option parsing + per-parse fallback
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

// 1. option parsing: true -> no warning, returns string
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', ['shared_http_client' => true]);
echo "true_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "true_warnings=" . count($warnings) . "\n";

// 2. false -> no warning, returns string (per-parse clients)
$before = count($warnings);
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', ['shared_http_client' => false]);
echo "false_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "false_warnings=" . (count($warnings) - $before) . "\n";

// 3. absent -> default (true), no warning, returns string
$before = count($warnings);
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', []);
echo "absent_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "absent_warnings=" . (count($warnings) - $before) . "\n";

// 4. int accepted (0/1, mirroring block_private_ips)
$before = count($warnings);
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', ['shared_http_client' => 0]);
echo "int0_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "int0_warnings=" . (count($warnings) - $before) . "\n";

// 5. wrong type -> E_WARNING + false
$before = count($warnings);
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', ['shared_http_client' => 'yes']);
echo "wrongtype_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "wrongtype_warnings=" . (count($warnings) - $before) . "\n";

// 6. Functional: with shared_http_client=false, includes still resolve via
//    per-parse clients (SSRF-safe transport honours block_private_ips).
$pid = 0;
$tmp = tempnam(sys_get_temp_dir(), 'mesi') . '.php';
file_put_contents($tmp, '<?php echo "SHARED-OFF-BODY";');
register_shutdown_function(function () use (&$pid, $tmp) {
    if ($pid) { exec('kill ' . (int)$pid . ' 2>/dev/null'); }
    @unlink($tmp);
});

$sock = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
$name = stream_socket_get_name($sock, false);
fclose($sock);
$port = (int)substr($name, strrpos($name, ':') + 1);

$cmd = 'php -S 127.0.0.1:' . $port . ' -t ' . escapeshellarg(dirname($tmp)) . ' >/dev/null 2>&1 & echo $!';
$pid = (int)trim(shell_exec($cmd));
// Wait for the built-in server to accept connections.
for ($i = 0; $i < 50; $i++) {
    $c = @fsockopen('127.0.0.1', $port, $e, $es, 0.1);
    if ($c !== false) { fclose($c); break; }
    usleep(20000);
}
$url = 'http://127.0.0.1:' . $port . '/' . basename($tmp);

// 6a. shared=false + block_private_ips=false -> fetch succeeds per-parse
$r = @\mesi\parse_with_config(
    '<esi:include src="' . $url . '" />',
    5, 'http://127.0.0.1/',
    ['shared_http_client' => false, 'block_private_ips' => false]);
echo "shared_off_body=" . ($r === false ? 'false' : $r) . "\n";

// 6b. shared=false + block_private_ips=true (default) -> loopback blocked
$r = @\mesi\parse_with_config(
    '<esi:include src="' . $url . '" />',
    5, 'http://127.0.0.1/', ['shared_http_client' => false]);
echo "shared_off_blocked=" . ($r === false ? 'false' : $r) . "\n";

// 6c. back to shared=true -> client re-attached, fetch succeeds again
$r = @\mesi\parse_with_config(
    '<esi:include src="' . $url . '" />',
    5, 'http://127.0.0.1/',
    ['shared_http_client' => true, 'block_private_ips' => false]);
echo "shared_on_body=" . ($r === false ? 'false' : $r) . "\n";
?>
--EXPECT--
true_result=string
true_warnings=0
false_result=string
false_warnings=0
absent_result=string
absent_warnings=0
int0_result=string
int0_warnings=0
wrongtype_result=false
wrongtype_warnings=1
shared_off_body=SHARED-OFF-BODY
shared_off_blocked=
shared_on_body=SHARED-OFF-BODY
