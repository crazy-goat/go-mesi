--TEST--
parse_with_config() debug=false keeps the parser silent
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// #216: an explicit false is routed through ParseJson as {"debug":false}
// and must not emit any core DEBUG line. run-tests merges stderr into the
// compared output, so any diagnostic would show up after the body and
// fail the exact EXPECT.
echo \mesi\parse_with_config('<p>debug-off</p>', 5, 'http://127.0.0.1/', ['debug' => false]);
?>
--EXPECT--
<p>debug-off</p>
