--TEST--
parse_with_config() cache_key_template differentiates cache keys by header and cookie
--SKIPIF--
<?php
if (!extension_loaded('mesi')) die('skip - mesi extension not loaded');
if (!function_exists('stream_socket_server')) die('skip - stream_socket_server not available');
if (!function_exists('proc_open')) die('skip - proc_open not available');
?>
--FILE--
<?php
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

$baseCfg = ['cache_backend' => 'memory', 'cache_size' => 100, 'cache_ttl' => 60, 'block_private_ips' => false];

// Call 1: header pl -> hits=1
$out1 = \mesi\parse_with_config($input, 5, $backend, array_merge($baseCfg, ['cache_key_template' => 'mesi:${url}:${header:Accept-Language}', 'request_headers' => ['Accept-Language' => 'pl']]));
usleep(300000);
$hits1 = (int)trim(file_get_contents($countFile));

// Call 2: header en -> distinct key -> hits=2
$out2 = \mesi\parse_with_config($input, 5, $backend, array_merge($baseCfg, ['cache_key_template' => 'mesi:${url}:${header:Accept-Language}', 'request_headers' => ['Accept-Language' => 'en']]));
usleep(300000);
$hits2 = (int)trim(file_get_contents($countFile));

// Call 3: header pl again -> cache hit -> hits stays 2
$out3 = \mesi\parse_with_config($input, 5, $backend, array_merge($baseCfg, ['cache_key_template' => 'mesi:${url}:${header:Accept-Language}', 'request_headers' => ['Accept-Language' => 'pl']]));
usleep(300000);
$hits3 = (int)trim(file_get_contents($countFile));

// Call 4a: cookie ab=v1 -> hits=3
$out4a = \mesi\parse_with_config($input, 5, $backend, array_merge($baseCfg, ['cache_key_template' => 'mesi:${url}:${cookie:ab}', 'request_cookies' => ['ab' => 'v1']]));
usleep(300000);
$hits4a = (int)trim(file_get_contents($countFile));

// Call 4b: cookie ab=v2 -> distinct -> hits=4
$out4b = \mesi\parse_with_config($input, 5, $backend, array_merge($baseCfg, ['cache_key_template' => 'mesi:${url}:${cookie:ab}', 'request_cookies' => ['ab' => 'v2']]));
usleep(300000);
$hits4b = (int)trim(file_get_contents($countFile));

echo "out1=" . ($out1 === false ? 'false' : $out1) . "\n";
echo "hits1=$hits1\n";
echo "out2=" . ($out2 === false ? 'false' : $out2) . "\n";
echo "hits2=$hits2\n";
echo "out3=" . ($out3 === false ? 'false' : $out3) . "\n";
echo "hits3=$hits3\n";
echo "out4a=" . ($out4a === false ? 'false' : $out4a) . "\n";
echo "hits4a=$hits4a\n";
echo "out4b=" . ($out4b === false ? 'false' : $out4b) . "\n";
echo "hits4b=$hits4b\n";
echo "outputs_match=" . (($out1 === $out2 && $out2 === $out3 && $out3 === $out4a && $out4a === $out4b) ? 'yes' : 'no') . "\n";

proc_terminate($proc);
proc_close($proc);
@unlink($countFile);
@unlink($serverScript);
@unlink($readyFile);
@unlink($portFile);
?>
--EXPECT--
out1=OK
hits1=1
out2=OK
hits2=2
out3=OK
hits3=2
out4a=OK
hits4a=3
out4b=OK
hits4b=4
outputs_match=yes
