--TEST--
Test if mesi works
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
echo \mesi\parse('<!--esi Hello, world!-->', 5, "http://127.0.0.1");
?>
--EXPECT--
Hello, world!