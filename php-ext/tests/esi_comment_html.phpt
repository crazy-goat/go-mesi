--TEST--
Test ESI comment with HTML content
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
$result = \mesi\parse('<!--esi <b>bold</b> -->', 5, 'http://localhost/');
echo $result;
?>
--EXPECT--
<b>bold</b>
