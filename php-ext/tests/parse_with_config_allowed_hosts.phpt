--TEST--
parse_with_config() allowed_hosts: option parsing + host whitelist enforcement
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

// --- 1. option parsing: absent / empty / single / multiple hosts ---
list($res, $w) = parse_ok([]);
echo "absent_result=$res\n";
echo "absent_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => '']);
echo "empty_result=$res\n";
echo "empty_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => 'localhost']);
echo "single_result=$res\n";
echo "single_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => '127.0.0.1 localhost']);
echo "multi_result=$res\n";
echo "multi_warnings=$w\n";

// leading/trailing whitespace is fine (libgomesi splits with strings.Fields)
list($res, $w) = parse_ok(['allowed_hosts' => '  localhost  ']);
echo "padded_result=$res\n";
echo "padded_warnings=$w\n";

// --- 2. validation: non-string / whitespace-only / control chars ---
list($res, $w) = parse_ok(['allowed_hosts' => 42]);
echo "wrongtype_result=$res\n";
echo "wrongtype_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => '   ']);
echo "spacesonly_result=$res\n";
echo "spacesonly_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => "\t"]);
echo "tabonly_result=$res\n";
echo "tabonly_warnings=$w\n";

// Unicode whitespace only: every byte-family branch of the validator
// rejects (each value tokenizes to zero hostnames under Go's
// strings.Fields, exactly like nginx's mesi_allowed_hosts hardening #354):
//   U+00A0 (0xC2 0xA0), U+1680 (0xE1 0x9A 0x80), U+2008 (0xE2 0x80 0x88),
//   U+202F (0xE2 0x80 0xAF), U+205F (0xE2 0x81 0x9F), U+3000 (0xE3 0x80 0x80)
list($res, $w) = parse_ok(['allowed_hosts' => "\xC2\xA0"]);
echo "unicodews_a0_result=$res\n";
echo "unicodews_a0_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => "\xE1\x9A\x80"]);
echo "unicodews_1680_result=$res\n";
echo "unicodews_1680_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => "\xE2\x80\x88"]);
echo "unicodews_2008_result=$res\n";
echo "unicodews_2008_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => "\xE2\x80\xAF"]);
echo "unicodews_202f_result=$res\n";
echo "unicodews_202f_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => "\xE2\x81\x9F"]);
echo "unicodews_205f_result=$res\n";
echo "unicodews_205f_warnings=$w\n";

list($res, $w) = parse_ok(['allowed_hosts' => "\xE3\x80\x80"]);
echo "unicodews_3000_result=$res\n";
echo "unicodews_3000_warnings=$w\n";

// control character inside the list is rejected
list($res, $w) = parse_ok(['allowed_hosts' => "local\nhost"]);
echo "ctrlchar_result=$res\n";
echo "ctrlchar_warnings=$w\n";

// Unicode whitespace BETWEEN real hostnames is a legal separator
list($res, $w) = parse_ok(['allowed_hosts' => "localhost\xC2\xA0example.com"]);
echo "unicodelist_result=$res\n";
echo "unicodelist_warnings=$w\n";

// --- 3. behaviour with a live server on 127.0.0.1 ---
// block_private_ips=false lets the loopback dial through, so allowed_hosts
// is the only gate under test.
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
$file = basename($tmp);
$base = 'http://127.0.0.1:' . $port . '/' . $file;
$loc  = 'http://localhost:' . $port . '/' . $file;   // resolves to loopback

$NO_BLOCK = ['block_private_ips' => false];

function fetch($src, array $cfg) {
    $r = @\mesi\parse_with_config(
        '<esi:include src="' . $src . '" />',
        5, 'http://127.0.0.1/', $cfg);
    return $r === false ? 'false' : $r;
}

// 3a. empty allowed_hosts -> all hosts allowed (backward compatible)
echo "empty_allows=" . fetch($loc, ['allowed_hosts' => '', 'block_private_ips' => false]) . "\n";

// 3b. single host exact match
echo "single_allow=" . fetch($loc, ['allowed_hosts' => 'localhost', 'block_private_ips' => false]) . "\n";

// 3c. IP literal listed -> allowed; hostname not listed -> blocked pre-dial
echo "ip_allow=" . fetch($base, ['allowed_hosts' => '127.0.0.1', 'block_private_ips' => false]) . "\n";
echo "ip_block=" . fetch($loc, ['allowed_hosts' => '127.0.0.1', 'block_private_ips' => false]) . "\n";

// 3d. multiple hosts: both entries work regardless of order
echo "multi_allow_a=" . fetch($loc, ['allowed_hosts' => 'localhost 127.0.0.1', 'block_private_ips' => false]) . "\n";
echo "multi_allow_b=" . fetch($base, ['allowed_hosts' => '127.0.0.1 localhost', 'block_private_ips' => false]) . "\n";

// 3e. suffix-injection guard: 'notlocalhost' must NOT match 'localhost'.
//     Blocked before any dial, so no DNS dependency.
echo "suffix_block=" . fetch('http://notlocalhost:' . $port . '/' . $file, ['allowed_hosts' => 'localhost', 'block_private_ips' => false]) . "\n";

// 3f. '.' boundary: 'xlocalhost' must NOT match 'localhost'
echo "boundary_block=" . fetch('http://xlocalhost:' . $port . '/' . $file, ['allowed_hosts' => 'localhost', 'block_private_ips' => false]) . "\n";
?>
--EXPECT--
absent_result=string
absent_warnings=0
empty_result=string
empty_warnings=0
single_result=string
single_warnings=0
multi_result=string
multi_warnings=0
padded_result=string
padded_warnings=0
wrongtype_result=false
wrongtype_warnings=1
spacesonly_result=false
spacesonly_warnings=1
tabonly_result=false
tabonly_warnings=1
unicodews_a0_result=false
unicodews_a0_warnings=1
unicodews_1680_result=false
unicodews_1680_warnings=1
unicodews_2008_result=false
unicodews_2008_warnings=1
unicodews_202f_result=false
unicodews_202f_warnings=1
unicodews_205f_result=false
unicodews_205f_warnings=1
unicodews_3000_result=false
unicodews_3000_warnings=1
ctrlchar_result=false
ctrlchar_warnings=1
unicodelist_result=string
unicodelist_warnings=0
empty_allows=ALLOWED-BODY
single_allow=ALLOWED-BODY
ip_allow=ALLOWED-BODY
ip_block=
multi_allow_a=ALLOWED-BODY
multi_allow_b=ALLOWED-BODY
suffix_block=
boundary_block=
