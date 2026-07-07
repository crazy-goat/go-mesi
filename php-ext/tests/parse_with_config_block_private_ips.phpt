--TEST--
parse_with_config() block_private_ips: option parsing + SSRF dial-time blocking
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
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', ['block_private_ips' => true]);
echo "true_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "true_warnings=" . count($warnings) . "\n";

// 2. false -> no warning, returns string
$before = count($warnings);
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', ['block_private_ips' => false]);
echo "false_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "false_warnings=" . (count($warnings) - $before) . "\n";

// 3. absent -> secure default (true), no warning, returns string
$before = count($warnings);
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', []);
echo "absent_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "absent_warnings=" . (count($warnings) - $before) . "\n";

// 4. wrong type -> E_WARNING + false
$before = count($warnings);
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', ['block_private_ips' => 'yes']);
echo "wrongtype_result=" . ($r === false ? 'false' : 'string') . "\n";
echo "wrongtype_warnings=" . (count($warnings) - $before) . "\n";

// 5. SSRF: start a local server on 127.0.0.1 (loopback is private/reserved)
//    and include from it. With blocking OFF the fetch succeeds; with
//    blocking ON (or default) the dial is refused at dial time.
$pid = 0;
$tmp = tempnam(sys_get_temp_dir(), 'mesi') . '.php';
file_put_contents($tmp, '<?php echo "ALLOWED-BODY";');
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

// 5a. block_private_ips=false -> allowed, server responds
$r = @\mesi\parse_with_config(
    '<esi:include src="' . $url . '" />',
    5, 'http://127.0.0.1/', ['block_private_ips' => false]);
echo "allowed_body=" . ($r === false ? 'false' : $r) . "\n";

// 5b. block_private_ips=true -> blocked (loopback is private/reserved)
$r = @\mesi\parse_with_config(
    '<esi:include src="' . $url . '" />',
    5, 'http://127.0.0.1/', ['block_private_ips' => true]);
echo "blocked_body=" . ($r === false ? 'false' : $r) . "\n";

// 5c. absent -> secure default (true) -> blocked
$r = @\mesi\parse_with_config(
    '<esi:include src="' . $url . '" />',
    5, 'http://127.0.0.1/', []);
echo "default_body=" . ($r === false ? 'false' : $r) . "\n";
?>
--EXPECT--
true_result=string
true_warnings=0
false_result=string
false_warnings=0
absent_result=string
absent_warnings=0
wrongtype_result=false
wrongtype_warnings=1
allowed_body=ALLOWED-BODY
blocked_body=
default_body=
