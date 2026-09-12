--TEST--
Test special characters in ESI content
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
$input = '<!--esi Café français 100% ✓-->';
$result = \mesi\parse($input, 5, 'http://localhost/');
echo $result;
?>
--EXPECT--
Café français 100% ✓
