--TEST--
parse_with_config() allow_private_ips_for_allowed_hosts: option parsing + per-host private-IP bypass
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// capture warnings via a custom handler so E_WARNING doesn't fail the test
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) {
        $warnings[] = $errstr;
        return true;
    }
    return false;
});

// Helper: parse_with_config on plain input, returns [result, warning count]
function parse_ok(array $cfg) {
    global $warnings;
    $before = count($warnings);
    $r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', $cfg);
    return [($r === false ? 'false' : 'string'), count($warnings) - $before];
}

// --- 1. option parsing: absent -> false; present true/false; non-bool rejected ---
list($res, $w) = parse_ok([]);
echo "absent_result=$res\n";
echo "absent_warnings=$w\n";

list($res, $w) = parse_ok(['allow_private_ips_for_allowed_hosts' => true]);
echo "present_true_result=$res\n";
echo "present_true_warnings=$w\n";

list($res, $w) = parse_ok(['allow_private_ips_for_allowed_hosts' => false]);
echo "present_false_result=$res\n";
echo "present_false_warnings=$w\n";

// integers are accepted like block_private_ips (non-zero = enabled)
list($res, $w) = parse_ok(['allow_private_ips_for_allowed_hosts' => 1]);
echo "int_one_result=$res\n";
echo "int_one_warnings=$w\n";

list($res, $w) = parse_ok(['allow_private_ips_for_allowed_hosts' => 0]);
echo "int_zero_result=$res\n";
echo "int_zero_warnings=$w\n";

// a typo must never silently enable the bypass: non-bool/non-int rejected
list($res, $w) = parse_ok(['allow_private_ips_for_allowed_hosts' => 'yes']);
echo "wrongtype_result=$res\n";
echo "wrongtype_warnings=$w\n";

// --- 2. behaviour with a live server on 127.0.0.1 ---
// The bypass is the ONLY thing that lets a private-IP dial through while
// block_private_ips=true. Without it the include renders empty; with it the
// fragment appears. Loopback = private, so no DNS/docker needed.
$pid = 0;
$tmp = tempnam(sys_get_temp_dir(), 'mesi') . '.php';
file_put_contents($tmp, '<?php echo "BY-PASS-BODY";');
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
for ($i = 0; $i < 50; $i++) {
    $c = @fsockopen('127.0.0.1', $port, $e, $es, 0.1);
    if ($c !== false) { fclose($c); break; }
    usleep(20000);
}
$file = basename($tmp);
$loc  = 'http://localhost:' . $port . '/' . $file; // resolves to loopback (private)

function fetch($src, array $cfg) {
    $r = @\mesi\parse_with_config(
        '<esi:include src="' . $src . '" />',
        5, 'http://127.0.0.1/', $cfg);
    return $r === false ? 'false' : $r;
}

// 2a. bypass off (default) + block_private_ips=true -> private dial blocked
echo "no_bypass_blocked=" . fetch($loc, ['allowed_hosts' => 'localhost', 'block_private_ips' => true]) . "\n";

// 2b. bypass on + block_private_ips=true + host listed -> allowed
echo "bypass_allowed=" . fetch($loc, ['allowed_hosts' => 'localhost', 'block_private_ips' => true, 'allow_private_ips_for_allowed_hosts' => true]) . "\n";

// 2c. bypass on but host NOT in allowed_hosts -> blocked pre-dial (whitelist first)
echo "unlisted_blocked=" . fetch($loc, ['allowed_hosts' => '127.0.0.1', 'block_private_ips' => true, 'allow_private_ips_for_allowed_hosts' => true]) . "\n";

// 2d. bypass on but allowed_hosts empty -> no whitelist entry matches, so no
//     bypass is granted and the private dial stays blocked (only effective
//     when BOTH block_private_ips and a non-empty allowed_hosts are set)
echo "empty_whitelist_blocked=" . fetch($loc, ['allowed_hosts' => '', 'block_private_ips' => true, 'allow_private_ips_for_allowed_hosts' => true]) . "\n";

// 2e. sanity: block_private_ips=false needs nothing more (whitelist passes)
echo "no_block_trivially_allowed=" . fetch($loc, ['allowed_hosts' => 'localhost', 'block_private_ips' => false, 'allow_private_ips_for_allowed_hosts' => true]) . "\n";

// 2f. bypass request must not break non-bypass parses: shared client intact
$r = @\mesi\parse_with_config('plain-ok', 5, 'http://127.0.0.1/', ['block_private_ips' => true]);
echo "plain_after_bypass=" . ($r === false ? 'false' : $r) . "\n";
?>
--EXPECT--
absent_result=string
absent_warnings=0
present_true_result=string
present_true_warnings=0
present_false_result=string
present_false_warnings=0
int_one_result=string
int_one_warnings=0
int_zero_result=string
int_zero_warnings=0
wrongtype_result=false
wrongtype_warnings=1
no_bypass_blocked=
bypass_allowed=BY-PASS-BODY
unlisted_blocked=
empty_whitelist_blocked=
no_block_trivially_allowed=BY-PASS-BODY
plain_after_bypass=plain-ok
