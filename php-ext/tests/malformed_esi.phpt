--TEST--
Test malformed ESI tags are handled gracefully
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
$result = \mesi\parse('<p>hello<esi:remove unclosed tag', 5, 'http://localhost/');
echo $result;
?>
--EXPECTF--
%s
