--TEST--
Test empty input returns empty string
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
$result = \mesi\parse('', 5, 'http://localhost/');
var_dump($result);
?>
--EXPECT--
string(0) ""
