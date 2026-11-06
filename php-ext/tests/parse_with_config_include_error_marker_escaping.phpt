--TEST--
parse_with_config() include_error_marker: quotes, backslashes and control chars survive JSON escaping
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// #221: the marker travels through libgomesi's ParseJson blob, so it must be
// JSON-escaped by the shared builder rather than hand-concatenated. A marker
// containing '"', '\\' and control characters must come back byte-identical
// in the rendered HTML (not truncated, not rejected, not a broken blob that
// makes libgomesi reject the parse config and return false).
$failed = '<esi:include src="http://127.0.0.1:1/frag" />';
$markers = [
    'quote' => '<!-- "quoted" -->',
    'backslash' => '<!-- C:\\path\\to -->',
    'quote_backslash' => '<!-- \\" -->',
    'control' => "<!-- \x01 \t \n -->",
    'unicode' => '<!-- zażółć -->',
];
$ok = 0;
foreach ($markers as $name => $marker) {
    $out = \mesi\parse_with_config('before' . $failed . 'after', 5, 'http://127.0.0.1/', [
        'block_private_ips' => false,
        'include_error_marker' => $marker,
    ]);
    $want = 'before' . $marker . 'after';
    echo $name . '=' . ($out === $want ? 'ok' : 'mismatch') . "\n";
    if ($out === $want) $ok++;
}
echo "total_ok=" . $ok . "/" . count($markers) . "\n";
?>
--EXPECT--
quote=ok
backslash=ok
quote_backslash=ok
control=ok
unicode=ok
total_ok=5/5
