--TEST--
parse_with_config() with debug absent keeps the legacy silent parser path
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// #216: an absent key must keep the exact positional ParseWithConfigCtx
// path with Debug=false (byte-identical to the pre-#216 behaviour). Any
// DEBUG line would be merged into the compared output by run-tests and
// fail the exact EXPECT.
echo \mesi\parse_with_config('<p>debug-absent</p>', 5, 'http://127.0.0.1/', []);
?>
--EXPECT--
<p>debug-absent</p>
