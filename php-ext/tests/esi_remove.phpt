--TEST--
Test ESI remove tag processing
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
$input = '<p>keep</p><esi:remove>drop</esi:remove>';
$result = \mesi\parse($input, 5, 'http://localhost/');
echo $result;
?>
--EXPECT--
<p>keep</p>
