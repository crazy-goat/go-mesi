--TEST--
parse_with_config() include_error_marker renders for failed includes only when no onerror/fallback
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// #221: include_error_marker is raw HTML rendered in place of a failed
// <esi:include> that has neither onerror="continue" nor a fallback body.
// 127.0.0.1:1 is a deterministic connection-refused target (nothing listens
// there); block_private_ips=false is required for the loopback dial to be
// attempted at all, so the only failure mode is the refused connection.
$failed = '<esi:include src="http://127.0.0.1:1/frag" />';
$cfg = ['block_private_ips' => false, 'include_error_marker' => '<!-- ESI_ERROR -->'];

// marker set -> rendered in place of the failed include
echo "set=" . \mesi\parse_with_config('before' . $failed . 'after', 5, 'http://127.0.0.1/', $cfg) . "\n";

// onerror="continue" suppresses the marker
$continue = '<esi:include src="http://127.0.0.1:1/frag" onerror="continue" />';
echo "onerror=" . \mesi\parse_with_config('before' . $continue . 'after', 5, 'http://127.0.0.1/', $cfg) . "\n";

// a fallback body suppresses the marker
$fallback = '<esi:include src="http://127.0.0.1:1/frag">fallback</esi:include>';
echo "fallback=" . \mesi\parse_with_config('before' . $fallback . 'after', 5, 'http://127.0.0.1/', $cfg) . "\n";

// absent key -> silent, legacy positional path
$absent = \mesi\parse_with_config('before' . $failed . 'after', 5, 'http://127.0.0.1/', []);
echo "absent=" . $absent . "\n";

// explicit empty string -> configured but silent
$warnings = [];
set_error_handler(function ($errno, $errstr) use (&$warnings) {
    if ($errno === E_WARNING) { $warnings[] = $errstr; return true; }
    return false;
});
$empty = \mesi\parse_with_config('before' . $failed . 'after', 5, 'http://127.0.0.1/', [
    'block_private_ips' => false,
    'include_error_marker' => '',
]);
restore_error_handler();
echo "empty=" . $empty . "\n";
echo "empty_warnings=" . count($warnings) . "\n";
?>
--EXPECT--
set=before<!-- ESI_ERROR -->after
onerror=beforeafter
fallback=beforefallbackafter
absent=beforeafter
empty=beforeafter
empty_warnings=0
