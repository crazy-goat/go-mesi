--TEST--
parse_with_config() with cache_backend=memory dedups <esi:include>s within the same worker process
--SKIPIF--
<?php
if (!extension_loaded('mesi')) die('skip - mesi extension not loaded');
if (!function_exists('stream_socket_server')) die('skip - stream_socket_server not available');
if (!function_exists('proc_open')) die('skip - proc_open not available');
?>
--FILE--
<?php
/*
 * Spawn a child PHP CLI process whose only job is to be a counting TCP
 * HTTP server. The parent PHP process then calls
 * parse_with_config(cache_backend=memory) twice; the first call should
 * trigger ONE TCP request ("hits_after_first=1") and the second call
 * within TTL should trigger ZERO new TCP requests
 * ("hits_after_second=1").
 *
 * The child is invoked via proc_open; it writes its hit count to a
 * shared file locked on every accept for cross-process atomicity.
 */

$countFile = tempnam(sys_get_temp_dir(), 'mesi-count-');
file_put_contents($countFile, "0");
$readyFile = $countFile . '-ready';
@unlink($readyFile);

$serverScript = tempnam(sys_get_temp_dir(), 'mesi-server-');
file_put_contents($serverScript, <<<'PHP'
<?php
$port_file = $argv[1];
$count_file = $argv[2];
$ready_file = $argv[3];
$server = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
if ($server === false) { fwrite(STDERR, "ERR $errstr\n"); exit(1); }
$addr = stream_socket_get_name($server, false);
file_put_contents($port_file, $addr);
touch($ready_file);
stream_set_blocking($server, false);
while (true) {
    $client = @stream_socket_accept($server, 1);
    if ($client === false) continue;
    stream_set_blocking($client, false);
    $req = '';
    $deadline = microtime(true) + 2.0;
    while (microtime(true) < $deadline) {
        $c = @fread($client, 4096);
        if ($c === false || $c === '') { usleep(2000); continue; }
        $req .= $c;
        if (strpos($req, "\r\n\r\n") !== false) break;
    }
    $fp = fopen($count_file, 'c+');
    flock($fp, LOCK_EX);
    $n = (int)trim(fread($fp, 32));
    $n++;
    ftruncate($fp, 0);
    rewind($fp);
    fwrite($fp, (string)$n);
    fflush($fp);
    flock($fp, LOCK_UN);
    fclose($fp);
    $body = 'OK';
    $resp = "HTTP/1.0 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 2\r\nConnection: close\r\n\r\nOK";
    @fwrite($client, $resp);
    @fclose($client);
}
PHP);
$portFile = $countFile . '-port';

$cmd = escapeshellcmd(PHP_BINARY)
     . ' ' . escapeshellarg($serverScript)
     . ' ' . escapeshellarg($portFile)
     . ' ' . escapeshellarg($countFile)
     . ' ' . escapeshellarg($readyFile);
$proc = proc_open(
    $cmd,
    [1 => ['file', '/dev/null', 'w'], 2 => ['file', '/dev/null', 'w']],
    $pipes
);
if (!is_resource($proc)) {
    echo "spawn_failed\n";
    @unlink($countFile); @unlink($serverScript);
    exit;
}

// Wait until the server reports its address.
$deadline = microtime(true) + 5;
while (microtime(true) < $deadline && !file_exists($readyFile)) {
    usleep(5000);
}
if (!file_exists($readyFile)) {
    echo "server_not_ready\n";
    proc_terminate($proc);
    @unlink($countFile); @unlink($serverScript); @unlink($readyFile); @unlink($portFile);
    exit;
}
$addr = trim(file_get_contents($portFile));
$backend = "http://$addr";
$esiUrl = "$backend/esi";
$input = '<esi:include src="' . $esiUrl . '"/>';

$out1 = \mesi\parse_with_config(
    $input, 5, $backend,
    ['cache_backend' => 'memory', 'cache_size' => 100, 'cache_ttl' => 60, 'block_private_ips' => false]
);
usleep(300000);
$hits1 = (int)trim(file_get_contents($countFile));

$out2 = \mesi\parse_with_config(
    $input, 5, $backend,
    ['cache_backend' => 'memory', 'cache_size' => 100, 'cache_ttl' => 60, 'block_private_ips' => false]
);
usleep(300000);
$hits2 = (int)trim(file_get_contents($countFile));

echo "first_body=" . ($out1 === false ? 'false' : $out1) . "\n";
echo "hits_after_first=$hits1\n";
echo "second_body=" . ($out2 === false ? 'false' : $out2) . "\n";
echo "hits_after_second=$hits2\n";
echo "outputs_match=" . ($out1 === $out2 ? 'yes' : 'no') . "\n";

proc_terminate($proc);
proc_close($proc);
@unlink($countFile);
@unlink($serverScript);
@unlink($readyFile);
@unlink($portFile);
?>
--EXPECT--
first_body=OK
hits_after_first=1
second_body=OK
hits_after_second=1
outputs_match=yes
