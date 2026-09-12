--TEST--
Test parse function parameter types and ranges
--SKIPIF--
<?php if (!extension_loaded('mesi')) die('skip'); ?>
--FILE--
<?php
// Test with max_depth=0
$result = \mesi\parse('<!--esi test-->', 0, 'http://localhost/');
echo "depth0: [" . $result . "]\n";

// Test with max_depth=1
$result = \mesi\parse('<!--esi test-->', 1, 'http://localhost/');
echo "depth1: [" . $result . "]\n";

// Test with large max_depth
$result = \mesi\parse('<!--esi test-->', 100, 'http://localhost/');
echo "depth100: [" . $result . "]\n";
?>
--EXPECT--
depth0: [test]
depth1: [test]
depth100: [test]
