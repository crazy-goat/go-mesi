--TEST--
Test plain HTML without ESI tags passes through
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
$input = '<!DOCTYPE html><html><body><p>plain html</p></body></html>';
$result = \mesi\parse($input, 5, 'http://localhost/');
echo $result;
?>
--EXPECT--
<!DOCTYPE html><html><body><p>plain html</p></body></html>
